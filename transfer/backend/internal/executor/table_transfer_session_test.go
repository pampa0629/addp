package executor

import (
	"context"
	"errors"
	"strings"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
)

func TestOpenNativeSourceAcceptsSessionOnlyProvider(t *testing.T) {
	provider := &fakeBatchReader{}
	executor := &TableTransferExecutor{SourceTableSessionProvider: provider}

	if _, err := executor.openSource(TableSourcePlan{
		Kind: TableEndpointNative,
		Path: engineplugin.EngineCatalogPath{},
	}); err != nil {
		t.Fatalf("openSource() error = %v", err)
	}
}

func TestNativeTableSessionBatchWriterAbortsAfterCloseFailure(t *testing.T) {
	session := &closeFailingTableWriteSession{closeErr: errors.New("refresh count failed")}
	writer := &nativeTableSessionBatchWriter{session: session}

	err := writer.Close(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refresh count failed") {
		t.Fatalf("Close() error = %v, want close failure", err)
	}
	if session.abortCalls != 1 {
		t.Fatalf("Abort() calls = %d, want 1", session.abortCalls)
	}
	if !writer.closed {
		t.Fatal("writer.closed = false after close failure cleanup")
	}
}

type closeFailingTableWriteSession struct {
	closeErr   error
	abortCalls int
}

func (*closeFailingTableWriteSession) WriteBatch(context.Context, *engineplugin.BatchData) error {
	return nil
}

func (s *closeFailingTableWriteSession) Close(context.Context) error {
	return s.closeErr
}

func (s *closeFailingTableWriteSession) Abort(context.Context) error {
	s.abortCalls++
	return nil
}

func TestOpenNativeTargetAcceptsSessionOnlyProvider(t *testing.T) {
	provider := &fakeNativeTableWriter{}
	executor := &TableTransferExecutor{
		TargetNativePreparer:       provider,
		TargetTableSessionProvider: provider,
	}

	if _, err := executor.openTarget(TableTargetPlan{
		Kind: TableEndpointNative,
		Path: engineplugin.EngineCatalogPath{},
	}); err != nil {
		t.Fatalf("openTarget() error = %v", err)
	}
}
