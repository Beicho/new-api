package deepseek

import (
	"bytes"
	"context"
	"fmt"
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

func testInfo(t *testing.T, format types.RelayFormat, mode int, stream bool) *relaycommon.RelayInfo {
	t.Helper()
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeDeepSeek, ApiType: constant.APITypeDeepSeek,
			ChannelBaseUrl: "https://api.deepseek.com", ApiKey: "upstream-test-key",
			UpstreamModelName: "deepseek-flash", SupportStreamOptions: true,
		},
		RelayFormat: format, RelayMode: mode, IsStream: stream, DisablePing: true,
		OriginModelName: "deepseek-flash", StartTime: time.Now(), ShouldIncludeUsage: true,
	}
}

func testContext(body, path string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer client-test-key")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeDeepSeek)
	return c, recorder
}

func TestRequestURLs(t *testing.T) {
	for _, base := range []string{"https://api.deepseek.com", "https://proxy.example/gateway/deepseek"} {
		for _, suffix := range []string{"", "/", "/v1", "/v1/", "/beta", "/beta/", "/anthropic", "/anthropic/", "/anthropic/v1", "/anthropic/v1/"} {
			for _, endpoint := range []struct {
				format types.RelayFormat
				mode   int
				path   string
			}{
				{types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions, "/chat/completions"},
				{types.RelayFormatOpenAI, relayconstant.RelayModeCompletions, "/beta/completions"},
				{types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses, "/responses"},
				{types.RelayFormatClaude, relayconstant.RelayModeChatCompletions, "/anthropic/v1/messages"},
			} {
				info := testInfo(t, endpoint.format, endpoint.mode, false)
				info.ChannelBaseUrl = base + suffix
				got, err := (&Adaptor{}).GetRequestURL(info)
				require.NoError(t, err)
				want := endpoint.path
				if endpoint.format == types.RelayFormatOpenAI && endpoint.mode == relayconstant.RelayModeChatCompletions && strings.HasPrefix(suffix, "/beta") {
					want = "/beta" + want
				}
				require.Equal(t, base+want, got, base+suffix)
			}
		}
	}
	info := testInfo(t, types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponsesCompact, false)
	_, err := (&Adaptor{}).GetRequestURL(info)
	require.Error(t, err)
	info.ChannelBaseUrl = "file:///tmp/key"
	_, err = (&Adaptor{}).GetRequestURL(info)
	require.Error(t, err)
}

func TestThinkingSuffixesAndExplicitParameters(t *testing.T) {
	for _, model := range []string{"deepseek-flash", "deepseek-v4-flash", "deepseek-v4-pro"} {
		for _, suffix := range []string{"", "-none", "-max"} {
			for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude} {
				info := testInfo(t, format, relayconstant.RelayModeChatCompletions, false)
				info.UpstreamModelName = model + suffix
				a := &Adaptor{}
				var converted any
				var err error
				switch format {
				case types.RelayFormatOpenAI:
					converted, err = a.ConvertOpenAIRequest(nil, info, &dto.GeneralOpenAIRequest{Model: model + suffix, THINKING: []byte(`{"type":"enabled"}`), ReasoningEffort: "low"})
				case types.RelayFormatOpenAIResponses:
					converted, err = a.ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{Model: model + suffix, Reasoning: &dto.Reasoning{Effort: "low", Summary: "auto"}})
				case types.RelayFormatClaude:
					converted, err = a.ConvertClaudeRequest(nil, info, &dto.ClaudeRequest{Model: model + suffix, Thinking: &dto.Thinking{Type: "enabled"}, OutputConfig: []byte(`{"effort":"low","other":false}`)})
				}
				require.NoError(t, err)
				data, err := common.Marshal(converted)
				require.NoError(t, err)
				var body map[string]any
				require.NoError(t, common.Unmarshal(data, &body))
				require.Equal(t, model, body["model"])
				require.Equal(t, model, info.UpstreamModelName)
				effort := "low"
				if suffix != "" {
					effort = strings.TrimPrefix(suffix, "-")
				}
				if format == types.RelayFormatOpenAIResponses {
					require.Equal(t, effort, body["reasoning"].(map[string]any)["effort"])
					require.Equal(t, "auto", body["reasoning"].(map[string]any)["summary"])
				} else {
					thinkingType := "enabled"
					if suffix == "-none" {
						thinkingType = "disabled"
					}
					require.Equal(t, thinkingType, body["thinking"].(map[string]any)["type"])
					if format == types.RelayFormatClaude {
						require.Equal(t, false, body["output_config"].(map[string]any)["other"])
					}
				}
			}
		}
	}
	a := &Adaptor{}
	_, err := a.ConvertClaudeRequest(nil, nil, nil)
	require.Error(t, err)
	for _, max := range []*uint{nil, common.GetPointer(uint(0)), common.GetPointer(uint(123))} {
		r := &dto.GeneralOpenAIRequest{Model: "deepseek-flash", MaxTokens: max, MaxCompletionTokens: common.GetPointer(uint(45))}
		_, err := a.ConvertOpenAIRequest(nil, nil, r)
		require.NoError(t, err)
		want := uint(45)
		if max != nil {
			want = *max
		}
		require.Equal(t, want, *r.MaxTokens)
		require.Nil(t, r.MaxCompletionTokens)
	}
}

type capturedRequest struct {
	path   string
	header http.Header
	body   []byte
}

func TestNativeProtocolsThroughHTTP(t *testing.T) {
	service.InitHttpClient()
	for _, tc := range []struct {
		name, path, upstream, input, response string
		format                                types.RelayFormat
	}{
		{"chat", "/v1/chat/completions", "/chat/completions", `{"model":"alias","messages":[{"role":"user","content":[{"type":"text","text":"Look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8=","detail":"original"}}]},{"role":"assistant","content":"","reasoning_content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"weather","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"sunny"}],"tools":[{"type":"function","function":{"name":"weather","parameters":{"type":"object"},"strict":false}}],"logprobs":false,"temperature":0,"max_completion_tokens":0,"user_id":"u1"}`, `{"id":"chat1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"sunny","reasoning_content":"checked"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":3,"total_tokens":11,"prompt_cache_hit_tokens":2,"completion_tokens_details":{"reasoning_tokens":1}}}`, types.RelayFormatOpenAI},
		{"responses", "/v1/responses", "/responses", `{"model":"alias","input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/image.png","detail":"low"}]},{"type":"reasoning","content":[{"type":"reasoning_text","text":"checked"}]},{"type":"custom_tool_call","call_id":"patch1","name":"apply_patch","input":"*** Begin Patch\n*** End Patch"},{"type":"custom_tool_call_output","call_id":"patch1","output":[{"type":"input_image","image_url":"data:image/png;base64,aGVsbG8="}]}],"tools":[{"type":"custom","name":"apply_patch"}],"text":{"format":{"type":"json_schema","name":"answer","schema":{"type":"object"},"strict":false}},"max_output_tokens":0,"stream":false}`, `{"id":"resp1","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":8,"output_tokens":3,"total_tokens":11,"input_tokens_details":{"cached_tokens":2},"output_tokens_details":{"reasoning_tokens":1}}}`, types.RelayFormatOpenAIResponses},
		{"messages", "/v1/messages", "/anthropic/v1/messages", `{"model":"alias","max_tokens":0,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://example.com/image.png"}},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}]},{"role":"assistant","content":[{"type":"thinking","thinking":"checked","signature":""},{"type":"tool_use","id":"tool1","name":"weather","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool1","content":[{"type":"text","text":"sunny"}]}]}],"tools":[{"name":"weather","input_schema":{"type":"object","additionalProperties":false}}],"temperature":0}`, `{"id":"msg1","type":"message","role":"assistant","model":"deepseek-flash","content":[{"type":"thinking","thinking":"checked"},{"type":"text","text":"sunny"}],"stop_reason":"end_turn","usage":{"input_tokens":8,"output_tokens":3,"cache_read_input_tokens":2}}`, types.RelayFormatClaude},
		{"fim", "/v1/completions", "/beta/completions", `{"model":"alias","prompt":"def add(a,b):","suffix":"\n","logprobs":0,"echo":false,"max_tokens":0}`, `{"id":"fim1","object":"text_completion","choices":[{"index":0,"text":"return a+b","finish_reason":"stop","logprobs":{"tokens":["return"],"token_logprobs":[-0.1]}}],"usage":{"prompt_tokens":8,"completion_tokens":3,"total_tokens":11,"prompt_cache_hit_tokens":2,"completion_tokens_details":{"reasoning_tokens":1}}}`, types.RelayFormatOpenAI},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captured := make(chan capturedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				captured <- capturedRequest{r.URL.Path, r.Header.Clone(), body}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.response)
			}))
			defer server.Close()
			info := testInfo(t, tc.format, relayconstant.Path2RelayMode(tc.path), false)
			info.OriginModelName = "alias"
			info.ChannelBaseUrl = server.URL + "/proxy/v1/"
			c, recorder := testContext(tc.input, tc.path)
			c.Request.Header.Set("anthropic-version", "2023-06-01")
			c.Request.Header.Set("anthropic-beta", "interleaved-thinking-2025-05-14")
			c.Set("model_mapping", `{"alias":"deepseek-flash-none"}`)
			request, err := helper.GetAndValidateRequest(c, tc.format)
			require.NoError(t, err)
			require.NoError(t, helper.ModelMappedHelper(c, info, request))
			a := &Adaptor{}
			a.Init(info)
			var converted any
			switch request := request.(type) {
			case *dto.GeneralOpenAIRequest:
				converted, err = a.ConvertOpenAIRequest(c, info, request)
			case *dto.OpenAIResponsesRequest:
				converted, err = a.ConvertOpenAIResponsesRequest(c, info, *request)
			case *dto.ClaudeRequest:
				converted, err = a.ConvertClaudeRequest(c, info, request)
			}
			require.NoError(t, err)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			resp, err := a.DoRequest(c, info, bytes.NewReader(body))
			require.NoError(t, err)
			got := <-captured
			require.Equal(t, "/proxy"+tc.upstream, got.path)
			if tc.format == types.RelayFormatClaude {
				require.Equal(t, info.ApiKey, got.header.Get("x-api-key"))
				require.Empty(t, got.header.Get("Authorization"))
				require.Equal(t, c.GetHeader("anthropic-version"), got.header.Get("anthropic-version"))
				require.Equal(t, c.GetHeader("anthropic-beta"), got.header.Get("anthropic-beta"))
			} else {
				require.Equal(t, "Bearer "+info.ApiKey, got.header.Get("Authorization"))
			}
			var before, after map[string]any
			require.NoError(t, common.UnmarshalJsonStr(tc.input, &before))
			require.NoError(t, common.Unmarshal(got.body, &after))
			require.Equal(t, "deepseek-flash", after["model"])
			for key, value := range before {
				if key == "model" {
					continue
				}
				if key == "max_completion_tokens" {
					key = "max_tokens"
				}
				require.Equal(t, value, after[key], key)
			}
			usageAny, apiErr := a.DoResponse(c, resp.(*http.Response), info)
			require.Nil(t, apiErr)
			usage := usageAny.(*dto.Usage)
			require.Equal(t, 8, usage.PromptTokens)
			require.Equal(t, 3, usage.CompletionTokens)
			require.Equal(t, 2, usage.PromptTokensDetails.CachedTokens)
			if tc.format != types.RelayFormatClaude {
				require.Equal(t, 1, usage.CompletionTokenDetails.ReasoningTokens)
			}
			require.JSONEq(t, tc.response, recorder.Body.String())
		})
	}
}

func TestBetaRoutingUsesFinalBodyIncludingPassthrough(t *testing.T) {
	service.InitHttpClient()
	for _, body := range []string{
		`{"model":"deepseek-flash","messages":[{"role":"assistant","content":"prefix","prefix":true}],"unknown":false}`,
		`{"model":"deepseek-flash","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"f","strict":true,"parameters":{"type":"object","additionalProperties":false}}}]}`,
	} {
		captured := make(chan capturedRequest, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			data, _ := io.ReadAll(r.Body)
			captured <- capturedRequest{r.URL.Path, r.Header.Clone(), data}
			_, _ = io.WriteString(w, `{}`)
		}))
		info := testInfo(t, types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions, false)
		info.ChannelBaseUrl = server.URL
		info.ChannelSetting.PassThroughBodyEnabled = true
		info.HeadersOverride = map[string]any{"X-Test-Override": "yes"}
		c, _ := testContext(body, "/v1/chat/completions")
		a := &Adaptor{}
		response, err := a.DoRequest(c, info, strings.NewReader(body))
		require.NoError(t, err)
		response.(*http.Response).Body.Close()
		got := <-captured
		require.Equal(t, "/beta/chat/completions", got.path)
		require.Equal(t, body, string(got.body))
		require.Equal(t, "yes", got.header.Get("X-Test-Override"))
		server.Close()
	}
}

func TestHTTPFailuresAndRequestCancellation(t *testing.T) {
	service.InitHttpClient()
	for _, status := range []int{400, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":{"type":"upstream_error","message":"test failure"}}`)
			}))
			defer server.Close()
			info := testInfo(t, types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses, false)
			info.ChannelBaseUrl = server.URL
			c, _ := testContext(`{}`, "/v1/responses")
			response, err := (&Adaptor{}).DoRequest(c, info, strings.NewReader(`{}`))
			require.NoError(t, err)
			apiErr := service.RelayErrorHandler(c.Request.Context(), response.(*http.Response), false)
			require.Equal(t, status, apiErr.StatusCode)
			require.Contains(t, apiErr.Error(), "test failure")
		})
	}
	info := testInfo(t, types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses, false)
	c, _ := testContext(`{}`, "/v1/responses")
	ctx, cancel := context.WithCancel(c.Request.Context())
	cancel()
	c.Request = c.Request.WithContext(ctx)
	_, err := (&Adaptor{}).DoRequest(c, info, strings.NewReader(`{}`))
	require.Error(t, err)
}
