package contentadapter

import (
	"context"
	"errors"
	"io"
	"testing"

	p "github.com/addp/common/engine/plugin"
	"github.com/addp/common/infra/objectstore"
)

type failingPutArea struct {
	objectstore.ContentArea
	failure error
}

func (s failingPutArea) Put(_ context.Context, _ string, body io.Reader, _ int64, _ string) error {
	_, err := io.Copy(io.Discard, body)
	if err != nil {
		return err
	}
	return s.failure
}
func TestContentWriterCloseReportsCommittedUploadFailure(t *testing.T) {
	failure := errors.New("object upload rejected")
	provider := &Provider{Name: objectstore.Manager, Store: failingPutArea{failure: failure}}
	writer, err := provider.CreateContent(t.Context(), nil, p.ObjectItemPath(0, objectstore.Manager, "tenant_7/file.csv"), p.WriteOptions{Overwrite: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(writer, "surface\n42.375\n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); !errors.Is(err, failure) {
		t.Fatalf("lost upload error: %v", err)
	}
}
