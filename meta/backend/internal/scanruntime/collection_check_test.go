package scanruntime

import (
	"context"
	"errors"
	"testing"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/scanresource"
)

func TestCollectionCheckPrecedesNamespaceEnumeration(t *testing.T) {
	denied := errors.New("qualification expired")
	ctx := scanresource.WithCollectionReadCheck(context.Background(), func(context.Context, *commonmodels.Engine) error { return denied })
	// No catalog Provider is installed: rejection must precede any invocation.
	if _, err := (&DatabaseRuntime{}).listTables(ctx, &commonmodels.Engine{ID: 9}, databaseScanCatalog{}, "outdoor"); !errors.Is(err, denied) {
		t.Fatal("namespace enumeration bypassed qualification", err)
	}
}
