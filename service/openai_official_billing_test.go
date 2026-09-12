package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOfficialCacheWriteBilling(t *testing.T) {
	usage := &dto.Usage{PromptTokens: 1000, CompletionTokens: 20, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 100, CacheWriteTokens: common.GetPointer(200), CachedCreationTokens: 999}}
	params := BuildTieredTokenParams(usage, false, map[string]bool{"cr": true, "cc": true})
	require.Equal(t, float64(700), params.P)
	require.Equal(t, float64(1000), params.Len)
	require.Equal(t, float64(200), params.CC)
	params = BuildTieredTokenParams(usage, false, map[string]bool{})
	require.Equal(t, float64(1000), params.P, "unpriced cache subcategories remain in p")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{OriginModelName: "test", StartTime: time.Now(), PriceData: types.PriceData{ModelRatio: 1, CompletionRatio: 2, CacheRatio: 0.1, CacheCreationRatio: 1.25, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}}
	summary := calculateTextQuotaSummary(c, info, usage)
	require.Equal(t, 200, summary.CacheCreationTokens)
	require.Equal(t, 1000, summary.Quota)
	claudeUsage := buildClaudeUsageFromOpenAIUsage(usage)
	require.Equal(t, 200, claudeUsage.CacheCreationInputTokens)
	require.Equal(t, 200, claudeUsage.CacheCreation.Ephemeral5mInputTokens)
}
