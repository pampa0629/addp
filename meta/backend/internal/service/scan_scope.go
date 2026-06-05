package service

import (
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresolver"
)

func (s *ScanService) ResolveScanScope(tenantID uint, opts scanflow.Options) (scanflow.Scope, error) {
	return s.ensureScopeResolver().ResolveScope(tenantID, opts)
}

func (s *ScanService) ensureScopeResolver() *scanresolver.Resolver {
	if s.scopeResolver == nil {
		s.scopeResolver = scanresolver.New(s.db)
	}
	return s.scopeResolver
}
