package service

import "context"

// registrationServiceTokens is shared by service tests that need a fixed
// tenant and platform service token without contacting System OAuth.
type registrationServiceTokens string

func (token registrationServiceTokens) Token(context.Context, uint) (string, error) {
	return string(token), nil
}

func (token registrationServiceTokens) PlatformToken(context.Context) (string, error) {
	return string(token), nil
}
