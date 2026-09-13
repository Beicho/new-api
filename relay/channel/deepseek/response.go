package deepseek

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// DeepSeek's Messages endpoint is native Anthropic. Keep error events in SSE
// and distinguish an explicit final zero usage from missing usage.
func handleClaudeStream(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}
	defer service.CloseResponseBodyGracefully(resp)
	info.FinalRequestRelayFormat = types.RelayFormatClaude
	state := &claude.ClaudeResponseInfo{Usage: &dto.Usage{}, Model: info.UpstreamModelName}
	var finalUsage, terminal bool
	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var event dto.ClaudeResponse
		if err := common.UnmarshalJsonStr(data, &event); err != nil {
			sr.Stop(err)
			return
		}
		if event.Type == "error" {
			helper.ClaudeChunkData(c, event, data)
			terminal = true
			sr.Stop(fmt.Errorf("upstream Anthropic error: %v", event.Error))
			return
		}
		claude.FormatClaudeResponseInfo(&event, nil, state)
		if event.Type == "message_delta" && event.Usage != nil {
			// Distinguish absent fields from explicit zeros in the final usage.
			var envelope struct {
				Usage struct {
					InputTokens   *int `json:"input_tokens"`
					OutputTokens  *int `json:"output_tokens"`
					CacheRead     *int `json:"cache_read_input_tokens"`
					CacheCreation *int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			}
			if err := common.UnmarshalJsonStr(data, &envelope); err != nil {
				sr.Stop(err)
				return
			}
			if v := envelope.Usage.InputTokens; v != nil {
				state.Usage.PromptTokens = *v
			}
			if v := envelope.Usage.OutputTokens; v != nil {
				state.Usage.CompletionTokens = *v
				finalUsage = true
			}
			if v := envelope.Usage.CacheRead; v != nil {
				state.Usage.PromptTokensDetails.CachedTokens = *v
			}
			if v := envelope.Usage.CacheCreation; v != nil {
				state.Usage.PromptTokensDetails.CachedCreationTokens = *v
			}
			state.Usage.TotalTokens = state.Usage.PromptTokens + state.Usage.CompletionTokens
		}
		helper.ClaudeChunkData(c, event, data)
		if event.Type == "message_stop" {
			terminal = true
			sr.Done()
		}
	})
	if !terminal && !info.StreamStatus.HasErrors() {
		info.StreamStatus.RecordError("Anthropic stream ended without message_stop")
	}
	if !finalUsage {
		claude.HandleStreamFinalResponse(c, info, state)
	}
	state.Usage.UsageSemantic = "anthropic"
	return state.Usage, nil
}
