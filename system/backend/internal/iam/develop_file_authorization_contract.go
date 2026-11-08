package iam

import (
	"context"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"gorm.io/gorm"
)

// DevelopFileReadPreparer reads Meta facts outside IAM locks. Its verifier
// consumes only that exact immutable boundary in the caller's IAM transaction.
// Neither preparation nor its scope is an Allow or an issued source Grant.
type DevelopFileReadPreparer func(context.Context, *ExecutionAuthorization, execution.DevelopFileIntent) (*DevelopFileReadPreparation, error)

type DevelopFileReadPreparation struct {
	Scope  execution.DevelopBoundFileScope
	Verify func(context.Context, *gorm.DB, *ExecutionAuthorization, *plugin.EngineCatalogPath) (*DevelopFileReadObservation, error)
}

type DevelopFileReadObservation struct {
	ObservedAt time.Time
	ExpiresAt  *time.Time
	Engine     *models.Engine
}
