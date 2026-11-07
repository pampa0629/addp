package plugin

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type publicationContractPlugin struct{ MockPlugin }

func (*publicationContractPlugin) PrepareNewFile(context.Context, ConnectionInfo, NewFilePublicationRequest, io.Reader) (PreparedNewFile, error) {
	return PreparedNewFile{}, nil
}
func (*publicationContractPlugin) PublishNewFile(context.Context, ConnectionInfo, PreparedNewFile, NewFilePublicationState) (NewFilePublicationOutcome, error) {
	return NewFilePublicationOutcome{}, nil
}
func (*publicationContractPlugin) ResolveNewFile(context.Context, ConnectionInfo, PreparedNewFile) (NewFilePublicationOutcome, error) {
	return NewFilePublicationOutcome{}, nil
}
func (*publicationContractPlugin) CleanupNewFile(context.Context, ConnectionInfo, PreparedNewFile, NewFilePublicationState) error {
	return nil
}

func TestNewFilePublicationCapabilityRequiresBothDeclarationAndProvider(t *testing.T) {
	store := &StoreCapability{NewFilePublication: true}
	p := &publicationContractPlugin{}
	if err := validateStoreCapabilities(&MockPlugin{}, store); err == nil {
		t.Fatal("declaration without provider admitted")
	}
	if err := validateStoreCapabilities(p, store); err != nil {
		t.Fatal(err)
	}
	if err := validateStoreProviderCapabilities(p, nil); err == nil {
		t.Fatal("undeclared provider admitted")
	}
	if err := validateStoreProviderCapabilities(p, &StorageCapabilities{Store: store}); err != nil {
		t.Fatal(err)
	}
}
func TestNewFilePublicationRequiresExactCanonicalTargetAndActualOutcome(t *testing.T) {
	valid := NewFilePublicationRequest{OperationID: uuid.NewString(), Target: FileItemPath(14, "outputs/difference.gpkg"), SizeBytes: 1, SHA256: strings.Repeat("a", 64)}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"actor-free-id", "zero-engine", "alias", "embedded-slash", "wrong-kind", "digest", "negative-size"} {
		t.Run(fault, func(t *testing.T) {
			r := valid
			r.Target.Segments = append([]EngineCatalogSegment{}, valid.Target.Segments...)
			switch fault {
			case "actor-free-id":
				r.OperationID = ""
			case "zero-engine":
				r.Target.EngineID = 0
			case "alias":
				r.Target.Segments[1].Name = ".."
			case "embedded-slash":
				r.Target.Segments[2].Name = "other/file"
			case "wrong-kind":
				r.Target.Segments[1].Kind = EngineCatalogKindFile
			case "digest":
				r.SHA256 = "bad"
			case "negative-size":
				r.SizeBytes = -1
			}
			if err := r.Validate(); err == nil {
				t.Fatal("invalid preparation admitted")
			}
		})
	}
	p := PreparedNewFile{NewFilePublicationRequest: valid}
	e := &ContentWriteEvidence{Target: valid.Target, Disposition: "created", SizeBytes: 1, Committed: true}
	if err := (NewFilePublicationOutcome{State: NewFilePublished, Evidence: e}).Validate(p); err != nil {
		t.Fatal(err)
	}
	if err := (NewFilePublicationOutcome{State: NewFileUnknown, Evidence: e}).Validate(p); err == nil {
		t.Fatal("unknown granted committed evidence")
	}
	e.SizeBytes++
	if err := (NewFilePublicationOutcome{State: NewFilePublished, Evidence: e}).Validate(p); err == nil {
		t.Fatal("wrong size evidence admitted")
	}
}
