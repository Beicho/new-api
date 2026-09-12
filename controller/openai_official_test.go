package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestOfficialRealtimeBetaRejectedBeforeUpgrade(t *testing.T) {
	for _, header := range []struct{ key, value string }{{"OpenAI-Beta", "realtime=v1"}, {"Sec-WebSocket-Protocol", "realtime,openai-beta.realtime-v1"}} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/v1/realtime?model=test", nil)
		c.Request.Header.Set(header.key, header.value)
		common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
		Relay(c, types.RelayFormatOpenAIRealtime)
		require.Equal(t, 400, w.Code)
		require.Contains(t, w.Body.String(), "realtime_beta_not_supported")
		require.Empty(t, w.Header().Get("Upgrade"))
	}
}
