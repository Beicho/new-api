package dto

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOfficialRequestRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		body    string
		request any
	}{
		{`{"model":"test","background":false,"temperature":0,"max_tool_calls":0,"reasoning":{"context":"all_turns","mode":"pro","generate_summary":"auto"},"prompt_cache_options":{"mode":"explicit","ttl":"30m"},"moderation":{"model":"omni-moderation-latest"}}`, &OpenAIResponsesRequest{}},
		{`{"model":"test","messages":[{"role":"assistant","content":null,"audio":{"id":"audio_1"},"refusal":"","function_call":{"name":"f","arguments":"{}"}}],"prompt_cache_options":{"mode":"explicit"},"moderation":{"model":"omni-moderation-latest"},"stream":false,"stream_options":{"include_usage":false,"include_obfuscation":false}}`, &GeneralOpenAIRequest{}},
		{`{"model":"test","prompt":"edit","images":[{"image_url":"data:image/png;base64,AAAA"}],"mask":{"file_id":"mask_1"},"input_fidelity":"high","n":0,"stream":false,"partial_images":0,"output_compression":0}`, &ImageRequest{}},
	} {
		require.NoError(t, common.Unmarshal([]byte(tc.body), tc.request))
		body, err := common.Marshal(tc.request)
		require.NoError(t, err)
		require.JSONEq(t, tc.body, string(body))
	}
	var empty OpenAIResponsesRequest
	body, err := common.Marshal(empty)
	require.NoError(t, err)
	require.NotContains(t, string(body), "background")
}

func TestAudioVoiceUnion(t *testing.T) {
	for _, body := range []string{`"alloy"`, `{"id":"voice_123"}`} {
		var voice AudioVoice
		require.NoError(t, common.Unmarshal([]byte(body), &voice))
		out, err := common.Marshal(voice)
		require.NoError(t, err)
		require.JSONEq(t, body, string(out))
	}
	for _, body := range []string{`null`, `false`, `12`, `{}`, `{"id":""}`} {
		var voice AudioVoice
		require.Error(t, common.Unmarshal([]byte(body), &voice))
	}
	require.Equal(t, "alloy", (AudioVoice{Name: "alloy"}).String())
}

func TestCustomToolsDoNotEmitFunction(t *testing.T) {
	for _, target := range []any{&ToolCallRequest{}, &ToolCallResponse{}} {
		body := `{"type":"custom","id":"call_1","custom":{"name":"run","input":"hello"}}`
		require.NoError(t, common.Unmarshal([]byte(body), target))
		out, err := common.Marshal(target)
		require.NoError(t, err)
		require.JSONEq(t, body, string(out))
	}
}

func TestCacheWriteAliasPrecedence(t *testing.T) {
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{"cached_creation_tokens":12}`, 12},
		{`{"cache_write_tokens":10,"cached_creation_tokens":12}`, 10},
		{`{"cache_write_tokens":0,"cached_creation_tokens":12}`, 0},
	} {
		var details InputTokenDetails
		require.NoError(t, common.Unmarshal([]byte(tc.body), &details))
		require.Equal(t, tc.want, details.GetCacheCreationTokens())
	}
}
