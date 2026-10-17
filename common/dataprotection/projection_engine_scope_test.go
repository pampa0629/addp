package dataprotection

import (
	"github.com/addp/common/models"
	"testing"
)

func TestProtectionEngineOwnershipRequiresMatchingIdentityPreimage(t *testing.T) {
	target := ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: models.GenerateItemFingerprint(13, "neo4j.graph")}
	if err := VerifyTargetEngine(target, 13, "neo4j.graph"); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		id   uint
		name string
	}{{0, "neo4j.graph"}, {13, ""}, {11, "neo4j.graph"}, {13, "other.graph"}} {
		if err := VerifyTargetEngine(target, input.id, input.name); err == nil {
			t.Fatalf("invalid preimage accepted: %+v", input)
		}
	}
	target.ResourceType = "professional_resource"
	if err := VerifyTargetEngine(target, 0, ""); err != nil {
		t.Fatal(err)
	}
	if err := VerifyTargetEngine(target, 13, "neo4j.graph"); err == nil {
		t.Fatal("professional resource acquired engine ownership")
	}
}
