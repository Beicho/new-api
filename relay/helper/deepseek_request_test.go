package helper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func requestContext(body string, channelType int) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyChannelType, channelType)
	return c
}

func TestResponsesInstructionsOnlyIsDeepSeekSpecific(t *testing.T) {
	for _, tc := range []struct {
		channel int
		body    string
		valid   bool
	}{
		{constant.ChannelTypeDeepSeek, `{"model":"deepseek-flash","instructions":"Answer hello"}`, true},
		{constant.ChannelTypeOpenAI, `{"model":"gpt-4.1","instructions":"Answer hello"}`, false},
		{constant.ChannelTypeDeepSeek, `{"model":"deepseek-flash"}`, false},
		{constant.ChannelTypeDeepSeek, `{"model":"deepseek-flash","instructions":null}`, false},
		{constant.ChannelTypeDeepSeek, `{"model":"deepseek-flash","input":"Hello"}`, true},
	} {
		c := requestContext(tc.body, tc.channel)
		_, err := GetAndValidateResponsesRequest(c)
		if tc.valid {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

func TestLogprobsValidationByEndpoint(t *testing.T) {
	for _, tc := range []struct {
		mode  int
		value string
		valid bool
	}{
		{relayconstant.RelayModeChatCompletions, "false", true},
		{relayconstant.RelayModeChatCompletions, "true", true},
		{relayconstant.RelayModeChatCompletions, "0", false},
		{relayconstant.RelayModeCompletions, "0", true},
		{relayconstant.RelayModeCompletions, "20", true},
		{relayconstant.RelayModeCompletions, "false", false},
	} {
		c := requestContext(`{"model":"deepseek-flash","prompt":"hi","messages":[{"role":"user","content":"hi"}],"logprobs":`+tc.value+`}`, constant.ChannelTypeDeepSeek)
		_, err := GetAndValidateTextRequest(c, tc.mode)
		if tc.valid {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}
