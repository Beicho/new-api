package openaicompat

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOfficialChatConversionPreservesCacheAndOptions(t *testing.T) {
	var req dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"test","messages":[{"role":"developer","content":[{"type":"text","text":"stable","prompt_cache_breakpoint":{"mode":"explicit"}}]},{"role":"user","content":[{"type":"text","text":"hi","prompt_cache_breakpoint":{"mode":"explicit"}}]}],"prompt_cache_options":{"mode":"explicit"},"moderation":{"model":"omni-moderation-latest"},"stream_options":{"include_usage":true,"include_obfuscation":false},"max_completion_tokens":0,"temperature":0,"top_logprobs":0,"parallel_tool_calls":false,"verbosity":"low"}`), &req))
	out, err := ChatCompletionsRequestToResponsesRequest(&req)
	require.NoError(t, err)
	require.JSONEq(t, `[{"role":"developer","content":[{"type":"input_text","text":"stable","prompt_cache_breakpoint":{"mode":"explicit"}}]},{"role":"user","content":[{"type":"input_text","text":"hi","prompt_cache_breakpoint":{"mode":"explicit"}}]}]`, string(out.Input))
	require.JSONEq(t, `{"mode":"explicit"}`, string(out.PromptCacheOptions))
	require.Equal(t, req.Moderation, out.Moderation)
	require.Zero(t, *out.MaxOutputTokens)
	require.Zero(t, *out.Temperature)
	require.Zero(t, *out.TopLogProbs)
	require.Equal(t, "false", string(out.ParallelToolCalls))
	require.False(t, *out.StreamOptions.IncludeObfuscation)
	require.Nil(t, out.StreamOptions.IncludeUsage)
	require.JSONEq(t, `{"verbosity":"low"}`, string(out.Text))
}

func TestOfficialChatConversionRejectsAudio(t *testing.T) {
	for _, body := range []string{
		`{"model":"test","audio":{"voice":"alloy"}}`,
		`{"model":"test","modalities":["audio"]}`,
		`{"model":"test","messages":[{"role":"assistant","audio":{"id":"a"}}]}`,
		`{"model":"test","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"AAAA","format":"wav"}}]}]}`,
	} {
		var req dto.GeneralOpenAIRequest
		require.NoError(t, common.Unmarshal([]byte(body), &req))
		_, err := ChatCompletionsRequestToResponsesRequest(&req)
		require.Error(t, err)
	}
}

func TestOfficialResponsesConversionKeepsToolsAndUsage(t *testing.T) {
	var resp dto.OpenAIResponsesResponse
	require.NoError(t, common.Unmarshal([]byte(`{"model":"test","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"run this"}]},{"type":"custom_tool_call","id":"item","call_id":"call","name":"run","input":"echo hi"}],"usage":{"input_tokens":100,"output_tokens":10,"input_tokens_details":{"cached_tokens":20,"cache_write_tokens":30},"output_tokens_details":{"reasoning_tokens":5}}}`), &resp))
	out, usage, err := ResponsesResponseToChatCompletionsResponse(&resp, "chat_1")
	require.NoError(t, err)
	require.Equal(t, 30, usage.PromptTokensDetails.GetCacheCreationTokens())
	require.Equal(t, 5, usage.CompletionTokenDetails.ReasoningTokens)
	require.Equal(t, "run this", out.Choices[0].Message.Content)
	require.JSONEq(t, `[{"id":"call","type":"custom","custom":{"name":"run","input":"echo hi"}}]`, string(out.Choices[0].Message.ToolCalls))
}
