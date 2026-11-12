package production

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func TestReceiptRegistryRequiresExactResultAndOwnsSnapshot(t *testing.T) {
	r := committedReceipt(plugin.TabularItemPath(2, "schema", "public", "result"))
	ref := r.Reference
	calls := 0
	registry, err := NewRegistry(Owner{Module: "develop", ClientID: "addp-develop", ReceiptPermission: "develop.production_receipt.read", ExecutionPermission: "develop.task.execute", ReadReceipt: func(_ context.Context, tenant uint, got Reference) (*Receipt, error) {
		calls++
		if tenant != 7 || got != ref {
			t.Fatal("reader escaped original identity")
		}
		return &r, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := registry.Read(t.Context(), 7, ref)
	if err != nil {
		t.Fatal(err)
	}
	r.Primary.Segments[2].Name = "later"
	r.Origin.PrincipalID++
	if snapshot.Primary.Segments[2].Name != "result" || snapshot.Origin.PrincipalID != 31 {
		t.Fatal("producer mutated retained receipt")
	}
	r.Reference.ResultID = uuid.NewString()
	if _, err = registry.Read(t.Context(), 7, ref); err == nil {
		t.Fatal("another result's receipt accepted")
	}
	if calls != 2 {
		t.Fatal("unexpected network calls", calls)
	}
	ref.Owner = "unregistered"
	if _, err = registry.Read(t.Context(), 7, ref); err == nil || calls != 2 {
		t.Fatal("unregistered owner invoked reader")
	}
}

func TestSourceKindsRequireTheirOwnProof(t *testing.T) {
	proof := &PrivateUploadProof{UploadID: uuid.NewString(), UploaderID: 31, TaskID: 7, ManifestSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if (SourceEvidence{Kind: SourcesPrivateInput, PrivateUpload: proof}).Validate() != nil {
		t.Fatal("valid private upload rejected")
	}
	for _, r := range []SourceEvidence{{Kind: SourcesPrivateInput}, {Kind: SourcesUnknown, PrivateUpload: proof}, {Kind: SourcesNone, PrivateUpload: proof}} {
		if r.Validate() == nil {
			t.Fatal("inconsistent source kind accepted", r)
		}
	}
}
