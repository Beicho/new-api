package openai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// Reader goroutines only read sockets. The event loop owns all session and usage
// state, so a disconnect cannot race with settlement of response.done.
func relayRealtimeGA(c *gin.Context, info *relaycommon.RelayInfo, consume func(*dto.RealtimeUsage) error) *dto.RealtimeUsage {
	type message struct {
		upstream bool
		kind     int
		data     []byte
		err      error
	}
	incoming := make(chan message, 16)
	stopCancel := context.AfterFunc(c.Request.Context(), func() { _ = info.ClientWs.Close(); _ = info.TargetWs.Close() })
	defer stopCancel()
	done := make(chan struct{})
	var readers sync.WaitGroup
	read := func(conn *websocket.Conn, upstream bool) {
		defer readers.Done()
		for {
			kind, data, err := conn.ReadMessage()
			select {
			case incoming <- message{upstream, kind, data, err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	}
	readers.Add(2)
	go read(info.ClientWs, false)
	go read(info.TargetWs, true)
	defer func() { close(done); _ = info.ClientWs.Close(); _ = info.TargetWs.Close(); readers.Wait() }()
	total, estimated := &dto.RealtimeUsage{}, &dto.RealtimeUsage{}
	settled := make(map[string]bool)
	settle := func(usage *dto.RealtimeUsage) bool {
		if usage == nil {
			return true
		}
		addRealtimeUsage(total, usage)
		if err := consume(usage); err != nil {
			logger.LogError(c, fmt.Sprintf("Realtime settlement failed: %v", err))
			return false
		}
		return true
	}
	defer func() {
		if estimated.TotalTokens > 0 {
			logger.LogWarn(c, "Realtime final usage unavailable; using local estimate")
			settle(estimated)
		}
	}()
	for {
		select {
		case <-c.Request.Context().Done():
			return total
		case msg := <-incoming:
			if msg.err != nil {
				return total
			}
			target := info.TargetWs
			if msg.upstream {
				target = info.ClientWs
			}
			payload := msg.data
			if msg.upstream {
				payload = relaycommon.RewriteClientResponseBytes(c, payload)
			}
			_ = target.SetWriteDeadline(time.Now().Add(30 * time.Second))
			writeErr := target.WriteMessage(msg.kind, payload)
			if msg.kind != websocket.TextMessage {
				if writeErr != nil {
					return total
				}
				continue
			}
			var event dto.RealtimeEvent
			if err := common.Unmarshal(msg.data, &event); err != nil {
				logger.LogWarn(c, "unrecognized Realtime event")
				if writeErr != nil {
					return total
				}
				continue
			}
			if event.Session != nil {
				in, out := event.Session.AudioFormats()
				if in != "" {
					info.InputAudioFormat = in
				}
				if out != "" {
					info.OutputAudioFormat = out
				}
				if event.Session.Tools != nil {
					info.RealtimeTools = event.Session.Tools
				}
			}
			if msg.upstream && event.Type == dto.RealtimeEventTypeResponseDone {
				if event.Response != nil && event.Response.ID != "" && settled[event.Response.ID] {
					if writeErr != nil {
						return total
					}
					continue
				}
				actual := estimated
				if event.Response != nil && event.Response.Usage != nil {
					actual = event.Response.Usage
				} else {
					logger.LogWarn(c, "Realtime response.done omitted usage; using local estimate")
					text, audio, err := service.CountTokenRealtime(info, event, info.UpstreamModelName)
					if err == nil {
						estimated.InputTokens += text + audio
						estimated.TotalTokens += text + audio
						estimated.InputTokenDetails.TextTokens += text
						estimated.InputTokenDetails.AudioTokens += audio
					}
				}
				estimated = &dto.RealtimeUsage{}
				if event.Response != nil && event.Response.ID != "" {
					settled[event.Response.ID] = true
				}
				if !settle(actual) {
					return total
				}
				info.IsFirstRequest = false
				if writeErr != nil {
					return total
				}
				continue
			}
			// Server echoes of client-created input are not additional input tokens.
			if msg.upstream && (event.Type == dto.RealtimeEventConversationItemCreated || event.Type == "conversation.item.added") {
				if writeErr != nil {
					return total
				}
				continue
			}
			text, audio, err := service.CountTokenRealtime(info, event, info.UpstreamModelName)
			if err != nil {
				logger.LogWarn(c, err.Error())
				if writeErr != nil {
					return total
				}
				continue
			}
			estimated.TotalTokens += text + audio
			if msg.upstream {
				estimated.OutputTokens += text + audio
				estimated.OutputTokenDetails.TextTokens += text
				estimated.OutputTokenDetails.AudioTokens += audio
			} else {
				estimated.InputTokens += text + audio
				estimated.InputTokenDetails.TextTokens += text
				estimated.InputTokenDetails.AudioTokens += audio
			}
			if writeErr != nil {
				return total
			}
		}
	}
}

func addRealtimeUsage(total, usage *dto.RealtimeUsage) {
	total.TotalTokens += usage.TotalTokens
	total.InputTokens += usage.InputTokens
	total.OutputTokens += usage.OutputTokens
	total.InputTokenDetails.TextTokens += usage.InputTokenDetails.TextTokens
	total.InputTokenDetails.AudioTokens += usage.InputTokenDetails.AudioTokens
	total.InputTokenDetails.CachedTokens += usage.InputTokenDetails.CachedTokens
	if usage.InputTokenDetails.CacheWriteTokens != nil || usage.InputTokenDetails.CachedCreationTokens != 0 {
		cacheWrite := total.InputTokenDetails.GetCacheCreationTokens() + usage.InputTokenDetails.GetCacheCreationTokens()
		total.InputTokenDetails.CacheWriteTokens = &cacheWrite
	}
	total.OutputTokenDetails.TextTokens += usage.OutputTokenDetails.TextTokens
	total.OutputTokenDetails.AudioTokens += usage.OutputTokenDetails.AudioTokens
}
