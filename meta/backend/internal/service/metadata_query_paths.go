package service

import (
	"strings"

	"github.com/addp/meta/internal/metapath"
)

func catalogPathCandidates(catalogPath string) []string {
	trimmed := normalizeCatalogPath(catalogPath)
	if trimmed == "" {
		return []string{""}
	}
	candidates := []string{trimmed}
	if strings.Contains(trimmed, "/") {
		candidates = append(candidates, strings.ReplaceAll(trimmed, "/", "."))
	}
	return candidates
}

func normalizeCatalogPath(catalogPath string) string {
	return metapath.SanitizeFSPath(catalogPath)
}
