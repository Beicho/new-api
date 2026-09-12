package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetStatusIncludesInviteCodeRequirement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := setting.GetEnhancementSetting()
	original := cfg.InviteCodeRequired
	cfg.InviteCodeRequired = true
	t.Cleanup(func() {
		cfg.InviteCodeRequired = original
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/api/status", nil)
	GetStatus(ctx)

	var response struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Equal(t, true, response.Data["invite_code_required"])
}

func TestGetStatusIncludesIndependentDomainEmailExemptionHints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalInviteList := common.EmailDomainInviteCodeExemptionList
	originalRegistrationList := common.EmailDomainRegistrationCodeExemptionList
	t.Cleanup(func() {
		common.EmailDomainInviteCodeExemptionList = originalInviteList
		common.EmailDomainRegistrationCodeExemptionList = originalRegistrationList
	})

	for _, tt := range []struct {
		name             string
		inviteList       []string
		registrationList []string
		inviteHint       bool
		registrationHint bool
	}{
		{"empty", nil, []string{"", " "}, false, false},
		{"invite only", []string{"invite.test"}, nil, true, false},
		{"registration only", nil, []string{"registration.test"}, false, true},
		{"both", []string{"invite.test"}, []string{"registration.test"}, true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			common.EmailDomainInviteCodeExemptionList = tt.inviteList
			common.EmailDomainRegistrationCodeExemptionList = tt.registrationList
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest("GET", "/api/status", nil)
			GetStatus(ctx)
			var response struct {
				Success bool                   `json:"success"`
				Data    map[string]interface{} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.True(t, response.Success)
			require.Equal(t, tt.inviteHint, response.Data["domain_email_no_invite_code"])
			require.Equal(t, tt.registrationHint, response.Data["domain_email_no_registration_code"])
			require.NotContains(t, response.Data, "domain_email_registration_enabled")
			require.NotContains(t, recorder.Body.String(), "invite.test")
			require.NotContains(t, recorder.Body.String(), "registration.test")
		})
	}
}

func TestSendEmailVerificationRejectsBlacklistedDomain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := append([]string(nil), common.EmailDomainBlacklist...)
	common.EmailDomainBlacklist = []string{"*.hdu.edu.cn"}
	t.Cleanup(func() {
		common.EmailDomainBlacklist = original
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/api/verification?email=user@mail.hdu.edu.cn", nil)
	SendEmailVerification(ctx)

	var response registrationAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.Equal(t, i18n.MsgUserEmailDomainBlacklisted, response.Message)
}
