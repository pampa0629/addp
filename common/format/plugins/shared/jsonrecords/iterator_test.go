package jsonrecords

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestRecordIteratorDoesNotTreatUnclosedDocumentAsComplete(t *testing.T) {
	for _, raw := range []string{`[{"id":1}`, `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"id":1},"geometry":null}]`, `[{"id":1}] {"id":2}`} {
		it, err := NewRecordIterator(strings.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 4; i++ {
			_, err = it.Next()
			if err != nil {
				break
			}
		}
		if err == nil || errors.Is(err, io.EOF) {
			t.Fatalf("invalid document reported successful EOF: %v", err)
		}
	}
}

func TestRecordIteratorCompleteArraysAndFeatureCollectionsHaveStableEOF(t *testing.T) {
	for _, raw := range []string{`[{"id":1}]`, `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"id":1},"geometry":null}],"name":"sample"}`, "{\"id\":1}\n{\"id\":2}\n"} {
		it, err := NewRecordIterator(strings.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for {
			_, err = it.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			count++
		}
		if count < 1 {
			t.Fatal("no records")
		}
		for i := 0; i < 2; i++ {
			if _, err := it.Next(); !errors.Is(err, io.EOF) {
				t.Fatal("unstable EOF", err)
			}
		}
	}
}
