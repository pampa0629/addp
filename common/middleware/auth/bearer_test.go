package auth

import "testing"

func TestCanonicalBearerTokenSyntax(t *testing.T) {
	for header, want := range map[string]string{
		"Bearer addp_at_user":     "addp_at_user",
		"bearer\taddp_at_user":    "addp_at_user",
		"  BEARER addp_at_user  ": "addp_at_user",
		"":                        "", "Bearer": "", "Basic addp_at_user": "", "Bearer addp_at_user extra": "",
	} {
		if got := CanonicalBearerToken(header); got != want {
			t.Errorf("header %q: got %q, want %q", header, got, want)
		}
	}
}
