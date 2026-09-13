package deepseek

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func streamResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func sse(events ...string) string {
	var result strings.Builder
	for _, event := range events {
		result.WriteString("data: " + event + "\n\n")
	}
	return result.String()
}

func TestChatAndFIMTerminalUsage(t *testing.T) {
	for _, mode := range []int{relayconstant.RelayModeChatCompletions, relayconstant.RelayModeCompletions} {
		for _, includeUsage := range []bool{true, false} {
			for _, zero := range []bool{false, true} {
				info := testInfo(t, types.RelayFormatOpenAI, mode, true)
				if mode == relayconstant.RelayModeCompletions {
					info.ChannelSetting.ForceFormat = true
					info.ChannelSetting.ThinkingToContent = true
				}
				info.ShouldIncludeUsage = includeUsage
				info.SetEstimatePromptTokens(999)
				c, recorder := testContext(`{}`, "/v1/chat/completions")
				content := `"delta":{"reasoning_content":"checking","content":"hello","tool_calls":[{"index":0,"id":"call1","type":"function","function":{"name":"test","arguments":"{}"}}]}`
				end := `"delta":{}`
				if mode == relayconstant.RelayModeCompletions {
					content = `"text":"hello"`
					end = `"text":""`
				}
				tokens := `"prompt_tokens":10,"completion_tokens":4,"total_tokens":14,"prompt_cache_hit_tokens":3,"completion_tokens_details":{"reasoning_tokens":2}`
				if zero {
					tokens = `"prompt_tokens":0,"completion_tokens":0,"total_tokens":0`
				}
				body := sse(`{"id":"chat1","choices":[{"index":0,`+content+`,"finish_reason":null}],"usage":null}`,
					`{"id":"chat1","choices":[{"index":0,`+end+`,"finish_reason":"stop"}],"usage":{`+tokens+`}}`, "[DONE]")
				usageAny, apiErr := (&Adaptor{}).DoResponse(c, streamResponse(body), info)
				require.Nil(t, apiErr)
				usage := usageAny.(*dto.Usage)
				if zero {
					require.Zero(t, usage.TotalTokens)
				} else {
					require.Equal(t, 14, usage.TotalTokens)
					require.Equal(t, 3, usage.PromptTokensDetails.CachedTokens)
					require.Equal(t, 2, usage.CompletionTokenDetails.ReasoningTokens)
				}
				output := recorder.Body.String()
				require.Contains(t, output, `"finish_reason":"stop"`)
				require.Contains(t, output, "hello")
				require.Equal(t, 1, strings.Count(output, "data: [DONE]"))
				if includeUsage {
					require.Contains(t, output, `"usage":`)
				} else {
					require.NotContains(t, output, `"usage":`)
				}
				require.False(t, info.StreamStatus.HasErrors())
			}
		}
	}
}

func TestResponsesNativeEventsAndTerminalUsage(t *testing.T) {
	for _, terminal := range []string{"completed", "incomplete", "failed"} {
		for _, zero := range []bool{false, true} {
			info := testInfo(t, types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses, true)
			info.SetEstimatePromptTokens(999)
			c, recorder := testContext(`{}`, "/v1/responses")
			tokens := `"input_tokens":10,"output_tokens":4,"total_tokens":14,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":2}`
			if zero {
				tokens = `"input_tokens":0,"output_tokens":0,"total_tokens":0`
			}
			last := `{"type":"response.` + terminal + `","sequence_number":6,"response":{"id":"r1","status":"` + terminal + `","usage":{` + tokens + `}`
			if terminal == "failed" {
				last += `,"error":{"code":"server_error","message":"failed"}`
			}
			if terminal == "incomplete" {
				last += `,"incomplete_details":{"reason":"max_output_tokens"}`
			}
			last += `}}`
			events := []string{
				`{"type":"response.created","sequence_number":0,"response":{"id":"r1","status":"in_progress"}}`,
				`{"type":"response.reasoning_text.delta","sequence_number":1,"delta":"thinking"}`,
				`{"type":"response.output_item.added","sequence_number":2,"item":{"type":"custom_tool_call","name":"apply_patch","call_id":"p1"}}`,
				`{"type":"response.custom_tool_call_input.delta","sequence_number":3,"delta":"*** Begin Patch\n"}`,
				`{"type":"response.function_call_arguments.delta","sequence_number":4,"delta":"{\"a\":"}`,
				`{"type":"response.output_text.delta","sequence_number":5,"delta":"hello"}`, last,
			}
			usageAny, apiErr := (&Adaptor{}).DoResponse(c, streamResponse(sse(events...)), info)
			require.Nil(t, apiErr)
			usage := usageAny.(*dto.Usage)
			if zero {
				require.Zero(t, usage.TotalTokens)
			} else {
				require.Equal(t, 14, usage.TotalTokens)
				require.Equal(t, 3, usage.PromptTokensDetails.CachedTokens)
				require.Equal(t, 2, usage.CompletionTokenDetails.ReasoningTokens)
			}
			output := recorder.Body.String()
			lastPosition := -1
			for _, event := range events {
				position := strings.Index(output, "data: "+event)
				require.Greater(t, position, lastPosition)
				lastPosition = position
			}
			require.Contains(t, output, "event: response."+terminal)
			require.NotContains(t, output, "[DONE]")
			require.Equal(t, terminal == "failed", info.StreamStatus.HasErrors())
		}
	}
}

func TestMessagesNativeStreamAndFailure(t *testing.T) {
	for _, failure := range []bool{false, true} {
		info := testInfo(t, types.RelayFormatClaude, relayconstant.RelayModeChatCompletions, true)
		info.SetEstimatePromptTokens(999)
		c, recorder := testContext(`{}`, "/v1/messages")
		events := []string{
			`{"type":"message_start","message":{"id":"m1","model":"deepseek-flash","usage":{"input_tokens":10,"output_tokens":0,"cache_read_input_tokens":3}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"checking"}}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"t1","name":"weather","input":{}}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
		}
		if failure {
			events = append(events, `{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`)
		} else {
			events = append(events, `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":0}}`, `{"type":"message_stop"}`)
		}
		usageAny, apiErr := (&Adaptor{}).DoResponse(c, streamResponse(sse(events...)), info)
		require.Nil(t, apiErr)
		usage := usageAny.(*dto.Usage)
		require.Equal(t, 10, usage.PromptTokens)
		require.Equal(t, 3, usage.PromptTokensDetails.CachedTokens)
		if !failure {
			require.Zero(t, usage.CompletionTokens)
		}
		for _, event := range events {
			require.Contains(t, recorder.Body.String(), "data: "+event)
		}
		require.NotContains(t, recorder.Body.String(), "[DONE]")
		require.Equal(t, failure, info.StreamStatus.HasErrors())
	}
}

func TestStreamFailuresAreRecordedWithoutReturningRetryableErrors(t *testing.T) {
	for _, body := range []string{
		sse(`{"type":"response.output_text.delta","delta":"partial"}`),
		sse(`{"type":"response.output_text.delta","delta":"partial"}`, `{broken`),
		sse(`{"type":"error","code":"server_error","message":"failed"}`),
	} {
		info := testInfo(t, types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses, true)
		c, recorder := testContext(`{}`, "/v1/responses")
		_, apiErr := (&Adaptor{}).DoResponse(c, streamResponse(body), info)
		require.Nil(t, apiErr)
		require.True(t, info.StreamStatus.HasErrors())
		require.NotContains(t, recorder.Body.String(), "[DONE]")
	}
	info := testInfo(t, types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions, true)
	info.ChannelSetting.ForceFormat = true
	c, recorder := testContext(`{}`, "/v1/chat/completions")
	_, apiErr := (&Adaptor{}).DoResponse(c, streamResponse(sse(`{"choices":[{"delta":{"content":"partial"}}]}`, `{"error":{"type":"server_error","message":"failed"}}`)), info)
	require.Nil(t, apiErr)
	require.True(t, info.StreamStatus.HasErrors())
	require.Contains(t, recorder.Body.String(), "partial")
	require.Contains(t, recorder.Body.String(), `"error"`)
	require.NotContains(t, recorder.Body.String(), "[DONE]")
}

func TestResponsesTerminalClosesOpenHTTPStream(t *testing.T) {
	service.InitHttpClient()
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse(`{"type":"response.completed","sequence_number":0,"response":{"usage":{"input_tokens":1,"output_tokens":0}}}`))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(closed)
	}))
	defer server.Close()
	info := testInfo(t, types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses, true)
	info.ChannelBaseUrl = server.URL
	c, _ := testContext(`{"model":"deepseek-flash","input":"hi","stream":true}`, "/v1/responses")
	a := &Adaptor{}
	response, err := a.DoRequest(c, info, strings.NewReader(`{"model":"deepseek-flash","input":"hi","stream":true}`))
	require.NoError(t, err)
	start := time.Now()
	_, apiErr := a.DoResponse(c, response.(*http.Response), info)
	require.Nil(t, apiErr)
	require.Less(t, time.Since(start), 2*time.Second)
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream connection not closed")
	}
}

func TestClientCancellationClosesResponseBody(t *testing.T) {
	info := testInfo(t, types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses, true)
	c, _ := testContext(`{}`, "/v1/responses")
	ctx, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	r, w := io.Pipe()
	defer w.Close()
	go func() {
		_, _ = fmt.Fprint(w, sse(`{"type":"response.output_text.delta","delta":"partial"}`))
		cancel()
	}()
	response := streamResponse("")
	response.Body = r
	start := time.Now()
	_, apiErr := (&Adaptor{}).DoResponse(c, response, info)
	require.Nil(t, apiErr)
	require.Less(t, time.Since(start), 2*time.Second)
	require.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
}

func TestNonstreamZeroUsageIsAuthoritative(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses} {
		mode := relayconstant.RelayModeChatCompletions
		body := `{"choices":[{"message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`
		if format == types.RelayFormatOpenAIResponses {
			mode = relayconstant.RelayModeResponses
			body = `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}`
		}
		info := testInfo(t, format, mode, false)
		info.SetEstimatePromptTokens(999)
		c, recorder := testContext(`{}`, "/v1/responses")
		response := streamResponse(body)
		response.Header.Set("Content-Type", "application/json")
		usageAny, apiErr := (&Adaptor{}).DoResponse(c, response, info)
		require.Nil(t, apiErr)
		require.Zero(t, usageAny.(*dto.Usage).TotalTokens)
		require.JSONEq(t, body, recorder.Body.String())
		require.False(t, common.GetContextKeyBool(c, "local_count_tokens"))
	}
}

func TestFIMWithoutUsagePreservesTextWhenFormattingIsEnabled(t *testing.T) {
	info := testInfo(t, types.RelayFormatOpenAI, relayconstant.RelayModeCompletions, false)
	info.ChannelSetting.ForceFormat = true
	info.SetEstimatePromptTokens(7)
	c, recorder := testContext(`{}`, "/v1/completions")
	response := streamResponse(`{"object":"text_completion","choices":[{"text":"hello world","index":0,"finish_reason":"stop"}]}`)
	usageAny, apiErr := (&Adaptor{}).DoResponse(c, response, info)
	require.Nil(t, apiErr)
	usage := usageAny.(*dto.Usage)
	require.Equal(t, 7, usage.PromptTokens)
	require.Positive(t, usage.CompletionTokens)
	var body map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "text_completion", body["object"])
	require.Equal(t, "hello world", body["choices"].([]any)[0].(map[string]any)["text"])
}
