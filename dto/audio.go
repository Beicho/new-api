package dto

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type AudioRequest struct {
	Model          string          `json:"model"`
	Input          string          `json:"input"`
	Voice          AudioVoice      `json:"voice"`
	Instructions   string          `json:"instructions,omitempty"`
	ResponseFormat string          `json:"response_format,omitempty"`
	Speed          *float64        `json:"speed,omitempty"`
	StreamFormat   string          `json:"stream_format,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	// vllm-omini
	TaskType                json.RawMessage `json:"task_type,omitempty"`
	Language                json.RawMessage `json:"language,omitempty"`
	RefAudio                json.RawMessage `json:"ref_audio,omitempty"`
	RefText                 json.RawMessage `json:"ref_text,omitempty"`
	XVectorOnlyMode         json.RawMessage `json:"x_vector_only_mode,omitempty"`
	MaxNewTokens            json.RawMessage `json:"max_new_tokens,omitempty"`
	InitialCodecChunkFrames json.RawMessage `json:"initial_codec_chunk_frames,omitempty"`
	Stream                  *bool           `json:"stream,omitempty"`
}

// AudioVoice represents either a built-in voice name or a custom voice reference.
type AudioVoice struct {
	Name string
	ID   string
}

func (v AudioVoice) String() string { return v.Name }

func (v AudioVoice) MarshalJSON() ([]byte, error) {
	if v.ID != "" {
		if v.Name != "" {
			return nil, fmt.Errorf("voice must be a name or an id, not both")
		}
		return common.Marshal(struct {
			ID string `json:"id"`
		}{v.ID})
	}
	return common.Marshal(v.Name)
}

func (v *AudioVoice) UnmarshalJSON(data []byte) error {
	*v = AudioVoice{}
	if common.GetJsonType(data) == "string" {
		return common.Unmarshal(data, &v.Name)
	}
	var ref struct {
		ID string `json:"id"`
	}
	if err := common.Unmarshal(data, &ref); err != nil {
		return fmt.Errorf("voice must be a string or an object with id: %w", err)
	}
	if strings.TrimSpace(ref.ID) == "" {
		return fmt.Errorf("voice object requires a non-empty id")
	}
	v.ID = ref.ID
	return nil
}

func (r *AudioRequest) GetTokenCountMeta() *types.TokenCountMeta {
	meta := &types.TokenCountMeta{
		CombineText: r.Input,
		TokenType:   types.TokenTypeTextNumber,
	}
	if strings.Contains(r.Model, "gpt") {
		meta.TokenType = types.TokenTypeTokenizer
	}
	return meta
}

func (r *AudioRequest) IsStream(c *gin.Context) bool {
	return r.StreamFormat == "sse" || (r.Stream != nil && *r.Stream)
}

func (r *AudioRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}

type AudioResponse struct {
	Text string `json:"text"`
}

type WhisperVerboseJSONResponse struct {
	Task     string    `json:"task,omitempty"`
	Language string    `json:"language,omitempty"`
	Duration float64   `json:"duration,omitempty"`
	Text     string    `json:"text,omitempty"`
	Segments []Segment `json:"segments,omitempty"`
}

type Segment struct {
	Id               int     `json:"id"`
	Seek             int     `json:"seek"`
	Start            float64 `json:"start"`
	End              float64 `json:"end"`
	Text             string  `json:"text"`
	Tokens           []int   `json:"tokens"`
	Temperature      float64 `json:"temperature"`
	AvgLogprob       float64 `json:"avg_logprob"`
	CompressionRatio float64 `json:"compression_ratio"`
	NoSpeechProb     float64 `json:"no_speech_prob"`
}
