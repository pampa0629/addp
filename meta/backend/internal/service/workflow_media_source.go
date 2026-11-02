package service

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/envi"
	commonjson "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/metaitem"
)

// Source-only inspection is part of the existing Meta scan authority, not a
// Human/Manager content Grant. It must still carry the exact observed files.
func runtimeMediaSourcePlan(source *commonModels.Engine, item *metaitem.DetectedItem, physicalPath string) (workflowaccess.SourcePlan, string, error) {
	if source == nil || item == nil {
		return workflowaccess.SourcePlan{}, "", fmt.Errorf("media inspection source is missing")
	}
	resourceType := resourcetree.TypeFile
	if source.EngineType == "minio" || source.EngineType == "s3" {
		resourceType = resourcetree.TypeObject
	}
	resolve := func(name string) (workflowaccess.Source, error) {
		locator := resourcetree.LocatorFromFullName(source.ID, source.EngineType, string(resourceType), name, nil)
		if locator == nil {
			return workflowaccess.Source{}, fmt.Errorf("cannot build media source locator")
		}
		return workflowaccess.ResolveSource(workflowaccess.ResourceSpec{Engine: source, Locator: locator, Kind: workflowaccess.KindFile, Format: item.Format})
	}
	main, err := resolve(physicalPath)
	if err != nil {
		return workflowaccess.SourcePlan{}, "", err
	}
	headerPath := ""
	if item.Format == string(format.FormatENVI) {
		native := commonjson.Section(item.Attributes, "format_info.envi")
		sha, _ := native["header_sha256"].(string)
		digest, decodeErr := hex.DecodeString(sha)
		if (source.EngineType != "nfs" && source.EngineType != "minio") || item.Layout != format.LayoutMulti || item.PrimaryContentPath != physicalPath || native["composition_complete"] != true || native["profile"] != envi.Profile || decodeErr != nil || len(digest) != 32 || strings.ToLower(sha) != sha || len(item.RefList) != 2 {
			return workflowaccess.SourcePlan{}, "", fmt.Errorf("ENVI inspection requires a complete observed pair")
		}
		primaryCount := 0
		for _, ref := range item.RefList {
			if !ref.Required {
				return workflowaccess.SourcePlan{}, "", fmt.Errorf("ENVI components must be required")
			}
			if ref.Primary {
				primaryCount++
				if ref.Path != physicalPath || ref.Role != "raster" {
					return workflowaccess.SourcePlan{}, "", fmt.Errorf("ENVI primary does not match the observed pair")
				}
			} else {
				if ref.Role != "header" || native["header_path"] != ref.Path {
					return workflowaccess.SourcePlan{}, "", fmt.Errorf("ENVI header does not match the observed pair")
				}
				headerPath = ref.Path
			}
		}
		if primaryCount != 1 || headerPath == "" {
			return workflowaccess.SourcePlan{}, "", fmt.Errorf("ENVI inspection requires one primary and one header")
		}
		header, err := resolve(headerPath)
		if err != nil {
			return workflowaccess.SourcePlan{}, "", err
		}
		main.RelatedFiles = []workflowaccess.RelatedFile{{Role: "header", Access: header.Access}}
		main.Metadata = commonModels.JSONMap{"expected_header_sha256": sha}
	}
	plan, err := workflowaccess.NewSourcePlan(main)
	return plan, headerPath, err
}
