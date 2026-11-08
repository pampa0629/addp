package authorization

import (
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

func TestSearchObservationAllowsEmptyBasisWithoutRelaxingMetadataChecks(t *testing.T) {
	request := SourceSearchCheckRequest{QueryNonce: "0123456789abcdef0123456789abcdef"}
	if !request.Valid() || (SourceMetadataCheckRequest{}).Valid() {
		t.Fatal("empty search observation changed another operation")
	}
	response := &SourceSearchCheck{ObservedAt: time.Now(), ObservationTag: strings.Repeat("A", 43)}
	if !response.Matches(request) {
		t.Fatal("valid empty observation rejected")
	}
	bad := strings.Repeat("B", 43)
	request.ExpectedObservationTag = &bad
	if response.Matches(request) {
		t.Fatal("accepted another basis")
	}
	request.ExpectedObservationTag = nil
	target := plugin.TabularItemPath(12, "schema", "public", "table")
	request.Targets = []plugin.EngineCatalogPath{target}
	response.Targets = []SourceSearchTargetCheck{{SourceMetadataTargetCheck: SourceMetadataTargetCheck{Target: target, Visible: true}, ItemFingerprint: strings.Repeat("a", 64)}}
	if !response.Matches(request) {
		t.Fatal("valid bound identity rejected")
	}
	response.Targets[0].Visible = false
	if response.Matches(request) {
		t.Fatal("hidden identity escaped observation")
	}
	response.Targets[0].ItemFingerprint = ""
	response.Targets[0].ContentReadable = true
	if response.Matches(request) {
		t.Fatal("content was readable without visibility")
	}
}
