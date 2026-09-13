package helper

import (
	"bytes"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"mime/multipart"
	"net/http/httptest"
	"testing"
)

func TestOfficialResponsesValidation(t *testing.T) {
	for _, body := range []string{`{"model":"test"}`, `{"model":"test","prompt":{"id":"p"},"background":false}`, `{"model":"test","previous_response_id":"r"}`} {
		c := requestContext(body, constant.ChannelTypeOpenAI)
		_, err := GetAndValidateResponsesRequest(c)
		require.NoError(t, err)
		common.CleanupBodyStorage(c)
	}
	for _, body := range []string{`{"input":"hi"}`, `{"model":"test","background":true}`} {
		c := requestContext(body, constant.ChannelTypeOpenAI)
		_, err := GetAndValidateResponsesRequest(c)
		require.Error(t, err)
		common.CleanupBodyStorage(c)
	}
}

func TestOfficialMultipartMediaScalars(t *testing.T) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for key, value := range map[string]string{"model": "test", "stream": "true", "n": "0", "partial_images": "0", "output_compression": "0"} {
		require.NoError(t, w.WriteField(key, value))
	}
	require.NoError(t, w.Close())
	for _, audio := range []bool{false, true} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/images/edits", bytes.NewReader(body.Bytes()))
		c.Request.Header.Set("Content-Type", w.FormDataContentType())
		common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
		if audio {
			req, err := GetAndValidAudioRequest(c, relayconstant.RelayModeAudioTranscription)
			require.NoError(t, err)
			require.True(t, req.IsStream(c))
		} else {
			req, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
			require.NoError(t, err)
			require.True(t, req.IsStream(c))
			require.NotNil(t, req.N)
			require.Zero(t, *req.N)
		}
		common.CleanupBodyStorage(c)
	}
	for _, body := range []string{`{"model":"test","prompt":"image"}`, `{"model":"test","prompt":"image","n":0}`} {
		c := requestContext(body, constant.ChannelTypeOpenAI)
		req, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesGenerations)
		require.NoError(t, err)
		out, err := common.Marshal(req)
		require.NoError(t, err)
		require.JSONEq(t, body, string(out))
		common.CleanupBodyStorage(c)
	}
}
