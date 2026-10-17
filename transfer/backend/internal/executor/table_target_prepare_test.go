package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/common/datatype"
	plugin "github.com/addp/common/engine/plugin"
)

type observedTargetPreparer struct {
	*fakeNativeTableWriter
	result   plugin.TableTargetPreparation
	failure  error
	calls    int
	oldCalls int
	options  plugin.TableTargetPrepareOptions
}

func (p *observedTargetPreparer) PrepareTableTarget(_ context.Context, _ plugin.ConnectionInfo, _ plugin.EngineCatalogPath, opts plugin.TableTargetPrepareOptions) (plugin.TableTargetPreparation, error) {
	p.calls++
	p.options = opts
	return p.result, p.failure
}

func (p *observedTargetPreparer) PrepareTableWrite(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.TableWriteOptions) error {
	p.oldCalls++
	return errors.New("separate preparation must not run")
}

func (p *observedTargetPreparer) DeleteResource(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath) error {
	p.oldCalls++
	return errors.New("separate deletion must not run")
}

func TestTableTargetPreparationUsesProviderOwnedBoundary(t *testing.T) {
	for _, disposition := range []plugin.TableTargetDisposition{plugin.TableTargetCreated, plugin.TableTargetRecreated, plugin.TableTargetReused, ""} {
		t.Run(string(disposition), func(t *testing.T) {
			provider := &observedTargetPreparer{fakeNativeTableWriter: &fakeNativeTableWriter{}, result: plugin.TableTargetPreparation{Disposition: disposition}}
			exec := &TableTransferExecutor{SourceNativeReader: &fakeBatchReader{batches: []*plugin.BatchData{{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}}, Rows: []map[string]interface{}{{"id": int64(1)}}}}}, TargetNativePreparer: provider, TargetNativeWriter: provider, TargetTableSessionProvider: provider, TargetDeleteProvider: provider}
			metrics, err := exec.Execute(t.Context(), TableTransferPlan{Source: TableSourcePlan{Kind: TableEndpointNative}, Target: TableTargetPlan{Kind: TableEndpointNative, DeleteBeforeWrite: true, TableWrite: plugin.BatchWriteOptions{Method: "copy"}}})
			if err != nil {
				t.Fatal(err)
			}
			if provider.calls != 1 || provider.oldCalls != 0 || !provider.options.RecreateExisting || len(provider.options.Definition.Fields) != 1 {
				t.Fatalf("split/duplicate target preparation: calls=%d old=%d opts=%+v", provider.calls, provider.oldCalls, provider.options)
			}
			if metrics.TargetPreparation.Disposition != disposition || metrics.RecordsWritten != 1 {
				t.Fatalf("preparation was fabricated/lost: %+v", metrics)
			}
		})
	}
}

func TestNativeTargetPreparationFailureDoesNotReuseOldResult(t *testing.T) {
	provider := &observedTargetPreparer{fakeNativeTableWriter: &fakeNativeTableWriter{}, result: plugin.TableTargetPreparation{Disposition: plugin.TableTargetCreated}, failure: errors.New("target conflict")}
	target := &nativeTableBatchTarget{preparer: provider, writer: provider, replace: true, preparation: plugin.TableTargetPreparation{Disposition: plugin.TableTargetCreated}}
	_, err := target.Open(t.Context(), &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}}}, nil)
	if err == nil || target.preparation != (plugin.TableTargetPreparation{}) || provider.oldCalls != 0 {
		t.Fatalf("failed preparation has successful evidence: result=%+v error=%v", target.preparation, err)
	}
}

func TestTargetPreparationDoesNotProveCommittedOutput(t *testing.T) {
	provider := &observedTargetPreparer{fakeNativeTableWriter: &fakeNativeTableWriter{}, result: plugin.TableTargetPreparation{Disposition: plugin.TableTargetCreated}}
	writeFailure := errors.New("progress persistence failed before commit")
	exec := &TableTransferExecutor{SourceNativeReader: &fakeBatchReader{batches: []*plugin.BatchData{{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}}, Rows: []map[string]interface{}{{"id": int64(1)}}}}}, TargetNativePreparer: provider, TargetNativeWriter: provider, TargetTableSessionProvider: provider}
	metrics, err := exec.Execute(t.Context(), TableTransferPlan{
		Source: TableSourcePlan{Kind: TableEndpointNative}, Target: TableTargetPlan{Kind: TableEndpointNative, TableWrite: plugin.BatchWriteOptions{Method: "copy"}},
		ProgressCallback: func(context.Context, TableProgressEvent) error { return writeFailure },
	})
	if !errors.Is(err, writeFailure) || metrics == nil || metrics.TargetPreparation.Disposition != plugin.TableTargetCreated || !provider.aborted || provider.closed {
		t.Fatalf("preparation confused with commit: metrics=%+v aborted=%v closed=%v error=%v", metrics, provider.aborted, provider.closed, err)
	}
}

func TestPreparationPersistenceFailureAbortsBeforeRows(t *testing.T) {
	provider := &observedTargetPreparer{fakeNativeTableWriter: &fakeNativeTableWriter{}, result: plugin.TableTargetPreparation{Disposition: plugin.TableTargetCreated}}
	receiptFailure := errors.New("receipt lease expired")
	exec := &TableTransferExecutor{SourceNativeReader: &fakeBatchReader{batches: []*plugin.BatchData{{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}}, Rows: []map[string]interface{}{{"id": int64(1)}}}}}, TargetNativePreparer: provider, TargetNativeWriter: provider, TargetTableSessionProvider: provider}
	_, err := exec.Execute(t.Context(), TableTransferPlan{Source: TableSourcePlan{Kind: TableEndpointNative}, Target: TableTargetPlan{Kind: TableEndpointNative, TableWrite: plugin.BatchWriteOptions{Method: "copy"}}, TargetPreparedCallback: func(context.Context, plugin.TableTargetPreparation) error { return receiptFailure }})
	if !errors.Is(err, receiptFailure) || len(provider.batches) != 0 || !provider.aborted || provider.closed {
		t.Fatalf("persist failure reached rows: provider=%+v error=%v", provider, err)
	}
}
