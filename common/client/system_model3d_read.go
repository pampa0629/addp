package client

import (
	"context"
	"strconv"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
)

// ManagerModel3DExpectedSource is the exact current or generated boundary for a current
// Human read. It contains no execution, task, Grant, credential or Allow.
type ManagerModel3DExpectedSource struct {
	EngineVersion   string                                     `json:"engine_version"`
	FileComposition authorizationfacts.FileCompositionBoundary `json:"file_composition"`
}

func (s *ManagerModel3DExpectedSource) Validate() error {
	if s == nil {
		return ErrManagerPreviewReadDenied
	}
	version, err := strconv.ParseInt(s.EngineVersion, 10, 64)
	if err != nil || version <= 0 || strconv.FormatInt(version, 10) != s.EngineVersion || !commonExecution.ManagerModel3DFileFormat(s.FileComposition.Format) {
		return ErrManagerPreviewReadDenied
	}
	primary, err := s.FileComposition.PrimaryTarget()
	if err != nil || (primary.Segments[len(primary.Segments)-1].Term != plugin.EngineCatalogTermFile && primary.Segments[len(primary.Segments)-1].Term != plugin.EngineCatalogTermObject) || ((s.FileComposition.Format == "ifc" || s.FileComposition.Format == "stl") && len(s.FileComposition.Refs) != 1) {
		return ErrManagerPreviewReadDenied
	}
	return nil
}

// CheckManagerModel3DRead uses the existing fixed ManagerPreview endpoint for
// current raw model content and generated results.
// System must compare expected_source with current trusted facts in the same
// Human permission decision. No primary-only fallback, terminal EA or ticket.
func (c *SystemServiceClient) CheckManagerModel3DRead(ctx context.Context, credential string, expectedSource ManagerModel3DExpectedSource) error {
	if err := expectedSource.Validate(); err != nil {
		return err
	}
	primary, _ := expectedSource.FileComposition.PrimaryTarget()
	expectedSource.FileComposition = *expectedSource.FileComposition.Clone()
	targets := []plugin.EngineCatalogPath{primary}
	request := struct {
		Targets        []plugin.EngineCatalogPath   `json:"targets"`
		ExpectedSource ManagerModel3DExpectedSource `json:"expected_source"`
	}{Targets: targets, ExpectedSource: expectedSource}
	return c.checkManagerSourceReadRequest(ctx, credential, targets, "/api/v1/system/engine-access/read-checks/manager-preview", false, request)
}
