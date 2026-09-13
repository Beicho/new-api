package model

// Apply legacy defaults before loading options, independent of database row order.
// An explicitly saved new list (including an empty one) always takes precedence.
// Old disabled configurations must not become active when the switches disappear.
func withEmailDomainExemptionOptions(options []*Option) []*Option {
	values := make(map[string]string, len(options))
	for _, option := range options {
		values[option.Key] = option.Value
	}
	legacyList := ""
	if values["DomainEmailRegistrationEnabled"] == "true" {
		legacyList = values["DomainEmailRegistrationWhitelist"]
	}
	for _, key := range []string{"EmailDomainInviteCodeExemptionList", "EmailDomainRegistrationCodeExemptionList"} {
		if _, exists := values[key]; !exists {
			options = append(options, &Option{Key: key, Value: legacyList})
		}
	}
	return options
}
