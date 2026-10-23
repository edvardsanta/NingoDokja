package communications

import "testing"

func TestLinkPolicyAuthorizesOnlyConfiguredPlatformsAndKnownActions(t *testing.T) {
	policy := LinkPolicy{Available: map[string]bool{"whatsapp": true, "disabled": false}}
	for _, action := range []LinkAction{LinkStatus, LinkStart, LinkReset} {
		name, err := policy.Authorize(action, " whatsapp ")
		if err != nil || name != "whatsapp" {
			t.Fatalf("%s: got %q, %v", action, name, err)
		}
	}
	for _, tc := range []struct {
		action LinkAction
		name   string
	}{
		{LinkStart, "discord"},
		{LinkReset, "disabled"},
		{LinkStatus, ""},
		{LinkStart, "../whatsapp"},
		{"communications.link.delete", "whatsapp"},
	} {
		if _, err := policy.Authorize(tc.action, tc.name); err == nil {
			t.Fatalf("authorized %s on %q", tc.action, tc.name)
		}
	}
}
