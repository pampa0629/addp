package oauth

import "github.com/ory/fosite"

type Client struct {
	ID            string
	SecretHash    []byte
	RedirectURIs  []string
	GrantTypes    []string
	ResponseTypes []string
	Scopes        []string
	Audiences     []string
	Public        bool
}

func (c *Client) GetID() string { return c.ID }

func (c *Client) GetHashedSecret() []byte { return append([]byte(nil), c.SecretHash...) }

func (c *Client) GetRedirectURIs() []string { return append([]string(nil), c.RedirectURIs...) }

func (c *Client) GetGrantTypes() fosite.Arguments {
	return fosite.Arguments(append([]string(nil), c.GrantTypes...))
}

func (c *Client) GetResponseTypes() fosite.Arguments {
	return fosite.Arguments(append([]string(nil), c.ResponseTypes...))
}

func (c *Client) GetScopes() fosite.Arguments {
	return fosite.Arguments(append([]string(nil), c.Scopes...))
}

func (c *Client) IsPublic() bool { return c.Public }

func (c *Client) GetAudience() fosite.Arguments {
	return fosite.Arguments(append([]string(nil), c.Audiences...))
}

var _ fosite.Client = (*Client)(nil)
