package deadletter

import (
	"context"
	"errors"
	"testing"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/transfer/internal/models"
)

type fakePayloadSink struct {
	err     error
	calls   int
	topic   string
	key     string
	payload []byte
}

func (s *fakePayloadSink) Write(_ context.Context, topic, identity string, _ time.Time, payload []byte) (PayloadReference, error) {
	s.calls++
	s.topic, s.key, s.payload = topic, identity, append([]byte(nil), payload...)
	if s.err != nil {
		return PayloadReference{}, s.err
	}
	return PayloadReference{Topic: topic, Partition: 0, Offset: 19}, nil
}

type fakeIndexStore struct {
	err         error
	calls       int
	observation *models.DeadLetter
}

func (s *fakeIndexStore) UpsertObservation(_ context.Context, observation *models.DeadLetter) (*models.DeadLetter, error) {
	s.calls++
	s.observation = observation
	if s.err != nil {
		return nil, s.err
	}
	return observation, nil
}

func TestRecorderWritesPayloadBeforeControlIndex(t *testing.T) {
	payloads := &fakePayloadSink{}
	index := &fakeIndexStore{}
	recorder, err := NewRecorder(payloads, index)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := recorder.Record(context.Background(), validRecordRequest())
	if err != nil {
		t.Fatal(err)
	}
	if payloads.calls != 1 || index.calls != 1 || len(payloads.payload) == 0 {
		t.Fatalf("payload calls=%d index calls=%d payload bytes=%d", payloads.calls, index.calls, len(payloads.payload))
	}
	if payloads.topic != "__addp_dlq.7.11" || payloads.key != stored.Identity {
		t.Fatalf("payload topic/key = %q/%q, identity=%q", payloads.topic, payloads.key, stored.Identity)
	}
	if stored.PayloadOffset != 19 || stored.SourceOffset != 41 || stored.OccurrenceCount != 1 {
		t.Fatalf("stored observation = %#v", stored)
	}
}

func TestRecorderDoesNotWriteIndexAfterPayloadFailure(t *testing.T) {
	payloads := &fakePayloadSink{err: errors.New("kafka unavailable")}
	index := &fakeIndexStore{}
	recorder, _ := NewRecorder(payloads, index)
	if _, err := recorder.Record(context.Background(), validRecordRequest()); err == nil {
		t.Fatal("payload failure was ignored")
	}
	if index.calls != 0 {
		t.Fatalf("control index called %d times after payload failure", index.calls)
	}
}

func TestRecorderReturnsIndexFailureAfterPayloadWrite(t *testing.T) {
	payloads := &fakePayloadSink{}
	index := &fakeIndexStore{err: errors.New("postgres unavailable")}
	recorder, _ := NewRecorder(payloads, index)
	if _, err := recorder.Record(context.Background(), validRecordRequest()); err == nil {
		t.Fatal("index failure was ignored")
	}
	if payloads.calls != 1 || index.calls != 1 {
		t.Fatalf("payload calls=%d index calls=%d", payloads.calls, index.calls)
	}
}

func validRecordRequest() RecordRequest {
	now := time.Date(2026, 7, 23, 6, 0, 0, 0, time.UTC)
	return RecordRequest{
		TenantID: 7, TaskID: 11, ExecutionID: "execution-1", ApplyIdentity: "8aa1d865-8d56-4ac3-b9aa-59f50e575c37",
		SourceIdentity: "addp://engine/9/path/orders?type=topic",
		Record:         engineplugin.ChangeRecord{Topic: "orders", Partition: "2", Offset: 41, Timestamp: now, Key: []byte("1"), Value: []byte("not-json")},
		Error:          ErrorDetail{Code: "invalid_json_object", Category: "record_decode", Message: "record value must be a JSON object"},
		DetectedAt:     now.Add(time.Second),
	}
}
