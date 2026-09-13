package openai

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestOfficialRealtimeHeaders(t *testing.T) {
	for _, browser := range []bool{false, true} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/v1/realtime?model=test", nil)
		if browser {
			c.Request.Header.Set("Sec-WebSocket-Protocol", "realtime,openai-insecure-api-key.client-token")
		}
		info := officialTestInfo()
		info.RelayMode = relayconstant.RelayModeRealtime
		info.ApiKey = "channel-token"
		header := http.Header{}
		require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))
		require.Equal(t, "Bearer channel-token", header.Get("Authorization"))
		require.Empty(t, header.Get("OpenAI-Beta"))
		require.NotContains(t, header.Get("Sec-WebSocket-Protocol"), "client-token")
		require.NotContains(t, header.Get("Sec-WebSocket-Protocol"), "beta")
		c.Request.Header.Set("OpenAI-Beta", "realtime=v1")
		require.Error(t, (&Adaptor{}).SetupRequestHeader(c, &http.Header{}, info))
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/realtime", nil)
	info := officialTestInfo()
	info.ChannelType = constant.ChannelTypeCustom
	info.RelayMode = relayconstant.RelayModeRealtime
	info.ApiKey = "key"
	header := http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))
	require.Equal(t, "realtime=v1", header.Get("OpenAI-Beta"))
	info.ChannelType = constant.ChannelTypeAzure
	header = http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))
	require.Equal(t, "key", header.Get("api-key"))
}

func officialWebSocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := u.Upgrade(w, r, nil)
		if err == nil {
			accepted <- conn
		}
	}))
	t.Cleanup(server.Close)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	var peer *websocket.Conn
	select {
	case peer = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("websocket was not accepted")
	}
	t.Cleanup(func() { client.Close(); peer.Close() })
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	_ = peer.SetReadDeadline(time.Now().Add(3 * time.Second))
	return client, peer
}

func TestOfficialRealtimeGATransportAndSettlement(t *testing.T) {
	client, clientRelay := officialWebSocketPair(t)
	upstreamRelay, upstream := officialWebSocketPair(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/realtime", nil)
	info := officialTestInfo()
	info.ClientWs = clientRelay
	info.TargetWs = upstreamRelay
	charges := make(chan dto.RealtimeUsage, 10)
	result := make(chan *dto.RealtimeUsage, 1)
	go func() {
		result <- relayRealtimeGA(c, info, func(u *dto.RealtimeUsage) error { charges <- *u; return nil })
	}()
	session := `{"type":"session.update","session":{"type":"realtime","audio":{"input":{"format":{"type":"audio/pcmu"}},"output":{"format":{"type":"audio/pcma"},"voice":{"id":"voice_1"}}},"future_field":true}}`
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(session)))
	_, got, err := upstream.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, session, string(got))
	done := `{"type":"response.done","response":{"id":"r1","usage":{"total_tokens":30,"input_tokens":10,"output_tokens":20,"input_token_details":{"text_tokens":2,"audio_tokens":8,"cached_tokens":3},"output_token_details":{"audio_tokens":20}}}}`
	audio := `{"type":"response.output_audio.delta","delta":"` + base64.StdEncoding.EncodeToString(make([]byte, 8000)) + `"}`
	for _, body := range []string{`{"type":"response.output_audio_transcript.delta","delta":"hello world"}`, done, done, audio, `{"type":"response.output_text.delta","delta":"fallback text"}`, `{"type":"response.done","response":null}`} {
		require.NoError(t, upstream.WriteMessage(websocket.TextMessage, []byte(body)))
		_, got, err := client.ReadMessage()
		require.NoError(t, err)
		require.Equal(t, body, string(got))
	}
	upstream.Close()
	select {
	case total := <-result:
		require.Greater(t, total.TotalTokens, 30)
		require.Equal(t, 10, total.InputTokens)
		require.Equal(t, 3, total.InputTokenDetails.CachedTokens)
		require.Greater(t, total.OutputTokenDetails.AudioTokens, 20, "GA audio deltas must contribute to the missing-usage estimate")
		require.Equal(t, "g711_ulaw", info.InputAudioFormat)
		require.Equal(t, "g711_alaw", info.OutputAudioFormat)
	case <-time.After(time.Second):
		t.Fatal("Realtime relay did not stop")
	}
	require.Len(t, charges, 2, "duplicate done must not settle again, and local estimates must not be added to actual usage")
	first := <-charges
	require.Equal(t, 30, first.TotalTokens)
}
