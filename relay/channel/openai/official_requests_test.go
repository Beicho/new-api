package openai

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOfficialImageJSONRequestThroughAdaptor(t *testing.T) {
	observed := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" || r.URL.Path != "/v1/images/edits" {
			http.Error(w, "bad upstream request", 400)
			return
		}
		body, _ := io.ReadAll(r.Body)
		observed <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"AAAA"}],"usage":{"input_tokens":5,"output_tokens":10,"total_tokens":15,"output_tokens_details":{"image_tokens":10}}}`)
	}))
	defer server.Close()
	service.InitHttpClient()
	body := `{"model":"alias","prompt":"edit","images":[{"image_url":"https://example.com/image.png"}],"mask":{"file_id":"mask"},"input_fidelity":"high","n":0,"stream":false,"output_compression":0}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/images/edits", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	defer common.CleanupBodyStorage(c)
	c.Set("model_mapping", `{"alias":"mapped"}`)
	req, err := helper.GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
	require.NoError(t, err)
	info := officialTestInfo()
	info.RelayMode = relayconstant.RelayModeImagesEdits
	info.OriginModelName = "alias"
	info.ChannelBaseUrl = server.URL
	info.RequestURLPath = "/v1/images/edits"
	info.ApiKey = "test-key"
	info.ParamOverride = map[string]any{"quality": "high"}
	require.NoError(t, helper.ModelMappedHelper(c, info, req))
	adaptor := &Adaptor{}
	adaptor.Init(info)
	converted, err := adaptor.ConvertImageRequest(c, info, *req)
	require.NoError(t, err)
	raw, err := common.Marshal(converted)
	require.NoError(t, err)
	raw, err = relaycommon.ApplyParamOverrideWithRelayInfo(raw, info)
	require.NoError(t, err)
	resp, err := adaptor.DoRequest(c, info, bytes.NewReader(raw))
	require.NoError(t, err)
	usage, apiErr := adaptor.DoResponse(c, resp.(*http.Response), info)
	require.Nil(t, apiErr)
	require.Equal(t, 15, usage.(*dto.Usage).TotalTokens)
	require.Equal(t, 10, usage.(*dto.Usage).CompletionTokenDetails.ImageTokens)
	require.JSONEq(t, `{"model":"mapped","prompt":"edit","images":[{"image_url":"https://example.com/image.png"}],"mask":{"file_id":"mask"},"input_fidelity":"high","n":0,"stream":false,"output_compression":0,"quality":"high"}`, string(<-observed))
}

func TestOfficialMultipartConversionKeepsFilesAndFields(t *testing.T) {
	for _, audio := range []bool{false, true} {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		for key, value := range map[string]string{"model": "alias", "stream": "true", "partial_images": "0", "input_fidelity": "high", "known_speaker_names[]": "Alice", "known_speaker_references[]": "data:audio/wav;base64,AAAA", "chunking_strategy": "auto"} {
			require.NoError(t, form.WriteField(key, value))
		}
		fileField := "image[]"
		if audio {
			fileField = "file"
		}
		file, err := form.CreateFormFile(fileField, "test.png")
		require.NoError(t, err)
		_, err = file.Write([]byte("test-image"))
		require.NoError(t, err)
		require.NoError(t, form.Close())
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/", bytes.NewReader(body.Bytes()))
		c.Request.Header.Set("Content-Type", form.FormDataContentType())
		common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
		info := officialTestInfo()
		var converted io.Reader
		if audio {
			info.RelayMode = relayconstant.RelayModeAudioTranscription
			req, err := helper.GetAndValidAudioRequest(c, info.RelayMode)
			require.NoError(t, err)
			req.Model = "mapped"
			converted, err = (&Adaptor{}).ConvertAudioRequest(c, info, *req)
			require.NoError(t, err)
		} else {
			info.RelayMode = relayconstant.RelayModeImagesEdits
			req, err := helper.GetAndValidOpenAIImageRequest(c, info.RelayMode)
			require.NoError(t, err)
			req.Model = "mapped"
			value, err := (&Adaptor{}).ConvertImageRequest(c, info, *req)
			require.NoError(t, err)
			converted = value.(io.Reader)
		}
		out := httptest.NewRequest("POST", "/", converted)
		out.Header.Set("Content-Type", c.GetHeader("Content-Type"))
		require.NoError(t, out.ParseMultipartForm(1<<20))
		require.Equal(t, "mapped", out.FormValue("model"))
		require.Equal(t, "true", out.FormValue("stream"))
		require.Equal(t, "0", out.FormValue("partial_images"))
		require.Equal(t, "Alice", out.FormValue("known_speaker_names[]"))
		require.Equal(t, "data:audio/wav;base64,AAAA", out.FormValue("known_speaker_references[]"))
		if !audio {
			fileField = "image"
		}
		uploaded, err := out.MultipartForm.File[fileField][0].Open()
		require.NoError(t, err)
		data, err := io.ReadAll(uploaded)
		uploaded.Close()
		require.NoError(t, err)
		require.Equal(t, "test-image", string(data))
		out.MultipartForm.RemoveAll()
		if c.Request.MultipartForm != nil {
			c.Request.MultipartForm.RemoveAll()
		}
		common.CleanupBodyStorage(c)
	}
}

func TestOfficialResponsesCountsActualTools(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 3
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	output := `[{"type":"web_search_call","id":"w1","status":"completed"},{"type":"web_search_call","id":"w2","status":"completed"},{"type":"file_search_call","id":"f1","status":"completed"}]`
	snapshot := `{"model":"test","tools":[{"type":"web_search"},{"type":"file_search"}],"output":` + output + `,"usage":{"input_tokens":100,"output_tokens":10,"input_tokens_details":{"cache_write_tokens":30}}}`
	for _, stream := range []bool{false, true} {
		info := officialTestInfo()
		info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{}}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		var usage *dto.Usage
		if stream {
			data := "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"web_search_call\",\"id\":\"w1\",\"status\":\"completed\"}}\n\n" + "data: {\"type\":\"response.completed\",\"response\":" + snapshot + "}\n\n"
			var err any
			usage, err = OaiResponsesStreamHandler(c, info, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(data))})
			require.Nil(t, err)
		} else {
			var err any
			usage, err = OaiResponsesHandler(c, info, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(snapshot))})
			require.Nil(t, err)
		}
		require.Equal(t, 2, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
		require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
		require.Equal(t, 30, usage.PromptTokensDetails.GetCacheCreationTokens())
	}
	info := officialTestInfo()
	info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	_, err := OaiResponsesHandler(c, info, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"tools":[{"type":"file_search"}],"output":[],"usage":{"input_tokens":1}}`))})
	require.Nil(t, err)
	require.Empty(t, info.ResponsesUsageInfo.BuiltInTools)
}

func TestOfficialChatReassemblyPreservesMediaAndCustomTools(t *testing.T) {
	message := `{"role":"assistant","content":null,"audio":{"id":"audio_1","data":"AAAA","expires_at":100,"transcript":"hello"},"refusal":"","function_call":{"name":"legacy","arguments":"{}"},"tool_calls":[{"id":"call_1","type":"custom","custom":{"name":"run","input":"hello"}}]}`
	for _, stream := range []bool{false, true} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		info := officialTestInfo()
		info.RelayFormat = types.RelayFormatOpenAI
		info.RelayMode = relayconstant.RelayModeChatCompletions
		info.ChannelSetting.ForceFormat = true
		key := "message"
		if stream {
			key = "delta"
		}
		body := `{"id":"chat_1","model":"test","service_tier":"default","obfuscation":"padding","choices":[{"index":0,"` + key + `":` + message + `}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"prompt_tokens_details":{"cache_write_tokens":3}}}`
		if stream {
			require.NoError(t, sendStreamData(c, info, body, true, false))
		} else {
			usage, err := OpenaiHandler(c, info, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))})
			require.Nil(t, err)
			require.Equal(t, 3, usage.PromptTokensDetails.GetCacheCreationTokens())
		}
		var envelope map[string]any
		require.NoError(t, common.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(w.Body.String(), "data: "))), &envelope))
		got := envelope["choices"].([]any)[0].(map[string]any)[key].(map[string]any)
		require.Equal(t, "audio_1", got["audio"].(map[string]any)["id"])
		require.Equal(t, "", got["refusal"])
		require.Equal(t, "legacy", got["function_call"].(map[string]any)["name"])
		call := got["tool_calls"].([]any)[0].(map[string]any)
		require.Equal(t, "hello", call["custom"].(map[string]any)["input"])
		require.NotContains(t, call, "function")
		require.Equal(t, "default", envelope["service_tier"])
		if stream {
			require.Equal(t, "padding", envelope["obfuscation"])
		}
	}
}

func TestOfficialResponsesToChatStreamFieldsAndUsage(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 3
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := officialTestInfo()
	info.RelayFormat = types.RelayFormatOpenAI
	info.ShouldIncludeUsage = true
	info.SetEstimatePromptTokens(99)
	events := []string{
		`{"type":"response.refusal.delta","delta":"cannot comply"}`,
		`{"type":"response.output_item.added","item":{"id":"item_1","type":"custom_tool_call","call_id":"call_1","name":"run","input":""}}`,
		`{"type":"response.custom_tool_call_input.delta","item_id":"item_1","delta":"hello"}`,
		`{"type":"response.completed","response":{"output":[{"type":"web_search_call","id":"w1","status":"completed"}],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`,
	}
	data := "data: " + strings.Join(events, "\n\ndata: ") + "\n\n"
	usage, err := OaiResponsesToChatStreamHandler(c, info, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(data))})
	require.Nil(t, err)
	require.Zero(t, usage.TotalTokens, "explicit zero usage must not be replaced with an estimate")
	require.Contains(t, w.Body.String(), `"refusal":"cannot comply"`)
	require.Contains(t, w.Body.String(), `"custom":{"input":"hello"}`)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
}

func TestOfficialMediaDoesNotCommitBeforeUpstreamStatus(t *testing.T) {
	service.InitHttpClient()
	settings := operation_setting.GetGeneralSetting()
	previous := *settings
	settings.PingIntervalEnabled = true
	settings.PingIntervalSeconds = 1
	t.Cleanup(func() { *settings = previous })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1200 * time.Millisecond)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
	info := officialTestInfo()
	info.IsStream = true
	info.RelayMode = relayconstant.RelayModeImagesGenerations
	info.ChannelBaseUrl = server.URL
	info.RequestURLPath = "/v1/images/generations"
	resp, err := (&Adaptor{}).DoRequest(c, info, strings.NewReader(`{"stream":true}`))
	require.NoError(t, err)
	defer resp.(*http.Response).Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.(*http.Response).StatusCode)
	require.False(t, c.Writer.Written(), "the HTTP error must remain writable even when keepalive is enabled")
}

func TestOfficialMultipartCancellationBeforeUpstreamHeaders(t *testing.T) {
	service.InitHttpClient()
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	defer server.CloseClientConnections()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/audio/transcriptions", nil).WithContext(ctx)
	info := officialTestInfo()
	info.RelayMode = relayconstant.RelayModeAudioTranscription
	info.ChannelBaseUrl = server.URL
	info.RequestURLPath = "/v1/audio/transcriptions"
	done := make(chan error, 1)
	go func() { _, err := (&Adaptor{}).DoRequest(c, info, strings.NewReader("form-data")); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream did not receive request")
	}
	cancel()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("multipart request ignored cancellation")
	}
}
