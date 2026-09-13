package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestEmailDomainExemptionOptionCompatibility(t *testing.T) {
	for _, tt := range []struct {
		name         string
		options      []*Option
		invite       string
		registration string
	}{
		{name: "new installation"},
		{name: "legacy enabled", options: []*Option{
			{Key: "DomainEmailRegistrationEnabled", Value: "true"},
			{Key: "DomainEmailRegistrationWhitelist", Value: "*.trusted.test"},
		}, invite: "*.trusted.test", registration: "*.trusted.test"},
		{name: "legacy disabled", options: []*Option{
			{Key: "DomainEmailRegistrationEnabled", Value: "false"},
			{Key: "DomainEmailRegistrationWhitelist", Value: "*.trusted.test"},
		}},
		{name: "legacy switch absent", options: []*Option{
			{Key: "DomainEmailRegistrationWhitelist", Value: "*.trusted.test"},
		}},
		{name: "independent new lists override legacy", options: []*Option{
			{Key: "DomainEmailRegistrationWhitelist", Value: "*.trusted.test"},
			{Key: "EmailDomainInviteCodeExemptionList", Value: "invite.test"},
			{Key: "EmailDomainRegistrationCodeExemptionList", Value: "registration.test"},
			{Key: "DomainEmailRegistrationEnabled", Value: "true"},
		}, invite: "invite.test", registration: "registration.test"},
		{name: "explicitly empty invite list wins", options: []*Option{
			{Key: "DomainEmailRegistrationWhitelist", Value: "*.trusted.test"},
			{Key: "EmailDomainInviteCodeExemptionList", Value: ""},
			{Key: "DomainEmailRegistrationEnabled", Value: "true"},
		}, registration: "*.trusted.test"},
		{name: "explicitly empty registration list wins", options: []*Option{
			{Key: "EmailDomainRegistrationCodeExemptionList", Value: ""},
			{Key: "DomainEmailRegistrationEnabled", Value: "true"},
			{Key: "DomainEmailRegistrationWhitelist", Value: "*.trusted.test"},
		}, invite: "*.trusted.test"},
		{name: "new lists need no switch", options: []*Option{
			{Key: "EmailDomainInviteCodeExemptionList", Value: "invite.test"},
			{Key: "EmailDomainRegistrationCodeExemptionList", Value: "registration.test"},
		}, invite: "invite.test", registration: "registration.test"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			options := withEmailDomainExemptionOptions(tt.options)
			values := make(map[string]string)
			for _, option := range options {
				require.NotContains(t, values, option.Key)
				values[option.Key] = option.Value
			}
			require.Contains(t, values, "EmailDomainInviteCodeExemptionList")
			require.Contains(t, values, "EmailDomainRegistrationCodeExemptionList")
			require.Equal(t, tt.invite, values["EmailDomainInviteCodeExemptionList"])
			require.Equal(t, tt.registration, values["EmailDomainRegistrationCodeExemptionList"])
			require.Equal(t, options, withEmailDomainExemptionOptions(options))
		})
	}
}

func TestEmailDomainExemptionOptionsPersistAndClear(t *testing.T) {
	db := setupRegistrationCodeTestDB(t)
	require.NoError(t, db.AutoMigrate(&Option{}))
	originalMap := common.OptionMap
	originalInvite := common.EmailDomainInviteCodeExemptionList
	originalRegistration := common.EmailDomainRegistrationCodeExemptionList
	common.OptionMap = make(map[string]string)
	t.Cleanup(func() {
		common.OptionMap = originalMap
		common.EmailDomainInviteCodeExemptionList = originalInvite
		common.EmailDomainRegistrationCodeExemptionList = originalRegistration
	})
	require.NoError(t, db.Create(&[]Option{
		{Key: "DomainEmailRegistrationEnabled", Value: "true"},
		{Key: "DomainEmailRegistrationWhitelist", Value: "*.trusted.test"},
	}).Error)
	loadOptionsFromDatabase()
	require.Equal(t, []string{"*.trusted.test"}, common.EmailDomainInviteCodeExemptionList)
	require.Equal(t, []string{"*.trusted.test"}, common.EmailDomainRegistrationCodeExemptionList)

	require.NoError(t, UpdateOption("EmailDomainInviteCodeExemptionList", "invite.test"))
	require.NoError(t, UpdateOption("EmailDomainRegistrationCodeExemptionList", "registration.test"))
	loadOptionsFromDatabase()
	require.Equal(t, []string{"invite.test"}, common.EmailDomainInviteCodeExemptionList)
	require.Equal(t, []string{"registration.test"}, common.EmailDomainRegistrationCodeExemptionList)

	require.NoError(t, UpdateOption("EmailDomainInviteCodeExemptionList", ""))
	loadOptionsFromDatabase()
	require.False(t, common.HasEmailDomainRules(common.EmailDomainInviteCodeExemptionList))
	require.Equal(t, []string{"registration.test"}, common.EmailDomainRegistrationCodeExemptionList)
	require.NoError(t, UpdateOption("EmailDomainRegistrationCodeExemptionList", ""))
	loadOptionsFromDatabase()
	require.False(t, common.HasEmailDomainRules(common.EmailDomainInviteCodeExemptionList))
	require.False(t, common.HasEmailDomainRules(common.EmailDomainRegistrationCodeExemptionList))
	require.Empty(t, common.OptionMap["EmailDomainInviteCodeExemptionList"])
	require.Empty(t, common.OptionMap["EmailDomainRegistrationCodeExemptionList"])

	// A failed database refresh must not turn a read failure into empty defaults.
	common.EmailDomainInviteCodeExemptionList = []string{"keep-invite.test"}
	common.EmailDomainRegistrationCodeExemptionList = []string{"keep-registration.test"}
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	loadOptionsFromDatabase()
	require.Equal(t, []string{"keep-invite.test"}, common.EmailDomainInviteCodeExemptionList)
	require.Equal(t, []string{"keep-registration.test"}, common.EmailDomainRegistrationCodeExemptionList)
}
