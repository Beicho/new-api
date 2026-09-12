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

// Exercise the production request helpers through HTTP. The mock returns a
// validation error after capturing the request, so these tests need no billing DB.
func TestDeepSeekHandlerRequestPipeline(t *testing.T) {
	service.InitHttpClient()
	for _, tc := range []struct {
		name, path, upstream, body string
		format                     types.RelayFormat
		passthrough                bool
		override                   map[string]any
	}{
		{"messages explicit zero", "/v1/messages", "/anthropic/v1/messages", `{"model":"deepseek-flash","max_tokens":0,"temperature":0,"messages":[{"role":"user","content":"hi"}]}`, types.RelayFormatClaude, false, nil},
		{"responses instructions only", "/v1/responses", "/responses", `{"model":"deepseek-flash","instructions":"hi","max_output_tokens":0}`, types.RelayFormatOpenAIResponses, false, nil},
		{"chat final override selects beta", "/v1/chat/completions", "/beta/chat/completions", `{"model":"deepseek-flash","messages":[{"role":"assistant","content":"prefix","prefix":false}],"max_completion_tokens":0}`, types.RelayFormatOpenAI, false, map[string]any{"messages": []any{map[string]any{"role": "assistant", "content": "prefix", "prefix": true}}}},
		{"chat passthrough unchanged", "/v1/chat/completions", "/beta/chat/completions", `{"model":"deepseek-flash","messages":[{"role":"assistant","content":"prefix","prefix":true}],"unknown":false}`, types.RelayFormatOpenAI, true, nil},
		{"responses passthrough unchanged", "/v1/responses", "/responses", `{"model":"deepseek-flash","input":"hi","unknown":false}`, types.RelayFormatOpenAIResponses, true, nil},
		{"messages passthrough unchanged", "/v1/messages", "/anthropic/v1/messages", `{"model":"deepseek-flash","max_tokens":0,"messages":[{"role":"user","content":"hi"}],"unknown":false}`, types.RelayFormatClaude, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type capture struct {
				path string
				body []byte
			}
			captured := make(chan capture, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				captured <- capture{r.URL.Path, data}
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"mock validation"}}`)
			}))
			defer server.Close()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeDeepSeek)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL+"/v1/")
			common.SetContextKey(c, constant.ContextKeyChannelKey, "mock-key")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "deepseek-flash")
			common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: tc.passthrough})
			common.SetContextKey(c, constant.ContextKeyChannelParamOverride, tc.override)
			request, err := helper.GetAndValidateRequest(c, tc.format)
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{Request: request, OriginModelName: "deepseek-flash", RelayFormat: tc.format, RelayMode: relayconstant.Path2RelayMode(tc.path), StartTime: time.Now(), DisablePing: true}
			var apiErr *types.NewAPIError
			switch tc.format {
			case types.RelayFormatClaude:
				apiErr = ClaudeHelper(c, info)
			case types.RelayFormatOpenAIResponses:
				apiErr = ResponsesHelper(c, info)
			default:
				apiErr = TextHelper(c, info)
			}
			require.NotNil(t, apiErr)
			require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			select {
			case got := <-captured:
				require.Equal(t, tc.upstream, got.path)
				if tc.passthrough {
					require.Equal(t, tc.body, string(got.body))
					return
				}
				var body map[string]any
				require.NoError(t, common.Unmarshal(got.body, &body))
				switch tc.format {
				case types.RelayFormatClaude:
					require.Equal(t, float64(0), body["max_tokens"])
				case types.RelayFormatOpenAIResponses:
					require.Equal(t, float64(0), body["max_output_tokens"])
					require.NotContains(t, body, "input")
				default:
					require.Equal(t, float64(0), body["max_tokens"])
					require.NotContains(t, body, "max_completion_tokens")
				}
			default:
				t.Fatalf("request did not reach upstream: %v", apiErr)
			}
		})
	}
}
