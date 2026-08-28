package iam

import "testing"

func TestValidateManagedOAuthClientDefinition(t *testing.T) {
	tests := []struct {
		name     string
		redirect []string
		wantErr  bool
	}{
		{name: "https callback", redirect: []string{"https://bi.example.com/oauth/callback"}},
		{name: "IPv4 loopback callback", redirect: []string{"http://127.0.0.1:49152/callback"}},
		{name: "IPv6 loopback callback", redirect: []string{"http://[::1]:49152/callback"}},
		{name: "remote HTTP", redirect: []string{"http://bi.example.com/callback"}, wantErr: true},
		{name: "localhost alias", redirect: []string{"http://localhost/callback"}, wantErr: true},
		{name: "HTTPS localhost alias", redirect: []string{"https://localhost/callback"}, wantErr: true},
		{name: "fragment", redirect: []string{"https://bi.example.com/callback#token"}, wantErr: true},
		{name: "wildcard", redirect: []string{"https://*.example.com/callback"}, wantErr: true},
		{name: "userinfo", redirect: []string{"https://user:password@bi.example.com/callback"}, wantErr: true},
		{name: "duplicate", redirect: []string{"https://bi.example.com/callback", "https://bi.example.com/callback"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := validateManagedOAuthClientDefinition("Research BI", test.redirect)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateManagedOAuthClientDefinition() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestGenerateManagedOAuthClientID(t *testing.T) {
	first, err := generateManagedOAuthClientID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := generateManagedOAuthClientID()
	if err != nil {
		t.Fatal(err)
	}
	if !validManagedOAuthClientID(first) || !validManagedOAuthClientID(second) || first == second {
		t.Fatalf("generated client IDs = %q, %q", first, second)
	}
}

func TestValidManagedOAuthClientID(t *testing.T) {
	for _, clientID := range []string{
		"addp_ext_short",
		"addp_ext_invalid.client.id",
		"platform-client",
	} {
		if validManagedOAuthClientID(clientID) {
			t.Fatalf("validManagedOAuthClientID(%q) = true", clientID)
		}
	}
}
