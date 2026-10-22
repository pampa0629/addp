package scanresource

import (
	"context"
	"errors"
	"testing"

	commonmodels "github.com/addp/common/models"
)

func TestCollectionCheckPrecedesCatalogOpen(t *testing.T) {
	denied := errors.New("qualification revoked")
	calls := 0
	ctx := WithCollectionReadCheck(context.Background(), func(context.Context, *commonmodels.Engine) error { calls++; return denied })
	// A nil Provider would panic if source capability inspection happened first.
	if _, err := RootBranchEntries(ctx, &commonmodels.Engine{}, nil); !errors.Is(err, denied) || calls != 1 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := CheckCollectionRead(cancelled, &commonmodels.Engine{}); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("cancelled read contacted the source", err)
	}
}
