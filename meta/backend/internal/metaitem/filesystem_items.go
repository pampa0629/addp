package metaitem

import (
	"path/filepath"
	"strings"

	"github.com/addp/meta/internal/dataitem"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
)

func FileSystemDetectedItemName(dirPath string, item *DetectedItem) (name, fullName string) {
	if item == nil {
		return inferFileSystemItemName(dirPath)
	}
	if item.Organization != dataitem.OrganizationWhole && item.EntryPath != "" {
		cleaned := strings.Trim(item.EntryPath, "/")
		return filepath.Base(cleaned), cleaned
	}
	if item.PhysicalPath != "" && item.Organization != dataitem.OrganizationWhole {
		cleaned := strings.Trim(item.PhysicalPath, "/")
		return filepath.Base(cleaned), cleaned
	}
	return inferFileSystemItemName(dirPath)
}

func ApplyContainerSummary(attrs models.JSONMap, detected *DetectedItem) {
	if attrs == nil || detected == nil || detected.DataType != dataitem.DataTypeContainer {
		return
	}
	metaattr.UpsertNested(attrs, "type_info", "container", map[string]interface{}{
		"children":       []map[string]interface{}{},
		"child_count":    0,
		"resource_count": 1,
	})
}

func inferFileSystemItemName(dirPath string) (name, fullName string) {
	cleaned := strings.Trim(dirPath, "/")
	parts := strings.Split(cleaned, "/")
	if len(parts) == 0 {
		return "unknown", dirPath
	}
	name = parts[len(parts)-1]
	fullName = cleaned
	return name, fullName
}
