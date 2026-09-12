package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	if responsesResponse.HasImageGenerationCall() {
		c.Set("image_generation_call", true)
		c.Set("image_generation_call_quality", responsesResponse.GetQuality())
		c.Set("image_generation_call_size", responsesResponse.GetSize())
	}

	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := normalizeResponsesUsage(responsesResponse.Usage)
	if responsesResponse.Usage == nil && info != nil {
		var text strings.Builder
		for _, item := range responsesResponse.Output {
			for _, content := range item.Content {
				text.WriteString(content.Text)
			}
			text.WriteString(item.Arguments)
		}
		usage = *service.ResponseText2Usage(c, text.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	if info == nil || info.ResponsesUsageInfo == nil || info.ResponsesUsageInfo.BuiltInTools == nil {
		return &usage, nil
	}
	// 解析 Tools 用量
	for _, tool := range responsesResponse.Tools {
		buildToolinfo, ok := info.ResponsesUsageInfo.BuiltInTools[common.Interface2String(tool["type"])]
		if !ok || buildToolinfo == nil {
			logger.LogError(c, fmt.Sprintf("BuiltInTools not found for tool type: %v", tool["type"]))
			continue
		}
		buildToolinfo.CallCount++
	}
	return &usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var usage = &dto.Usage{}
	var responseTextBuilder strings.Builder
	var hasUsage, terminal bool

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			sr.Stop(err)
			return
		}
		sendResponsesStreamData(c, streamResponse, data)
		switch streamResponse.Type {
		case "response.completed", "response.incomplete", "response.failed":
			terminal = true
			if streamResponse.Response != nil {
				if streamResponse.Response.Usage != nil {
					*usage = normalizeResponsesUsage(streamResponse.Response.Usage)
					hasUsage = true
				}
				if streamResponse.Response.HasImageGenerationCall() {
					c.Set("image_generation_call", true)
					c.Set("image_generation_call_quality", streamResponse.Response.GetQuality())
					c.Set("image_generation_call_size", streamResponse.Response.GetSize())
				}
			}
			if streamResponse.Type == "response.failed" {
				failure := fmt.Errorf("upstream response.failed")
				if streamResponse.Response != nil && streamResponse.Response.Error != nil {
					failure = fmt.Errorf("upstream response.failed: %v", streamResponse.Response.Error)
				}
				sr.Stop(failure)
			} else {
				sr.Done()
			}
		case "error", "response.error":
			terminal = true
			// The native error event was already delivered. Do not append JSON or retry.
			sr.Stop(fmt.Errorf("upstream Responses error: %s", data))
		case "response.output_text.delta", "response.reasoning_text.delta", "response.reasoning_summary_text.delta",
			"response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
			// 处理输出文本
			responseTextBuilder.WriteString(streamResponse.Delta)
		case dto.ResponsesOutputTypeItemDone:
			// 函数调用处理
			if streamResponse.Item != nil {
				switch streamResponse.Item.Type {
				case dto.BuildInCallWebSearchCall:
					if info != nil && info.ResponsesUsageInfo != nil && info.ResponsesUsageInfo.BuiltInTools != nil {
						if webSearchTool, exists := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; exists && webSearchTool != nil {
							webSearchTool.CallCount++
						}
					}
				}
			}
		}
	})

	if !terminal && !info.StreamStatus.HasErrors() {
		info.StreamStatus.RecordError("Responses stream ended without a terminal event")
	}
	if !hasUsage {
		usage = service.ResponseText2Usage(c, responseTextBuilder.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
	}

	return usage, nil
}

func normalizeResponsesUsage(upstream *dto.Usage) dto.Usage {
	if upstream == nil {
		return dto.Usage{}
	}
	usage := *upstream
	usage.PromptTokens = upstream.InputTokens
	usage.CompletionTokens = upstream.OutputTokens
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	if upstream.InputTokensDetails != nil {
		usage.PromptTokensDetails = *upstream.InputTokensDetails
	}
	if upstream.OutputTokensDetails != nil {
		usage.CompletionTokenDetails = *upstream.OutputTokensDetails
	}
	return usage
}
