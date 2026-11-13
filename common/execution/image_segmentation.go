package execution

import (
	"errors"
	"path"
	"strings"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
)

const TaskTypeImageSegmentation = "image_segmentation"

type ImageSegmentationSource struct {
	Locator         string                                     `json:"locator"`
	EngineID        uint                                       `json:"engine_id"`
	EngineVersion   int64                                      `json:"engine_version"`
	ItemID          uint                                       `json:"item_id"`
	ItemFingerprint string                                     `json:"item_fingerprint"`
	Composition     authorizationfacts.FileCompositionBoundary `json:"composition"`
}

type ImageSegmentationTarget struct {
	Locator       string                   `json:"locator"`
	EngineID      uint                     `json:"engine_id"`
	EngineVersion int64                    `json:"engine_version"`
	Path          plugin.EngineCatalogPath `json:"path"`
}

type ImageSegmentationConfig struct {
	Source         ImageSegmentationSource `json:"source"`
	Target         ImageSegmentationTarget `json:"target"`
	Text           string                  `json:"text"`
	ModelProfileID string                  `json:"model_profile_id"`
}

func (c ImageSegmentationConfig) Validate() error {
	source, target := c.Source, c.Target
	primary, err := source.Composition.PrimaryTarget()
	if err != nil || primary.EngineID != source.EngineID || source.EngineVersion <= 0 || source.Locator == "" ||
		source.ItemID == 0 || source.ItemFingerprint == "" || len(source.Composition.Refs) != 1 || source.Composition.Format != "tiff" {
		return errors.New("invalid segmentation source")
	}
	object, err := plugin.RequireObjectLeafPath(target.Path)
	if err != nil || target.Locator == "" || target.Path.EngineID != target.EngineID || target.EngineVersion <= 0 ||
		strings.Contains(object, "\\") || path.Clean(object) != object || strings.ToLower(path.Ext(object)) != ".geojson" {
		return errors.New("invalid segmentation target")
	}
	if c.Text == "" || strings.TrimSpace(c.Text) != c.Text || len(c.Text) > 256 || c.ModelProfileID == "" {
		return errors.New("invalid segmentation prompt or profile")
	}
	return nil
}
