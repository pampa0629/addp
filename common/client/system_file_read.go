package client

import (
	"context"
	"strconv"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
)

// ManagerFileExpectedSource is the exact current or generated boundary for a current
// Human read. It contains no execution, task, Grant, credential or Allow.
type ManagerFileExpectedSource struct {
	EngineVersion   string                                     `json:"engine_version"`
	FileComposition authorizationfacts.FileCompositionBoundary `json:"file_composition"`
}

func (s *ManagerFileExpectedSource) Validate() error {
	if s == nil {
		return ErrManagerPreviewReadDenied
	}
	version, err := strconv.ParseInt(s.EngineVersion, 10, 64)
	if err != nil || version <= 0 || strconv.FormatInt(version, 10) != s.EngineVersion || s.FileComposition.DataType() == "" {
		return ErrManagerPreviewReadDenied
	}
	primary, err := s.FileComposition.PrimaryTarget()
	if err != nil || (primary.Segments[len(primary.Segments)-1].Term != plugin.EngineCatalogTermFile && primary.Segments[len(primary.Segments)-1].Term != plugin.EngineCatalogTermObject) || ((s.FileComposition.Format == "ifc" || s.FileComposition.Format == "stl" || s.FileComposition.Format == "glb") && len(s.FileComposition.Refs) != 1) {
		return ErrManagerPreviewReadDenied
	}
	if s.FileComposition.Format == "glb" && s.FileComposition.Refs[0].Role != "model" {
		return ErrManagerPreviewReadDenied
	}
	return nil
}

// CheckManagerFileRead uses the existing fixed ManagerPreview endpoint for
// current exact-file content and generated results.
// System must compare expected_source with current trusted facts in the same
// Human permission decision. No primary-only fallback, terminal EA or ticket.
func (c *SystemServiceClient) CheckManagerFileRead(ctx context.Context, credential string, expectedSource ManagerFileExpectedSource) error {
	if err := expectedSource.Validate(); err != nil {
		return err
	}
	primary, _ := expectedSource.FileComposition.PrimaryTarget()
	expectedSource.FileComposition = *expectedSource.FileComposition.Clone()
	targets := []plugin.EngineCatalogPath{primary}
	request := struct {
		Targets        []plugin.EngineCatalogPath `json:"targets"`
		ExpectedSource ManagerFileExpectedSource  `json:"expected_source"`
	}{Targets: targets, ExpectedSource: expectedSource}
	return c.checkSourceReadRequest(ctx, credential, targets, "/api/v1/system/engine-access/read-observations/current-source", false, request)
}
