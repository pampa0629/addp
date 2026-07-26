package oauth

import "testing"

func TestClientGettersDoNotExposeMutableStorage(t *testing.T) {
	client := &Client{
		ID:            "addp-cli",
		SecretHash:    []byte("hash"),
		RedirectURIs:  []string{"http://127.0.0.1/callback"},
		GrantTypes:    []string{"authorization_code"},
		ResponseTypes: []string{"code"},
		Scopes:        []string{"openid"},
		Audiences:     []string{"addp.api"},
		Public:        true,
	}

	client.GetHashedSecret()[0] = 'x'
	client.GetRedirectURIs()[0] = "changed"
	client.GetGrantTypes()[0] = "changed"
	client.GetResponseTypes()[0] = "changed"
	client.GetScopes()[0] = "changed"
	client.GetAudience()[0] = "changed"

	if string(client.SecretHash) != "hash" || client.RedirectURIs[0] != "http://127.0.0.1/callback" ||
		client.GrantTypes[0] != "authorization_code" || client.ResponseTypes[0] != "code" ||
		client.Scopes[0] != "openid" || client.Audiences[0] != "addp.api" {
		t.Fatal("client getter exposed mutable storage")
	}
}
