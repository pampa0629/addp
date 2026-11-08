package service

import (
	"context"
	"errors"

	"github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resourcetree"
)

var ErrSearchVisibilityUnavailable = errors.New("search visibility unavailable")

type SearchVisibilityChecker interface {
	CheckManagerSearchRead(context.Context, string, authorization.SourceSearchCheckRequest) (*authorization.SourceSearchCheck, error)
}

type indexedSearchIdentity struct {
	ID         string `json:"document_id"`
	Locator    string `json:"locator"`
	EngineType string `json:"engine_type"`
	TenantID   uint   `json:"tenant_id"`
	EngineID   uint   `json:"engine_id"`
}

func (doc indexedSearchIdentity) catalogPath() (plugin.EngineCatalogPath, bool) {
	loc, err := resourcetree.ParseURI(doc.Locator)
	if err != nil || loc.ItemID == nil || *loc.ItemID == 0 || loc.EngineID != doc.EngineID {
		return plugin.EngineCatalogPath{}, false
	}
	model, err := dbbridge.EngineCatalogModel(doc.EngineType)
	if err != nil {
		return plugin.EngineCatalogPath{}, false
	}
	path, err := resourcetree.EngineCatalogPathFromLocator(model, loc)
	if err != nil {
		return plugin.EngineCatalogPath{}, false
	}
	fingerprint, err := authorizationfacts.PrimaryCandidateFingerprint(path)
	return path, err == nil && fingerprint == doc.ID
}
