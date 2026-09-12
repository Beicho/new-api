package openai

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func officialTestInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: "test"}, StartTime: time.Now()}
}

func TestOfficialMediaSSEFlushesBeforeUpstreamCloses(t *testing.T) {
	for _, endType := range []string{"image_generation.completed", "image_edit.completed", "transcript.text.done", "speech.audio.done"} {
		t.Run(endType, func(t *testing.T) {
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			first := "id: event-1\nevent: future.media.event\ndata: {\"type\":\"future.media.event\",\n" + "data: \"payload\":\"" + strings.Repeat("a", 128<<10) + "\"}\n\n"
			last := "event: " + endType + "\ndata: {\"type\":\"" + endType + "\",\"usage\":{\"input_tokens\":7,\"output_tokens\":3,\"total_tokens\":10,\"input_tokens_details\":{\"audio_tokens\":5},\"output_tokens_details\":{\"audio_tokens\":3}}}\n\n"
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, first)
				w.(http.Flusher).Flush()
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				_, _ = io.WriteString(w, last)
				w.(http.Flusher).Flush()
			}))
			defer upstream.Close()
			defer unblock()
			result := make(chan *dto.Usage, 1)
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				req, _ := http.NewRequestWithContext(r.Context(), "GET", upstream.URL, nil)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					http.Error(w, err.Error(), 502)
					return
				}
				c, _ := gin.CreateTestContext(w)
				c.Request = r
				result <- openAIMediaStreamHandler(c, resp, officialTestInfo())
			}))
			defer gateway.Close()
			client := &http.Client{Timeout: 3 * time.Second}
			resp, err := client.Get(gateway.URL)
			require.NoError(t, err)
			defer resp.Body.Close()
			reader := bufio.NewReader(resp.Body)
			var got strings.Builder
			for {
				line, err := reader.ReadString('\n')
				require.NoError(t, err)
				got.WriteString(line)
				if line == "\n" {
					break
				}
			}
			require.Equal(t, first, got.String(), "first frame must arrive before releasing upstream")
			unblock()
			rest, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.Equal(t, last, string(rest))
			select {
			case usage := <-result:
				require.Equal(t, 10, usage.TotalTokens)
				require.Equal(t, 5, usage.PromptTokensDetails.AudioTokens)
				require.Equal(t, 3, usage.CompletionTokenDetails.AudioTokens)
			case <-time.After(time.Second):
				t.Fatal("stream did not complete")
			}
		})
	}
}

func TestOfficialMediaSSEErrorsDoNotAppendJSON(t *testing.T) {
	for _, body := range []string{
		"event: error\ndata: {\"type\":\"error\",\"error\":{\"message\":\"failed\"}}\n\n",
		"event: transcript.text.delta\ndata: {\"type\":\"transcript.text.delta\",\"delta\":\"hi\"}\n\n",
		"data: not-json\n\n",
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/", nil)
		info := officialTestInfo()
		info.SetEstimatePromptTokens(8)
		usage := openAIMediaStreamHandler(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, info)
		require.Equal(t, body, w.Body.String())
		require.True(t, info.StreamStatus.HasErrors())
		require.Equal(t, 8, usage.PromptTokens)
	}
}

func TestOfficialMediaSSECancellationUnblocksReader(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	info := officialTestInfo()
	done := make(chan struct{})
	go func() {
		defer close(done)
		openAIMediaStreamHandler(c, &http.Response{StatusCode: 200, Body: reader}, info)
	}()
	cancel()
	select {
	case <-done:
		require.True(t, info.StreamStatus.HasErrors())
	case <-time.After(time.Second):
		t.Fatal("cancelled SSE reader remained blocked")
	}
}

func TestOfficialMediaUsageAliasesAndZero(t *testing.T) {
	usage := parseMediaUsage([]byte(`{"input_tokens":100,"output_tokens":50,"input_tokens_details":{"cached_tokens":20,"cache_write_tokens":30,"image_tokens":10},"output_tokens_details":{"image_tokens":50}}`))
	require.NotNil(t, usage)
	require.Equal(t, 150, usage.TotalTokens)
	require.Equal(t, 30, usage.PromptTokensDetails.GetCacheCreationTokens())
	require.Equal(t, 50, usage.CompletionTokenDetails.ImageTokens)
	require.NotNil(t, parseMediaUsage([]byte(`{"total_tokens":0}`)))
	require.Nil(t, parseMediaUsage([]byte(`{"type":"duration","seconds":30}`)))
	var req dto.AudioRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"tts","input":"hi","voice":{"id":"voice_1"},"speed":0}`), &req))
	info := officialTestInfo()
	info.RelayMode = relayconstant.RelayModeAudioSpeech
	body, err := (&Adaptor{}).ConvertAudioRequest(nil, info, req)
	require.NoError(t, err)
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"voice":{"id":"voice_1"}`)
	require.Contains(t, string(raw), `"speed":0`)
}

type disconnectedMediaWriter struct{ *httptest.ResponseRecorder }

func (w disconnectedMediaWriter) Write([]byte) (int, error) {
	return 0, errors.New("client disconnected")
}

func TestOfficialMediaUsageSurvivesClientWriteFailure(t *testing.T) {
	w := disconnectedMediaWriter{httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", nil)
	info := officialTestInfo()
	info.SetEstimatePromptTokens(99)
	info.RelayMode = relayconstant.RelayModeAudioSpeech
	data := "data: {\"type\":\"speech.audio.done\",\"usage\":{\"input_tokens\":5,\"output_tokens\":10,\"total_tokens\":15}}\n\n"
	usage := openAIMediaStreamHandler(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(data))}, info)
	require.Equal(t, 15, usage.TotalTokens)
	require.Equal(t, 5, usage.PromptTokensDetails.TextTokens)
	require.Equal(t, 10, usage.CompletionTokenDetails.AudioTokens)
	require.True(t, info.StreamStatus.HasErrors())
}

func TestOfficialMediaJSONValidationAndTranscriptionUsage(t *testing.T) {
	for _, tc := range []struct {
		body, contentType string
		mode              int
		wantError         bool
	}{
		{`not JSON`, "application/json", relayconstant.RelayModeImagesGenerations, true},
		{`hello world`, "text/plain", relayconstant.RelayModeAudioTranscription, false},
		{`{"text":"hi","usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13,"input_token_details":{"audio_tokens":8,"text_tokens":2}}}`, "application/json", relayconstant.RelayModeAudioTranscription, false},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/", nil)
		info := officialTestInfo()
		info.RelayMode = tc.mode
		usage, err := openAIMediaJSONHandler(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{tc.contentType}}, Body: io.NopCloser(strings.NewReader(tc.body))}, info)
		if tc.wantError {
			require.NotNil(t, err)
			require.False(t, c.Writer.Written())
		} else {
			require.Nil(t, err)
			require.Equal(t, tc.body, w.Body.String())
			if tc.contentType == "application/json" {
				require.Equal(t, 8, usage.PromptTokensDetails.AudioTokens)
				require.Equal(t, 2, usage.PromptTokensDetails.TextTokens)
				require.Equal(t, 3, usage.CompletionTokenDetails.TextTokens)
			}
		}
	}
}
