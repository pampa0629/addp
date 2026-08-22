package service

import "context"

// staticServiceTokenSource is shared by service tests that exercise internal
// clients without contacting the real OAuth control plane.
type staticServiceTokenSource string

func (source staticServiceTokenSource) Token(context.Context, uint) (string, error) {
	return string(source), nil
}

func (source staticServiceTokenSource) PlatformToken(context.Context) (string, error) {
	return string(source), nil
}
