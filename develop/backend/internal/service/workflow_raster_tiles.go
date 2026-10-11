package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/resourcetree"
	developi18n "github.com/addp/develop/backend/i18n"
)

func workflowRasterTilesOutputSchema(title string) map[string]interface{} {
	return map[string]interface{}{
		"type": "object", "title": title,
		"properties": map[string]interface{}{"tiles": map[string]interface{}{
			"type": "object", "additionalProperties": workflowResourceOutputSchema(title),
		}},
		"required": []interface{}{"tiles"}, "additionalProperties": false,
	}
}

// Batch scopes are authorized plans, not produced data items. Reject overlapping
// writes before dispatch, including a scalar save nested in a batch's scope.
func rejectWorkflowBatchTargetOverlap(targets []WorkflowProducedTarget) error {
	for i, left := range targets {
		for _, right := range targets[i+1:] {
			if left.EngineID != right.EngineID || (!left.BatchScope && !right.BatchScope) {
				continue
			}
			if (left.BatchScope && rasterPathContains(left.Path, right.Path)) ||
				(right.BatchScope && rasterPathContains(right.Path, left.Path)) {
				return fmt.Errorf("%s", commoni18n.ForLanguage("", developi18n.MsgRasterTileTargetOverlap))
			}
		}
	}
	return nil
}

func rasterPathContains(parent, child []string) bool {
	return len(parent) <= len(child) && slices.Equal(parent, child[:len(parent)])
}

type workflowRasterTilesManifest struct {
	ArtifactType   string                     `json:"artifact_type"`
	SourceWidth    int64                      `json:"source_width"`
	SourceHeight   int64                      `json:"source_height"`
	TileSize       []int64                    `json:"tile_size"`
	Columns        int64                      `json:"columns"`
	Rows           int64                      `json:"rows"`
	TotalTiles     int64                      `json:"total_tiles"`
	CompletedTiles int64                      `json:"completed_tiles"`
	Tiles          map[string]json.RawMessage `json:"tiles"`
	FailedTile     string                     `json:"failed_tile"`
	Publication    string                     `json:"publication"`
}

type workflowRasterTileFact struct {
	ArtifactType  string                       `json:"artifact_type"`
	Name          string                       `json:"name"`
	Window        []int64                      `json:"window"`
	Width         int64                        `json:"width"`
	Height        int64                        `json:"height"`
	SizeBytes     int64                        `json:"size_bytes"`
	UploadedFiles int                          `json:"uploaded_files"`
	Format        string                       `json:"format"`
	Profile       string                       `json:"profile"`
	BandCount     int64                        `json:"band_count"`
	Bands         []workflowRasterTileBandFact `json:"bands"`
	CRS           string                       `json:"crs"`
	SourceCRS     string                       `json:"source_crs"`
	ExtentSRID    int64                        `json:"extent_srid"`
	Transform     []float64                    `json:"transform"`
	Extent        []float64                    `json:"extent"`
}

type workflowRasterTileBandFact struct {
	Band                int64     `json:"band"`
	Dtype               string    `json:"dtype"`
	ColorInterpretation string    `json:"color_interpretation"`
	NoData              *float64  `json:"nodata"`
	NoDataIsNaN         bool      `json:"nodata_is_nan"`
	BlockSize           []int64   `json:"block_size"`
	Overviews           [][]int64 `json:"overviews"`
}

func decodeRasterTileFacts(raw []byte, destination interface{}) error {
	// Decode only the public facts contract; private paths and caller-supplied
	// resource identities never enter the stored execution result.
	if !json.Valid(raw) {
		return fmt.Errorf("invalid JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(destination)
}

func resolveWorkflowRasterTileTargets(planned []WorkflowProducedTarget, resp *WorkflowResponse) ([]WorkflowProducedTarget, error) {
	actual := make([]WorkflowProducedTarget, 0, len(planned))
	for _, target := range planned {
		raw, exists := resp.AllResults[target.TaskID]
		if !target.BatchScope {
			if resp.Status == "success" || exists {
				actual = append(actual, target)
			}
			continue
		}
		if !exists && resp.Status != "success" {
			continue // Failed before this node; an authorized scope is not a write fact.
		}
		children, err := workflowRasterTileTargets(target, raw, resp.Status == "success")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", commoni18n.ForLanguage("", developi18n.MsgRasterTileManifestInvalid), err)
		}
		actual = append(actual, children...)
	}
	return actual, nil
}

func workflowRasterTileTargets(scope WorkflowProducedTarget, raw string, success bool) ([]WorkflowProducedTarget, error) {
	var manifest workflowRasterTilesManifest
	if err := decodeRasterTileFacts([]byte(raw), &manifest); err != nil {
		return nil, fmt.Errorf("invalid JSON")
	}
	if manifest.ArtifactType != "raster_tiles" || manifest.SourceWidth < 1 || manifest.SourceWidth > 2147483647 ||
		manifest.SourceHeight < 1 || manifest.SourceHeight > 2147483647 || len(manifest.TileSize) != 2 ||
		manifest.TileSize[0] < 1 || manifest.TileSize[0] > 2147483647 || manifest.TileSize[1] < 1 || manifest.TileSize[1] > 2147483647 {
		return nil, fmt.Errorf("invalid source or tile grid")
	}
	columns := (manifest.SourceWidth + manifest.TileSize[0] - 1) / manifest.TileSize[0]
	rows := (manifest.SourceHeight + manifest.TileSize[1] - 1) / manifest.TileSize[1]
	if manifest.Columns != columns || manifest.Rows != rows || manifest.TotalTiles != columns*rows ||
		manifest.CompletedTiles != int64(len(manifest.Tiles)) || manifest.CompletedTiles > manifest.TotalTiles || manifest.Tiles == nil {
		return nil, fmt.Errorf("inconsistent counts")
	}
	if (success && manifest.CompletedTiles != manifest.TotalTiles) ||
		(manifest.CompletedTiles == manifest.TotalTiles && (manifest.FailedTile != "" || manifest.Publication != "")) {
		return nil, fmt.Errorf("incomplete successful batch")
	}
	if manifest.CompletedTiles < manifest.TotalTiles && manifest.Publication != "unknown" && manifest.Publication != "not_attempted" {
		return nil, fmt.Errorf("missing failure publication state")
	}
	var records = make(map[string]workflowRasterTileFact, len(manifest.Tiles))
	for key, rawFact := range manifest.Tiles {
		var fact workflowRasterTileFact
		if err := decodeRasterTileFacts(rawFact, &fact); err != nil || len(fact.Window) != 4 {
			return nil, fmt.Errorf("invalid tile window")
		}
		x, y, width, height := fact.Window[0], fact.Window[1], fact.Window[2], fact.Window[3]
		if x < 0 || y < 0 || x >= manifest.SourceWidth || y >= manifest.SourceHeight ||
			x%manifest.TileSize[0] != 0 || y%manifest.TileSize[1] != 0 ||
			width != min(manifest.TileSize[0], manifest.SourceWidth-x) || height != min(manifest.TileSize[1], manifest.SourceHeight-y) ||
			key != fmt.Sprintf("r%d_c%d", y, x) || fact.Name != key+".tif" || fact.Width != width || fact.Height != height ||
			fact.ArtifactType != "raster" || fact.Format != "tiff" || (fact.Profile != "geotiff" && fact.Profile != "cog") ||
			fact.SizeBytes <= 0 || fact.UploadedFiles != 1 {
			return nil, fmt.Errorf("invalid tile publication facts")
		}
		records[key] = fact
	}
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := records[keys[i]].Window, records[keys[j]].Window
		return left[1] < right[1] || (left[1] == right[1] && left[0] < right[0])
	})
	// Confirmed writes must form the row-major prefix of the grid, not arbitrary
	// caller-supplied names under a broadly authorized directory.
	for index, key := range keys {
		if key != fmt.Sprintf("r%d_c%d", int64(index)/columns*manifest.TileSize[1], int64(index)%columns*manifest.TileSize[0]) {
			return nil, fmt.Errorf("noncontiguous tile publication list")
		}
	}
	if manifest.CompletedTiles < manifest.TotalTiles {
		expected := fmt.Sprintf("r%d_c%d", manifest.CompletedTiles/columns*manifest.TileSize[1], manifest.CompletedTiles%columns*manifest.TileSize[0])
		if manifest.FailedTile != expected && !(manifest.CompletedTiles == 0 && manifest.FailedTile == "" && manifest.Publication == "not_attempted") {
			return nil, fmt.Errorf("invalid failed tile")
		}
	}
	resourceType := resourcetree.TypeFile
	if scope.Type == string(resourcetree.TypePrefix) {
		resourceType = resourcetree.TypeObject
	}
	actual := make([]WorkflowProducedTarget, 0, len(keys))
	for _, key := range keys {
		child := workflowProducedTarget(scope.EngineID, resourceType, appendPath(scope.Path, records[key].Name))
		child.TaskID, child.TileKey, child.WriteMode = scope.TaskID, key, scope.WriteMode
		actual = append(actual, child)
	}
	return actual, nil
}
