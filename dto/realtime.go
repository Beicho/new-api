package dto

import (
	"github.com/QuantumNous/new-api/types"
	"net/http"
	"strings"
)

func IsRealtimeBetaRequest(header http.Header) bool {
	for _, value := range header.Values("OpenAI-Beta") {
		for _, item := range strings.Split(value, ",") {
			if strings.TrimSpace(strings.ToLower(item)) == "realtime=v1" {
				return true
			}
		}
	}
	for _, value := range header.Values("Sec-WebSocket-Protocol") {
		for _, item := range strings.Split(value, ",") {
			if strings.TrimSpace(item) == "openai-beta.realtime-v1" {
				return true
			}
		}
	}
	return false
}

const (
	RealtimeEventTypeError              = "error"
	RealtimeEventTypeSessionUpdate      = "session.update"
	RealtimeEventTypeConversationCreate = "conversation.item.create"
	RealtimeEventTypeResponseCreate     = "response.create"
	RealtimeEventInputAudioBufferAppend = "input_audio_buffer.append"
)

const (
	RealtimeEventTypeResponseDone                   = "response.done"
	RealtimeEventResponseOutputAudioDelta           = "response.output_audio.delta"
	RealtimeEventResponseOutputAudioTranscriptDelta = "response.output_audio_transcript.delta"
	RealtimeEventResponseOutputTextDelta            = "response.output_text.delta"
	RealtimeEventTypeSessionUpdated                 = "session.updated"
	RealtimeEventTypeSessionCreated                 = "session.created"
	RealtimeEventResponseAudioDelta                 = "response.audio.delta"
	RealtimeEventResponseAudioTranscriptionDelta    = "response.audio_transcript.delta"
	RealtimeEventResponseFunctionCallArgumentsDelta = "response.function_call_arguments.delta"
	RealtimeEventResponseFunctionCallArgumentsDone  = "response.function_call_arguments.done"
	RealtimeEventConversationItemCreated            = "conversation.item.created"
)

type RealtimeEvent struct {
	EventId string `json:"event_id"`
	Type    string `json:"type"`
	//PreviousItemId string `json:"previous_item_id"`
	Session  *RealtimeSession   `json:"session,omitempty"`
	Item     *RealtimeItem      `json:"item,omitempty"`
	Error    *types.OpenAIError `json:"error,omitempty"`
	Response *RealtimeResponse  `json:"response,omitempty"`
	Delta    string             `json:"delta,omitempty"`
	Audio    string             `json:"audio,omitempty"`
}

type RealtimeResponse struct {
	ID    string         `json:"id"`
	Usage *RealtimeUsage `json:"usage"`
}

type RealtimeUsage struct {
	TotalTokens        int                `json:"total_tokens"`
	InputTokens        int                `json:"input_tokens"`
	OutputTokens       int                `json:"output_tokens"`
	InputTokenDetails  InputTokenDetails  `json:"input_token_details"`
	OutputTokenDetails OutputTokenDetails `json:"output_token_details"`
}

type RealtimeSession struct {
	Type                    *string                 `json:"type,omitempty"`
	Audio                   *RealtimeAudio          `json:"audio,omitempty"`
	Modalities              []string                `json:"modalities"`
	Instructions            string                  `json:"instructions"`
	Voice                   string                  `json:"voice"`
	InputAudioFormat        string                  `json:"input_audio_format"`
	OutputAudioFormat       string                  `json:"output_audio_format"`
	InputAudioTranscription InputAudioTranscription `json:"input_audio_transcription"`
	TurnDetection           interface{}             `json:"turn_detection"`
	Tools                   []RealTimeTool          `json:"tools"`
	ToolChoice              string                  `json:"tool_choice"`
	Temperature             float64                 `json:"temperature"`
	//MaxResponseOutputTokens int                     `json:"max_response_output_tokens"`
}

type RealtimeAudio struct {
	Input  *RealtimeAudioConfig `json:"input,omitempty"`
	Output *RealtimeAudioConfig `json:"output,omitempty"`
}

type RealtimeAudioConfig struct {
	Format *RealtimeAudioFormat `json:"format,omitempty"`
}

type RealtimeAudioFormat struct {
	Type string `json:"type"`
	Rate *int   `json:"rate,omitempty"`
}

func (s *RealtimeSession) AudioFormats() (string, string) {
	in, out := s.InputAudioFormat, s.OutputAudioFormat
	if s.Audio != nil {
		if s.Audio.Input != nil && s.Audio.Input.Format != nil {
			in = s.Audio.Input.Format.LegacyName()
		}
		if s.Audio.Output != nil && s.Audio.Output.Format != nil {
			out = s.Audio.Output.Format.LegacyName()
		}
	}
	return in, out
}

func (f *RealtimeAudioFormat) LegacyName() string {
	switch f.Type {
	case "audio/pcm":
		return "pcm16"
	case "audio/pcmu":
		return "g711_ulaw"
	case "audio/pcma":
		return "g711_alaw"
	default:
		return f.Type
	}
}

type InputAudioTranscription struct {
	Model string `json:"model"`
}

type RealTimeTool struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type RealtimeItem struct {
	Id        string            `json:"id"`
	Type      string            `json:"type"`
	Status    string            `json:"status"`
	Role      string            `json:"role"`
	Content   []RealtimeContent `json:"content"`
	Name      *string           `json:"name,omitempty"`
	ToolCalls any               `json:"tool_calls,omitempty"`
	CallId    string            `json:"call_id,omitempty"`
}
type RealtimeContent struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	Audio      string `json:"audio,omitempty"` // Base64-encoded audio bytes.
	Transcript string `json:"transcript,omitempty"`
}
