package executor

import (
	"context"
	"errors"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
)

func TestEncodedRecordExportExecutorStreamsBatchesAndCountsRecords(t *testing.T) {
	source := &encodedRecordSource{
		rawCopyContentStore: &rawCopyContentStore{},
		batches: []*engineplugin.EncodedRecordBatchData{
			{Content: []byte("{\"_id\":{\"$oid\":\"a\"}}\n"), Records: 1, Offset: 0},
			{Content: []byte("{\"_id\":{\"$oid\":\"b\"}}\n"), Records: 1, Offset: 1},
			{},
		},
	}
	target := &rawCopyContentStore{files: map[string][]byte{"exports/people.ejsonl": []byte("old")}}
	exec := &EncodedRecordExportExecutor{
		SourceRecordReader: source, TargetContentWriter: target, TargetDelete: target,
	}
	var events []EncodedRecordExportProgressEvent
	metrics, err := exec.Execute(context.Background(), EncodedRecordExportPlan{
		Source:    EncodedRecordExportEndpointPlan{Path: mongoCollectionPathForTest()},
		Target:    EncodedRecordExportEndpointPlan{Path: engineplugin.FileItemPath(2, "exports/people.ejsonl"), DeleteBeforeWrite: true},
		Format:    "mongodb_extended_jsonl",
		BatchSize: 1,
		ProgressCallback: func(_ context.Context, event EncodedRecordExportProgressEvent) error {
			events = append(events, event)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	want := "{\"_id\":{\"$oid\":\"a\"}}\n{\"_id\":{\"$oid\":\"b\"}}\n"
	if got := string(target.files["exports/people.ejsonl"]); got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
	if metrics.RecordsRead != 2 || metrics.RecordsWritten != 2 || metrics.BytesWritten != int64(len(want)) {
		t.Fatalf("metrics = %#v", metrics)
	}
	if len(events) != 3 || !events[len(events)-1].Final {
		t.Fatalf("events = %#v", events)
	}
}

func TestEncodedRecordExportExecutorPassesProtectionBeforeEncoding(t *testing.T) {
	source := &encodedRecordSource{rawCopyContentStore: &rawCopyContentStore{}, batches: []*engineplugin.EncodedRecordBatchData{{}}}
	target := &rawCopyContentStore{files: map[string][]byte{}}
	exec := &EncodedRecordExportExecutor{SourceRecordReader: source, TargetContentWriter: target, TargetDelete: target}
	called := false
	_, err := exec.Execute(context.Background(), EncodedRecordExportPlan{
		Source: EncodedRecordExportEndpointPlan{Path: mongoCollectionPathForTest()},
		Target: EncodedRecordExportEndpointPlan{Path: engineplugin.FileItemPath(2, "exports/people.ejsonl")},
		Format: "mongodb_extended_jsonl", BatchSize: 1,
		BeforeEncode: func(document map[string]interface{}) error {
			called = true
			document["phone"] = "136****4499"
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !source.beforeEncodeInstalled || !called {
		t.Fatalf("before encode installed=%t called=%t", source.beforeEncodeInstalled, called)
	}
}

func TestEncodedRecordExportExecutorDeletesTargetWhenFinalProgressFails(t *testing.T) {
	targetPath := engineplugin.FileItemPath(2, "exports/people.ejsonl")
	source := &encodedRecordSource{
		rawCopyContentStore: &rawCopyContentStore{},
		batches: []*engineplugin.EncodedRecordBatchData{
			{Content: []byte("{\"_id\":{\"$oid\":\"a\"}}\n"), Records: 1},
			{},
		},
	}
	target := &rawCopyContentStore{files: map[string][]byte{}}
	exec := &EncodedRecordExportExecutor{
		SourceRecordReader: source, TargetContentWriter: target, TargetDelete: target,
	}

	_, err := exec.Execute(context.Background(), EncodedRecordExportPlan{
		Source:    EncodedRecordExportEndpointPlan{Path: mongoCollectionPathForTest()},
		Target:    EncodedRecordExportEndpointPlan{Path: targetPath},
		Format:    "mongodb_extended_jsonl",
		BatchSize: 1,
		ProgressCallback: func(_ context.Context, event EncodedRecordExportProgressEvent) error {
			if event.Final {
				return errors.New("persist checkpoint")
			}
			return nil
		},
	})
	if err == nil {
		t.Fatal("Execute() error = nil, want final progress error")
	}
	if !target.deleted[targetPath.StringPath()] {
		t.Fatal("target was not deleted after final progress failure")
	}
	if _, ok := target.files[targetPath.StringPath()]; ok {
		t.Fatal("partial target still exists after final progress failure")
	}
}

type encodedRecordSource struct {
	*rawCopyContentStore
	batches               []*engineplugin.EncodedRecordBatchData
	beforeEncodeInstalled bool
}

func (s *encodedRecordSource) OpenEncodedRecordReadSession(_ context.Context, _ engineplugin.ConnectionInfo, _ engineplugin.EngineCatalogPath, opts engineplugin.EncodedRecordReadSessionOptions) (engineplugin.EncodedRecordReadSession, error) {
	s.beforeEncodeInstalled = opts.BeforeEncode != nil
	if opts.BeforeEncode != nil {
		if err := opts.BeforeEncode(map[string]interface{}{"phone": "13661384499"}); err != nil {
			return nil, err
		}
	}
	return &encodedRecordSession{batches: append([]*engineplugin.EncodedRecordBatchData(nil), s.batches...)}, nil
}

type encodedRecordSession struct {
	batches []*engineplugin.EncodedRecordBatchData
	index   int
}

func (s *encodedRecordSession) ReadBatch(context.Context, int) (*engineplugin.EncodedRecordBatchData, error) {
	if s.index >= len(s.batches) {
		return &engineplugin.EncodedRecordBatchData{}, nil
	}
	batch := s.batches[s.index]
	s.index++
	return batch, nil
}

func (s *encodedRecordSession) Close(context.Context) error { return nil }

func mongoCollectionPathForTest() engineplugin.EngineCatalogPath {
	return engineplugin.EngineCatalogPath{
		Version: engineplugin.EngineCatalogPathVersion, EngineID: 1,
		Segments: []engineplugin.EngineCatalogSegment{
			{Term: engineplugin.EngineCatalogTermServer, Kind: engineplugin.EngineCatalogKindRoot, Name: "mongodb"},
			{Term: engineplugin.EngineCatalogTermDatabase, Kind: engineplugin.EngineCatalogKindNamespace, Name: "Outdoor"},
			{Term: engineplugin.EngineCatalogTermCollection, Kind: engineplugin.EngineCatalogKindCollection, Name: "Persons"},
		},
	}
}
