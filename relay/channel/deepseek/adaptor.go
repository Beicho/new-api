package deepseek

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type Adaptor struct {
	useBeta bool
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, req *dto.ClaudeRequest) (any, error) {
	if req == nil {
		return nil, errors.New("request is nil")
	}
	adaptor := claude.Adaptor{}
	convertedRequest, err := adaptor.ConvertClaudeRequest(c, info, req)
	if err != nil {
		return nil, err
	}
	claudeRequest, ok := convertedRequest.(*dto.ClaudeRequest)
	if !ok {
		return convertedRequest, nil
	}
	if err := applyDeepSeekV4ClaudeThinkingSuffix(info, claudeRequest); err != nil {
		return nil, err
	}
	return claudeRequest, nil
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
	a.useBeta = false
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	base, err := url.Parse(info.ChannelBaseUrl)
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") {
		return "", errors.New("invalid DeepSeek base URL")
	}
	base.Path = strings.TrimRight(base.Path, "/")
	configuredBeta := strings.HasSuffix(base.Path, "/beta")
	for _, suffix := range []string{"/anthropic/v1", "/anthropic", "/v1", "/beta"} {
		if strings.HasSuffix(base.Path, suffix) {
			base.Path = strings.TrimSuffix(base.Path, suffix)
			break
		}
	}
	var endpoint string
	switch {
	case info.RelayFormat == types.RelayFormatClaude:
		endpoint = "/anthropic/v1/messages"
	case info.RelayMode == constant.RelayModeResponses:
		endpoint = "/responses"
	case info.RelayMode == constant.RelayModeCompletions:
		endpoint = "/beta/completions"
	case info.RelayMode == constant.RelayModeChatCompletions:
		endpoint = "/chat/completions"
		if configuredBeta || a.useBeta {
			endpoint = "/beta" + endpoint
		}
	default:
		return "", errors.New("unsupported DeepSeek endpoint")
	}
	base.Path += endpoint
	base.RawPath = ""
	base.Fragment = ""
	return base.String(), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	if info.RelayFormat == types.RelayFormatClaude {
		adaptor := claude.Adaptor{}
		return adaptor.SetupRequestHeader(c, req, info)
	}
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("Authorization", "Bearer "+info.ApiKey)
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	if err := applyDeepSeekV4OpenAIThinkingSuffix(info, request); err != nil {
		return nil, err
	}
	if request.MaxTokens == nil {
		request.MaxTokens = request.MaxCompletionTokens
	}
	request.MaxCompletionTokens = nil
	if info != nil {
		info.ReasoningEffort = request.ReasoningEffort
	}

	return request, nil
}

func applyDeepSeekV4OpenAIThinkingSuffix(info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) error {
	modelName := request.Model
	if info != nil && info.ChannelMeta != nil && info.UpstreamModelName != "" {
		modelName = info.UpstreamModelName
	}
	baseModel, thinkingType, effort, ok := reasoning.ParseDeepSeekV4ThinkingSuffix(modelName)
	if !ok {
		return nil
	}
	thinking, err := common.Marshal(map[string]string{
		"type": thinkingType,
	})
	if err != nil {
		return fmt.Errorf("error marshalling thinking: %w", err)
	}
	request.Model = baseModel
	request.THINKING = thinking
	request.ReasoningEffort = effort
	if info != nil {
		if info.ChannelMeta != nil {
			info.UpstreamModelName = baseModel
		}
		info.ReasoningEffort = effort
	}
	return nil
}

func applyDeepSeekV4ClaudeThinkingSuffix(info *relaycommon.RelayInfo, request *dto.ClaudeRequest) error {
	modelName := request.Model
	if info != nil && info.ChannelMeta != nil && info.UpstreamModelName != "" {
		modelName = info.UpstreamModelName
	}
	baseModel, thinkingType, effort, ok := reasoning.ParseDeepSeekV4ThinkingSuffix(modelName)
	if !ok {
		return nil
	}
	request.Model = baseModel
	request.Thinking = &dto.Thinking{Type: thinkingType}
	var config map[string]any
	if len(request.OutputConfig) > 0 {
		if err := common.Unmarshal(request.OutputConfig, &config); err != nil {
			return fmt.Errorf("invalid output_config: %w", err)
		}
	}
	if config == nil {
		config = make(map[string]any)
	}
	delete(config, "effort")
	if effort != "" {
		config["effort"] = effort
	}
	request.OutputConfig = nil
	if len(config) > 0 {
		outputConfig, err := common.Marshal(config)
		if err != nil {
			return fmt.Errorf("error marshalling output_config: %w", err)
		}
		request.OutputConfig = outputConfig
	}
	if info != nil {
		if info.ChannelMeta != nil {
			info.UpstreamModelName = baseModel
		}
		info.ReasoningEffort = effort
	}
	return nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	modelName := request.Model
	if info != nil && info.ChannelMeta != nil && info.UpstreamModelName != "" {
		modelName = info.UpstreamModelName
	}
	baseModel, thinkingType, effort, ok := reasoning.ParseDeepSeekV4ThinkingSuffix(modelName)
	if ok {
		request.Model = baseModel
		if thinkingType == "disabled" {
			effort = "none"
		}
		if request.Reasoning == nil {
			request.Reasoning = &dto.Reasoning{}
		}
		request.Reasoning.Effort = effort
		if info != nil && info.ChannelMeta != nil {
			info.UpstreamModelName = baseModel
		}
	}
	if info != nil && request.Reasoning != nil {
		info.ReasoningEffort = request.Reasoning.Effort
	}
	return &request, nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	// Inspect the final body, after parameter overrides (also in passthrough mode).
	// Do not re-marshal it: tool schemas, image blocks and unknown fields must survive.
	if info.RelayMode == constant.RelayModeChatCompletions && info.RelayFormat == types.RelayFormatOpenAI {
		body, err := io.ReadAll(requestBody)
		if err != nil {
			return nil, err
		}
		var request struct {
			Messages []struct {
				Prefix *bool `json:"prefix"`
			} `json:"messages"`
			Tools []struct {
				Function struct {
					Strict *bool `json:"strict"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := common.Unmarshal(body, &request); err != nil {
			return nil, fmt.Errorf("invalid DeepSeek chat request: %w", err)
		}
		a.useBeta = false
		for _, message := range request.Messages {
			a.useBeta = a.useBeta || (message.Prefix != nil && *message.Prefix)
		}
		for _, tool := range request.Tools {
			a.useBeta = a.useBeta || (tool.Function.Strict != nil && *tool.Function.Strict)
		}
		requestBody = bytes.NewReader(body)
	}
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	switch info.RelayFormat {
	case types.RelayFormatClaude:
		if info.IsStream {
			return handleClaudeStream(c, resp, info)
		}
		adaptor := claude.Adaptor{}
		return adaptor.DoResponse(c, resp, info)
	default:
		adaptor := openai.Adaptor{}
		return adaptor.DoResponse(c, resp, info)
	}
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
