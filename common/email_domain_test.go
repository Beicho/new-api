package common

import "testing"

func TestDomainEmailRegistrationExemptions(t *testing.T) {
	originalInviteList := EmailDomainInviteCodeExemptionList
	originalRegistrationList := EmailDomainRegistrationCodeExemptionList
	originalBlacklist := append([]string(nil), EmailDomainBlacklist...)
	t.Cleanup(func() {
		EmailDomainInviteCodeExemptionList = originalInviteList
		EmailDomainRegistrationCodeExemptionList = originalRegistrationList
		EmailDomainBlacklist = originalBlacklist
	})

	EmailDomainInviteCodeExemptionList = []string{"invite.test", "*.trusted.test"}
	EmailDomainRegistrationCodeExemptionList = []string{"registration.test", "*.trusted.test"}
	EmailDomainBlacklist = []string{"*.blocked.trusted.test"}

	tests := []struct {
		email        string
		invite       bool
		registration bool
		blacklisted  bool
	}{
		{"user@invite.test", true, false, false},
		{"user@registration.test", false, true, false},
		{"user@mail.trusted.test", true, true, false},
		{"user@TRUSTED.TEST", true, true, false},
		{"user@mail.blocked.trusted.test", false, false, true},
		{"user@BLOCKED.TRUSTED.TEST", false, false, true},
		{"user@nottrusted.test", false, false, false},
		{"user@trusted.test.evil.test", false, false, false},
		{"user@mail.invite.test", false, false, false},
		{"user@example.com", false, false, false},
		{"invalid", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			if got := IsDomainEmailInviteCodeExempt(tt.email); got != tt.invite {
				t.Errorf("invite exemption = %v, want %v", got, tt.invite)
			}
			if got := IsDomainEmailRegistrationCodeExempt(tt.email); got != tt.registration {
				t.Errorf("registration exemption = %v, want %v", got, tt.registration)
			}
			if got := IsEmailDomainBlacklisted(tt.email); got != tt.blacklisted {
				t.Errorf("blacklisted = %v, want %v", got, tt.blacklisted)
			}
		})
	}

	EmailDomainInviteCodeExemptionList = []string{"", " "}
	EmailDomainRegistrationCodeExemptionList = nil
	if IsDomainEmailInviteCodeExempt("user@mail.trusted.test") || IsDomainEmailRegistrationCodeExempt("user@mail.trusted.test") {
		t.Fatal("empty lists must not exempt any domain")
	}
}

func TestHasEmailDomainRules(t *testing.T) {
	if HasEmailDomainRules(nil) || HasEmailDomainRules([]string{"", " "}) {
		t.Fatal("empty lists must not enable public exemption hints")
	}
	if !HasEmailDomainRules([]string{"", "example.com"}) {
		t.Fatal("nonempty lists must enable public exemption hints")
	}
}

func TestGetEmailDomain(t *testing.T) {
	tests := map[string]string{
		" Student@Mail.SWJTU.edu.cn ": "mail.swjtu.edu.cn",
		"invalid":                     "",
		"missing-domain@":             "",
	}
	for input, want := range tests {
		if got := GetEmailDomain(input); got != want {
			t.Fatalf("GetEmailDomain(%q) = %q, want %q", input, got, want)
		}
	}
}
