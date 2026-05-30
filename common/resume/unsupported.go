package resume

import (
	"fmt"
	"strings"
)

// RejectUnsupported returns a consistent error when a provider receives a
// marker before it has implemented marker-based resume semantics.
func RejectUnsupported(marker *Marker, provider string) error {
	if marker == nil {
		return nil
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = "provider"
	}
	return fmt.Errorf("%s does not support resume marker", provider)
}
