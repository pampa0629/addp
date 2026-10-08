package authorization

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"unicode/utf8"
)

const (
	// PlatformDefinitionPublishPermission is owned by Ontology's manifest.
	PlatformDefinitionPublishPermission       = "ontology.platform_definition.publish"
	PlatformDefinitionPublisherClient         = "addp-ontology"
	PlatformPublicationCheckByteLimit   int64 = 2048
)

var platformPublicationDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

// PlatformPublicationCheck binds a permission observation, not a publication.
// Revision is a canonical BIGINT string to preserve precision across clients.
type PlatformPublicationCheck struct {
	Capability string `json:"capability"`
	Revision   string `json:"revision"`
	Digest     string `json:"digest"`
}

func (b PlatformPublicationCheck) Validate() error {
	if len(b.Capability) > 128 || !authContextToolScopePattern.MatchString(b.Capability) || !positivePlatformPublicationID(b.Revision) || !platformPublicationDigest.MatchString(b.Digest) {
		return errors.New("invalid platform publication binding")
	}
	return nil
}

// PlatformPublicationObservation carries current identity facts only. It must
// never be cached or used as a bearer credential or activation receipt.
type PlatformPublicationObservation struct {
	PlatformPublicationCheck
	ContextType          string `json:"context_type"`
	ClientID             string `json:"client_id"`
	PrincipalID          string `json:"principal_id"`
	PrincipalType        string `json:"principal_type"`
	AuthorizationVersion string `json:"authorization_version"`
}

func (o PlatformPublicationObservation) Validate(binding PlatformPublicationCheck) error {
	if o.PlatformPublicationCheck != binding || binding.Validate() != nil || o.ContextType != "platform" || o.ClientID != PlatformDefinitionPublisherClient || o.PrincipalType != "service_principal" || !positivePlatformPublicationID(o.PrincipalID) || !positivePlatformPublicationID(o.AuthorizationVersion) {
		return errors.New("invalid platform publication observation")
	}
	return nil
}

func positivePlatformPublicationID(value string) bool {
	n, err := strconv.ParseInt(value, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == value
}

func (b *PlatformPublicationCheck) UnmarshalJSON(data []byte) error {
	v, err := decodePlatformPublicationFields(data, "capability", "revision", "digest")
	if err != nil {
		return err
	}
	*b = PlatformPublicationCheck{Capability: v["capability"], Revision: v["revision"], Digest: v["digest"]}
	return b.Validate()
}

func (o *PlatformPublicationObservation) UnmarshalJSON(data []byte) error {
	v, err := decodePlatformPublicationFields(data, "capability", "revision", "digest", "context_type", "client_id", "principal_id", "principal_type", "authorization_version")
	if err != nil {
		return err
	}
	*o = PlatformPublicationObservation{PlatformPublicationCheck: PlatformPublicationCheck{Capability: v["capability"], Revision: v["revision"], Digest: v["digest"]}, ContextType: v["context_type"], ClientID: v["client_id"], PrincipalID: v["principal_id"], PrincipalType: v["principal_type"], AuthorizationVersion: v["authorization_version"]}
	return o.Validate(o.PlatformPublicationCheck)
}

// This bounded flat contract rejects aliases, duplicate keys, nulls and extra
// identity/payload fields on both sides of the owner boundary.
func decodePlatformPublicationFields(data []byte, fields ...string) (map[string]string, error) {
	invalid := errors.New("invalid platform publication JSON")
	if int64(len(data)) > PlatformPublicationCheckByteLimit || !utf8.Valid(data) {
		return nil, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, invalid
	}
	allowed := make(map[string]bool, len(fields))
	for _, field := range fields {
		allowed[field] = true
	}
	values := make(map[string]string, len(fields))
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || !allowed[name] {
			return nil, invalid
		}
		if _, duplicate := values[name]; duplicate {
			return nil, invalid
		}
		value, err := decoder.Token()
		text, ok := value.(string)
		if err != nil || !ok {
			return nil, invalid
		}
		values[name] = text
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || len(values) != len(fields) {
		return nil, invalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, invalid
	}
	return values, nil
}
