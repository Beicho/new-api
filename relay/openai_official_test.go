package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Use the production helpers and a local upstream validation response so that
// request mapping, filtering and overrides are exercised without billing a DB.
func TestOfficialRequestPipeline(t *testing.T) {
	service.InitHttpClient()
	previousForce := constant.ForceStreamOption
	constant.ForceStreamOption = true
	t.Cleanup(func() { constant.ForceStreamOption = previousForce })
	for _, tc := range []struct {
		name, path, body string
		format           types.RelayFormat
		allowFields      bool
		override         map[string]any
		blocked          bool
	}{
		{"chat preserves options", "/v1/chat/completions", `{"model":"alias","stream":true,"stream_options":{"include_usage":false,"include_obfuscation":false},"messages":[{"role":"assistant","content":null,"audio":{"id":"a1"},"refusal":""}],"prompt_cache_options":{"mode":"explicit"},"moderation":{"model":"omni-moderation-latest"},"service_tier":"priority","safety_identifier":"user1"}`, types.RelayFormatOpenAI, true, nil, false},
		{"chat filters options", "/v1/chat/completions", `{"model":"alias","stream":true,"stream_options":{"include_usage":false,"include_obfuscation":false},"messages":[{"role":"user","content":"hi"}],"service_tier":"priority","safety_identifier":"user1"}`, types.RelayFormatOpenAI, false, nil, false},
		{"responses optional input", "/v1/responses", `{"model":"alias","background":false,"reasoning":{"context":"all_turns","mode":"pro","generate_summary":"auto"},"prompt_cache_options":{"mode":"explicit"},"moderation":{"model":"omni-moderation-latest"}}`, types.RelayFormatOpenAIResponses, false, map[string]any{"temperature": 0}, false},
		{"responses background override blocked", "/v1/responses", `{"model":"alias","background":false}`, types.RelayFormatOpenAIResponses, false, map[string]any{"background": true}, true},
		{"image zero", "/v1/images/generations", `{"model":"alias","prompt":"draw","n":0,"stream":false}`, types.RelayFormatOpenAIImage, false, map[string]any{"partial_images": 0}, false},
		{"image absent", "/v1/images/generations", `{"model":"alias","prompt":"draw"}`, types.RelayFormatOpenAIImage, false, nil, false},
		{"image JSON edit", "/v1/images/edits", `{"model":"alias","prompt":"edit","images":[{"file_id":"f1"}],"stream":true}`, types.RelayFormatOpenAIImage, false, map[string]any{"input_fidelity": "high"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captured := make(chan []byte, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				captured <- body
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"mock validation"}}`)
			}))
			defer upstream.Close()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			defer common.CleanupBodyStorage(c)
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(c, constant.ContextKeyChannelKey, "test-key")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "alias")
			common.SetContextKey(c, constant.ContextKeyChannelParamOverride, tc.override)
			common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{AllowIncludeObfuscation: tc.allowFields, AllowServiceTier: tc.allowFields, AllowSafetyIdentifier: tc.allowFields})
			c.Set("model_mapping", `{"alias":"mapped"}`)
			request, err := helper.GetAndValidateRequest(c, tc.format)
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{Request: request, OriginModelName: "alias", RequestURLPath: tc.path, RelayFormat: tc.format, RelayMode: relayconstant.Path2RelayMode(tc.path), StartTime: time.Now(), DisablePing: true}
			var apiErr *types.NewAPIError
			switch tc.format {
			case types.RelayFormatOpenAIResponses:
				apiErr = ResponsesHelper(c, info)
			case types.RelayFormatOpenAIImage:
				apiErr = ImageHelper(c, info)
			default:
				apiErr = TextHelper(c, info)
			}
			require.NotNil(t, apiErr)
			require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			if tc.blocked {
				require.Contains(t, apiErr.Error(), "background=true")
				require.Empty(t, captured)
				return
			}
			select {
			case raw := <-captured:
				var got, want map[string]any
				require.NoError(t, common.Unmarshal(raw, &got))
				require.NoError(t, common.Unmarshal([]byte(tc.body), &want))
				want["model"] = "mapped"
				for key, value := range tc.override {
					want[key] = value
				}
				if tc.format == types.RelayFormatOpenAI {
					want["stream_options"].(map[string]any)["include_usage"] = true
					if !tc.allowFields {
						delete(want["stream_options"].(map[string]any), "include_obfuscation")
						delete(want, "service_tier")
						delete(want, "safety_identifier")
					}
					require.False(t, info.ShouldIncludeUsage, "upstream usage must not override the client's preference")
				}
				wantJSON, err := common.Marshal(want)
				require.NoError(t, err)
				require.JSONEq(t, string(wantJSON), string(raw))
			default:
				t.Fatalf("request did not reach upstream: %v", apiErr)
			}
		})
	}
}
