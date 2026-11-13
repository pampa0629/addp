package iam

import (
	"context"
	"crypto/sha256"

	"github.com/addp/common/authorization"
)

type authenticatedRequestIdentityKey struct{}

type authenticatedRequestIdentity struct {
	credential [sha256.Size]byte
	identity   authorization.AuthContext
}

// WithAuthenticatedRequestIdentity is called only after System's HTTP
// authentication has resolved and validated the credential. It does not resolve
// credentials, create an Allow, or change the behavior of any IAM resolver.
func WithAuthenticatedRequestIdentity(ctx context.Context, credential string, identity authorization.AuthContext) context.Context {
	return context.WithValue(ctx, authenticatedRequestIdentityKey{}, authenticatedRequestIdentity{
		credential: sha256.Sum256([]byte(credential)),
		identity:   authorization.CloneAuthContext(identity),
	})
}

// AuthenticatedRequestIdentity supplies only the current request's preparation
// facts. A final rule snapshot must independently resolve the original token.
func AuthenticatedRequestIdentity(ctx context.Context, credential string) (authorization.AuthContext, bool) {
	facts, ok := ctx.Value(authenticatedRequestIdentityKey{}).(authenticatedRequestIdentity)
	if !ok || credential == "" || facts.credential != sha256.Sum256([]byte(credential)) {
		return authorization.AuthContext{}, false
	}
	return authorization.CloneAuthContext(facts.identity), true
}
