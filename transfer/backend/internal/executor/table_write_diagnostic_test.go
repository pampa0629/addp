package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/resume"
)

type diagnosticSource struct {
	info             *datatype.TableInfo
	rows             []*plugin.BatchData
	openErr, readErr error
}

func (s *diagnosticSource) Open(context.Context) (TableBatchReader, error) { return s, s.openErr }
func (s *diagnosticSource) TableInfo() *datatype.TableInfo                 { return s.info }
func (*diagnosticSource) SpatialInfo() *datatype.SpatialInfo               { return nil }
func (*diagnosticSource) Close(context.Context) error                      { return nil }
func (*diagnosticSource) ResumeMarker() *resume.Marker                     { return nil }
func (s *diagnosticSource) ReadBatch(context.Context, int) (*plugin.BatchData, error) {
	if len(s.rows) == 0 {
		return nil, s.readErr
	}
	row := s.rows[0]
	s.rows = s.rows[1:]
	return row, nil
}

type diagnosticTargetProvider struct {
	fakeBatchWriter
	fail  string
	cause error
}

func (p *diagnosticTargetProvider) failure(stage string) error {
	if p.fail == stage {
		return p.cause
	}
	return nil
}
func (p *diagnosticTargetProvider) DeleteResource(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath) error {
	return p.failure("delete")
}
func (p *diagnosticTargetProvider) PrepareTableWrite(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.TableWriteOptions) error {
	return p.failure("prepare")
}
func (p *diagnosticTargetProvider) WriteBatch(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, *plugin.BatchData, plugin.BatchWriteOptions) error {
	return p.failure("write")
}
func (p *diagnosticTargetProvider) OpenTableWriteSession(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.TableWriteSessionOptions) (plugin.TableWriteSession, error) {
	return &diagnosticSession{p}, p.failure("open")
}

type diagnosticSession struct{ provider *diagnosticTargetProvider }

func (s *diagnosticSession) WriteBatch(context.Context, *plugin.BatchData) error {
	return s.provider.failure("write")
}
func (s *diagnosticSession) Close(context.Context) error { return s.provider.failure("close") }
func (*diagnosticSession) Abort(context.Context) error   { return nil }

func TestTablePipelineTracksEnteredTargetEffectsWithoutInferringRows(t *testing.T) {
	for _, tc := range []struct {
		name, failure                                                                               string
		managed, session, deletion, replacement, sourceOpen, sourceRead, reject, progress, possible bool
	}{
		{name: "pure definition rejection", reject: true},
		{name: "source open", sourceOpen: true},
		{name: "source read before first write", managed: true, sourceRead: true},
		{name: "delete failure", failure: "delete", deletion: true, possible: true},
		{name: "prepare failure", failure: "prepare", possible: true},
		{name: "source read after prepare", sourceRead: true, possible: true},
		{name: "session replacement open", failure: "open", session: true, replacement: true, possible: true},
		{name: "ordinary session open", failure: "open", session: true, managed: true},
		{name: "direct write", failure: "write", managed: true, possible: true},
		{name: "session write", failure: "write", managed: true, session: true, possible: true},
		{name: "session close", failure: "close", managed: true, session: true, possible: true},
		{name: "source read after write", managed: true, possible: true},
		{name: "progress after write", managed: true, progress: true, possible: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := errors.New("private cause")
			provider := &diagnosticTargetProvider{fail: tc.failure, cause: cause}
			target := &nativeTableBatchTarget{preparer: provider, writer: provider, managedExisting: tc.managed, replace: tc.replacement, writeOptions: plugin.BatchWriteOptions{Method: "insert"}}
			if tc.session {
				target.tableSessionProvider = provider
				target.writeOptions.Method = "copy"
			}
			if tc.deletion {
				target.deleter = &engineTargetResourceDeleter{provider: provider}
			}
			source := &diagnosticSource{info: &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}}}}
			if tc.reject {
				source.info.Fields[0].Type = datatype.FieldTypeArray
				source.info.Fields[0].ElementType = datatype.FieldTypeJSON
				target.definitionChecker = &postgresql.PostgreSQLPlugin{}
			}
			if tc.sourceOpen {
				source.openErr = cause
			}
			if tc.sourceRead {
				source.readErr = cause
			} else {
				source.rows = []*plugin.BatchData{{Rows: []map[string]interface{}{{"id": 1}}}}
			}
			if tc.name == "source read after write" {
				source.readErr = cause
			}
			pipeline := &TablePipeline{Source: source, Target: target}
			if tc.progress {
				pipeline.ProgressCallback = func(context.Context, TableProgressEvent) error { return cause }
			}
			_, err := pipeline.Execute(t.Context())
			if err == nil || errors.Is(err, ErrTargetMayHaveChanged) != tc.possible {
				t.Fatalf("possible=%v got %v", tc.possible, err)
			}
			if !tc.reject && !errors.Is(err, cause) {
				t.Fatalf("lost original cause: %v", err)
			}
		})
	}
}
