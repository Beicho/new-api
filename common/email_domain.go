package common

import "strings"

// GetEmailDomain returns the normalized domain part of an email address.
func GetEmailDomain(email string) string {
	email = strings.TrimSpace(email)
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(email[at+1:]))
}

func IsEmailDomainBlacklisted(email string) bool {
	return IsDomainListed(GetEmailDomain(email), EmailDomainBlacklist)
}

// Callers must verify email ownership before applying either exemption.
func IsDomainEmailInviteCodeExempt(email string) bool {
	return !IsEmailDomainBlacklisted(email) && IsDomainListed(GetEmailDomain(email), EmailDomainInviteCodeExemptionList)
}

func IsDomainEmailRegistrationCodeExempt(email string) bool {
	return !IsEmailDomainBlacklisted(email) && IsDomainListed(GetEmailDomain(email), EmailDomainRegistrationCodeExemptionList)
}

// HasEmailDomainRules ignores empty entries in the comma-separated options.
func HasEmailDomainRules(domains []string) bool {
	for _, domain := range domains {
		if strings.TrimSpace(domain) != "" {
			return true
		}
	}
	return false
}
