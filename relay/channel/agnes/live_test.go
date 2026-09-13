package agnes_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/agnes"
	taskagnes "github.com/QuantumNous/new-api/relay/channel/task/agnes"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Explicit opt-in only. No automatic retries of billable POSTs. Each invocation
// runs just one suite: text, images, or videos. Keys and media bodies are not logged.
func TestAgnesLive(t *testing.T) {
	if os.Getenv("AGNES_LIVE_TEST") != "1" {
		t.Skip("set AGNES_LIVE_TEST=1 and AGNES_API_KEY to run")
	}
	key := os.Getenv("AGNES_API_KEY")
	require.NotEmpty(t, key)
	service.InitHttpClient()
	suite := os.Getenv("AGNES_LIVE_SUITE")
	if suite == "retrieve" {
		// Recheck existing tasks without submitting any additional billable work.
		var tasks []struct {
			Model   string `json:"model"`
			VideoID string `json:"video_id"`
		}
		require.NoError(t, common.UnmarshalJsonStr(os.Getenv("AGNES_LIVE_RETRIEVALS"), &tasks))
		require.NotEmpty(t, tasks)
		for _, task := range tasks {
			t.Run(task.Model, func(t *testing.T) {
				a := &taskagnes.TaskAdaptor{}
				resp, err := a.FetchTask("https://apihub.agnes-ai.com", key, map[string]any{"video_id": task.VideoID, "model_name": task.Model}, "")
				require.NoError(t, err)
				defer resp.Body.Close()
				require.Equal(t, 200, resp.StatusCode)
				data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
				require.NoError(t, err)
				result, err := a.ParseTaskResult(data)
				require.NoError(t, err)
				require.Equal(t, "SUCCESS", result.Status)
				require.NotEmpty(t, result.Url)
				public, err := a.ConvertToOpenAIVideo(&model.Task{TaskID: "public", Status: model.TaskStatusSuccess, Data: data})
				require.NoError(t, err)
				require.Contains(t, string(public), result.Url)
				t.Log("completed task URL and public response verified")
			})
		}
		return
	}
	if suite == "videos" {
		for _, model := range []string{agnes.ModelVideoV20, agnes.ModelVideo25, agnes.ModelVideo25Flash} {
			t.Run(model, func(t *testing.T) { liveVideo(t, key, model) })
		}
		return
	}
	if suite == "images" {
		for _, base64 := range []bool{false, true} {
			t.Run(fmt.Sprint("base64=", base64), func(t *testing.T) {
				info := liveInfo(key, "/v1/images/generations", relayconstant.RelayModeImagesGenerations)
				var request dto.ImageRequest
				require.NoError(t, common.Unmarshal([]byte(fmt.Sprintf(`{"model":"agnes-image-2.5-flash","prompt":"A small blue circle on white paper.","size":"1K","ratio":"1:1","return_base64":%t}`, base64)), &request))
				c := liveContext(info.RequestURLPath, "{}")
				a := &agnes.Adaptor{}
				converted, err := a.ConvertImageRequest(c, info, request)
				require.NoError(t, err)
				body, err := common.Marshal(converted)
				require.NoError(t, err)
				response := liveRequest(t, a, c, info, body)
				var result struct {
					Data []struct {
						URL    string `json:"url"`
						Base64 string `json:"b64_json"`
					} `json:"data"`
				}
				require.NoError(t, common.Unmarshal(response, &result))
				require.Len(t, result.Data, 1)
				if base64 {
					require.NotEmpty(t, result.Data[0].Base64)
				} else {
					require.NotEmpty(t, result.Data[0].URL)
				}
				t.Log("one image returned in requested format")
			})
		}
		return
	}
	for _, tc := range []struct {
		name, path, body string
		mode             int
		format           types.RelayFormat
	}{
		{"chat", "/v1/chat/completions", `{"model":"agnes-2.5-flash","messages":[{"role":"user","content":"Reply OK."}],"max_tokens":64,"temperature":0,"chat_template_kwargs":{"enable_thinking":false}}`, relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAI},
		{"stream_usage", "/v1/chat/completions", `{"model":"agnes-2.5-flash","messages":[{"role":"user","content":"Reply OK."}],"max_tokens":64,"stream":true,"stream_options":{"include_usage":true},"chat_template_kwargs":{"enable_thinking":false}}`, relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAI},
		{"responses", "/v1/responses", `{"model":"agnes-2.5-flash","input":"Reply OK.","max_output_tokens":64}`, relayconstant.RelayModeResponses, types.RelayFormatOpenAI},
		{"messages", "/v1/messages", `{"model":"agnes-2.5-flash","messages":[{"role":"user","content":"Reply OK."}],"max_tokens":64,"temperature":0}`, relayconstant.RelayModeChatCompletions, types.RelayFormatClaude},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := liveInfo(key, tc.path, tc.mode)
			info.RelayFormat = tc.format
			response := liveRequest(t, &agnes.Adaptor{}, liveContext(tc.path, tc.body), info, []byte(tc.body))
			if tc.name == "stream_usage" {
				require.Contains(t, string(response), "[DONE]")
				found := false
				for _, line := range strings.Split(string(response), "\n") {
					line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
					var chunk struct {
						Usage *dto.Usage `json:"usage"`
					}
					if common.Unmarshal([]byte(line), &chunk) == nil && chunk.Usage != nil && chunk.Usage.TotalTokens > 0 {
						found = true
					}
				}
				require.True(t, found, "upstream did not return streaming token usage")
			} else {
				var result map[string]any
				require.NoError(t, common.Unmarshal(response, &result))
				require.Nil(t, result["error"])
				require.NotNil(t, result["usage"])
			}
			t.Log("HTTP 200 and token usage verified")
		})
	}
}

func liveInfo(key, path string, mode int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{RequestURLPath: path, RelayMode: mode, RelayFormat: types.RelayFormatOpenAI, ChannelMeta: &relaycommon.ChannelMeta{ApiKey: key, ChannelType: constant.ChannelTypeAgnesAI, ChannelBaseUrl: "https://apihub.agnes-ai.com"}}
}

func liveContext(path, body string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

func liveRequest(t *testing.T, a *agnes.Adaptor, c *gin.Context, info *relaycommon.RelayInfo, body []byte) []byte {
	t.Helper()
	result, err := a.DoRequest(c, info, bytes.NewReader(body))
	require.NoError(t, err)
	resp := result.(*http.Response)
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	require.NoError(t, err)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTTP %d: %.600s", resp.StatusCode, data)
	}
	return data
}

func liveVideo(t *testing.T, key, modelName string) {
	body := fmt.Sprintf(`{"model":%q,"prompt":"A blue ball rolling slowly on a plain white floor.","mode":"text","seconds":"4","size":"720P"}`, modelName)
	if modelName == agnes.ModelVideoV20 {
		body = fmt.Sprintf(`{"model":%q,"prompt":"A blue ball rolling slowly on a plain white floor.","num_frames":25,"frame_rate":24}`, modelName)
	}
	c := liveContext("/v1/videos", body)
	info := liveInfo(key, "/v1/videos", 0)
	info.OriginModelName = modelName
	info.UpstreamModelName = modelName
	info.TaskRelayInfo = &relaycommon.TaskRelayInfo{PublicTaskID: "task_live_probe"}
	a := &taskagnes.TaskAdaptor{}
	a.Init(info)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	require.Nil(t, a.ValidateMappedRequest(c, info))
	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	resp, err := a.DoRequest(c, info, reader)
	require.NoError(t, err)
	if resp.StatusCode != 200 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 600))
		t.Fatalf("submit HTTP %d: %s", resp.StatusCode, data)
	}
	id, _, taskErr := a.DoResponse(c, resp, info)
	require.Nil(t, taskErr)
	require.NotEmpty(t, info.UpstreamVideoID)
	t.Logf("created task %s; retrieval ID %s", id, info.UpstreamVideoID)
	deadline := time.Now().Add(8 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(15 * time.Second)
		resp, err := a.FetchTask(info.ChannelBaseUrl, key, map[string]any{"task_id": id, "video_id": info.UpstreamVideoID, "model_name": modelName}, "")
		if err != nil {
			t.Log("poll transport error; retrying")
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		require.NoError(t, err)
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			t.Logf("poll HTTP %d; retrying", resp.StatusCode)
			continue
		}
		require.Equal(t, 200, resp.StatusCode)
		result, err := a.ParseTaskResult(data)
		require.NoError(t, err)
		t.Logf("status=%s progress=%s", result.Status, result.Progress)
		if result.Status == "SUCCESS" {
			require.NotEmpty(t, result.Url)
			return
		}
		if result.Status == "FAILURE" {
			t.Fatalf("upstream task failed: %s", result.Reason)
		}
	}
	t.Fatal("poll deadline reached; task was not resubmitted")
}
