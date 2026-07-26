package oauth

import (
	"time"

	"github.com/ory/fosite"
	"github.com/ory/fosite/token/jwt"
)

// IAMSession is the in-memory projection of stable IAM facts used by Fosite.
// It is reconstructed from explicit database columns and is never serialized as a blob.
type IAMSession struct {
	PrincipalID                int64
	ContextType                string
	TenantMembershipID         *int64
	IssuedAuthorizationVersion int64
	Subject                    string
	AuthenticationMethods      []string
	AssuranceLevel             string
	AuthenticatedAt            time.Time
	RequestedAt                time.Time
	OIDCNonce                  string
	OIDCExtraClaims            map[string]interface{}

	expiresAt map[fosite.TokenType]time.Time
	claims    *jwt.IDTokenClaims
	headers   *jwt.Headers
}

func NewIAMSession() *IAMSession {
	return &IAMSession{
		expiresAt: make(map[fosite.TokenType]time.Time),
		claims:    &jwt.IDTokenClaims{},
		headers:   jwt.NewHeaders(),
	}
}

func (s *IAMSession) SetExpiresAt(key fosite.TokenType, expiresAt time.Time) {
	if s.expiresAt == nil {
		s.expiresAt = make(map[fosite.TokenType]time.Time)
	}
	s.expiresAt[key] = expiresAt.UTC()
}

func (s *IAMSession) GetExpiresAt(key fosite.TokenType) time.Time {
	if s == nil || s.expiresAt == nil {
		return time.Time{}
	}
	return s.expiresAt[key]
}

func (*IAMSession) GetUsername() string { return "" }

func (s *IAMSession) GetSubject() string {
	if s == nil {
		return ""
	}
	return s.Subject
}

func (s *IAMSession) Clone() fosite.Session {
	if s == nil {
		return nil
	}

	clone := *s
	clone.TenantMembershipID = cloneInt64Pointer(s.TenantMembershipID)
	clone.AuthenticationMethods = append([]string(nil), s.AuthenticationMethods...)
	clone.OIDCExtraClaims = cloneJSONMap(s.OIDCExtraClaims)
	clone.expiresAt = make(map[fosite.TokenType]time.Time, len(s.expiresAt))
	for tokenType, expiresAt := range s.expiresAt {
		clone.expiresAt[tokenType] = expiresAt
	}
	clone.claims = cloneIDTokenClaims(s.IDTokenClaims())
	clone.headers = &jwt.Headers{Extra: cloneJSONMap(s.IDTokenHeaders().Extra)}
	return &clone
}

func (s *IAMSession) IDTokenClaims() *jwt.IDTokenClaims {
	if s.claims == nil {
		s.claims = &jwt.IDTokenClaims{}
	}
	s.claims.Subject = s.Subject
	s.claims.Nonce = s.OIDCNonce
	s.claims.RequestedAt = s.RequestedAt
	s.claims.AuthTime = s.AuthenticatedAt
	s.claims.AuthenticationContextClassReference = s.AssuranceLevel
	s.claims.AuthenticationMethodsReferences = append([]string(nil), s.AuthenticationMethods...)
	s.claims.Extra = cloneJSONMap(s.OIDCExtraClaims)
	return s.claims
}

func (s *IAMSession) IDTokenHeaders() *jwt.Headers {
	if s.headers == nil {
		s.headers = jwt.NewHeaders()
	}
	return s.headers
}

func cloneIDTokenClaims(source *jwt.IDTokenClaims) *jwt.IDTokenClaims {
	if source == nil {
		return &jwt.IDTokenClaims{}
	}
	clone := *source
	clone.Audience = append([]string(nil), source.Audience...)
	clone.AuthenticationMethodsReferences = append([]string(nil), source.AuthenticationMethodsReferences...)
	clone.Extra = cloneJSONMap(source.Extra)
	return &clone
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneJSONMap(source map[string]interface{}) map[string]interface{} {
	if source == nil {
		return nil
	}
	clone := make(map[string]interface{}, len(source))
	for key, value := range source {
		clone[key] = cloneJSONValue(value)
	}
	return clone
}

func cloneJSONValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return cloneJSONMap(typed)
	case []interface{}:
		clone := make([]interface{}, len(typed))
		for index := range typed {
			clone[index] = cloneJSONValue(typed[index])
		}
		return clone
	case []string:
		return append([]string(nil), typed...)
	default:
		return typed
	}
}

var _ fosite.Session = (*IAMSession)(nil)
