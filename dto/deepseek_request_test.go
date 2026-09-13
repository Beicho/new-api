package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestRequestScalarAndReasoningPresence(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"logprobs":false,"echo":false,"temperature":0,"top_p":0,"max_tokens":0,"top_logprobs":0,"user_id":"","stream":false,"stream_options":{"include_usage":false,"include_obfuscation":false},"messages":[{"role":"assistant","content":"","reasoning_content":"","prefix":false}]}`,
		`{"logprobs":0}`,
		`{"logprobs":20,"echo":true}`,
		`{"stream_options":{}}`,
	} {
		t.Run(body, func(t *testing.T) {
			var request GeneralOpenAIRequest
			require.NoError(t, common.UnmarshalJsonStr(body, &request))
			copy, err := common.DeepCopy(&request)
			require.NoError(t, err)
			encoded, err := common.Marshal(copy)
			require.NoError(t, err)
			require.JSONEq(t, body, string(encoded))
		})
	}
}

func TestLogprobsRejectsInvalidWireTypes(t *testing.T) {
	for _, body := range []string{`{"logprobs":"true"}`, `{"logprobs":0.5}`, `{"logprobs":[]}`, `{"logprobs":{}}`} {
		var request GeneralOpenAIRequest
		require.Error(t, common.UnmarshalJsonStr(body, &request), body)
	}
}
