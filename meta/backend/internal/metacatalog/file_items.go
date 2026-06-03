package metacatalog

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

type FileCatalogDetectedItemPlan struct {
	ItemType    string
	ItemName    string
	FullName    string
	Fingerprint string
	SizeBytes   int64
	Attributes  models.JSONMap
}

func PlanFileCatalogDetectedItem(engineID uint, dirPath string, item *metaitem.DetectedItem, itemType string) (FileCatalogDetectedItemPlan, bool) {
	if item == nil {
		return FileCatalogDetectedItemPlan{}, false
	}
	itemName, fullName := FileCatalogDetectedItemName(dirPath, item)
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))
	if len(item.Fields) > 0 {
		metaattr.SetTableFields(attrs, item.Fields)
	}
	return FileCatalogDetectedItemPlan{
		ItemType:    itemType,
		ItemName:    itemName,
		FullName:    fullName,
		Fingerprint: commonModels.GenerateItemFingerprint(engineID, fullName),
		SizeBytes:   item.Size(),
		Attributes:  attrs,
	}, true
}

func FileCatalogDetectedItemName(dirPath string, item *metaitem.DetectedItem) (name, fullName string) {
	if item == nil {
		return inferFileCatalogItemName(dirPath)
	}
	if item.Layout != format.LayoutWhole && item.PrimaryContentPath != "" {
		cleaned := strings.Trim(item.PrimaryContentPath, "/")
		return filepath.Base(cleaned), cleaned
	}
	if item.PhysicalPath != "" && item.Layout != format.LayoutWhole {
		cleaned := strings.Trim(item.PhysicalPath, "/")
		return filepath.Base(cleaned), cleaned
	}
	return inferFileCatalogItemName(dirPath)
}

func inferFileCatalogItemName(dirPath string) (name, fullName string) {
	cleaned := strings.Trim(dirPath, "/")
	parts := strings.Split(cleaned, "/")
	if len(parts) == 0 {
		return "unknown", dirPath
	}
	name = parts[len(parts)-1]
	if name == "" {
		name = fmt.Sprintf("item_%s", strings.ReplaceAll(strings.Trim(dirPath, "/"), "/", "_"))
	}
	fullName = cleaned
	return name, fullName
}
