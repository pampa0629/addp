package service

import (
	"context"
	"github.com/addp/common/dataitem/authorizationfacts"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/engineaccess"
)

type fileCompositionFactsReader interface {
	ReadAuthorizationFacts(context.Context, uint, []string) (*authorizationfacts.AuthorizationFactsBatch, error)
}

// TIFF stream reads and COG generation consume the same formal Meta boundary.
func readFileCompositionFromMeta(ctx context.Context, reader fileCompositionFactsReader, tenant uint, fingerprint string, item *commonModels.MetaItem, format string) (*authorizationfacts.FileCompositionBoundary, error) {
	batch, err := reader.ReadAuthorizationFacts(ctx, tenant, []string{fingerprint})
	if err != nil {
		return nil, engineaccess.ErrUnavailable
	}
	// The shared client validates the exact batch envelope at its HTTP boundary.
	facts := batch.Data[0].Facts
	if facts == nil || item == nil || facts.EngineID != item.EngineID || facts.FullName != item.FullName ||
		facts.ItemType != item.ItemType || facts.BasisState != authorizationfacts.AuthorizationBasisComplete ||
		facts.Boundary == nil || facts.Boundary.FileComposition == nil || facts.Boundary.FileComposition.Format != format {
		return nil, ErrFileSourceAuthorizationRequired
	}
	return facts.Boundary.FileComposition.Clone(), nil
}
