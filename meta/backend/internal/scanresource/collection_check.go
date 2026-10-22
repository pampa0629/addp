package scanresource

import (
	"context"

	commonmodels "github.com/addp/common/models"
)

type collectionReadCheckKey struct{}
type CollectionReadCheck func(context.Context, *commonmodels.Engine) error

// The task execution owner installs this callback only after proving the
// frozen governance plan. It is never constructed from HTTP request fields.
func WithCollectionReadCheck(ctx context.Context, check CollectionReadCheck) context.Context {
	return context.WithValue(ctx, collectionReadCheckKey{}, check)
}

func CheckCollectionRead(ctx context.Context, resource *commonmodels.Engine) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if check, ok := ctx.Value(collectionReadCheckKey{}).(CollectionReadCheck); ok {
		return check(ctx, resource)
	}
	return nil
}
