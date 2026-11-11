package main

import (
	"strings"
	"testing"
)

func TestSourceConfiguration(t *testing.T) {
	for _, kind := range []string{"redpanda", "kafka-connect"} {
		if _, err := configuration(strings.NewReader(`{"kind":"` + kind + `","address":"172.20.0.2"}`)); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{`{}`, `{"kind":"other","address":"127.0.0.1"}`, `{"kind":"redpanda","address":"8.8.8.8"}`, `{"kind":"redpanda","address":"127.0.0.1","url":"http://example.org"}`, `{"kind":"redpanda","address":"127.0.0.1"} {}`} {
		if _, err := configuration(strings.NewReader(raw)); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}
