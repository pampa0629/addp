package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMinIOConfigurationRejectsAmbiguousAndPrivilegedInputs(t *testing.T) {
	config := map[string]string{"address": "127.0.0.1:9000", "scope": "infra", "access_key": "ADDPAABBCCDDEEFF0011", "secret_key": "private-observer-secret", "container_id": strings.Repeat("a", 64), "container_started_at": "2026-01-01T00:00:00Z"}
	encode := func() string { data, _ := json.Marshal(config); return string(data) }
	if _, err := decode(strings.NewReader(encode())); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"address": "https://127.0.0.1:9000", "scope": "tenant", "access_key": "root-key", "secret_key": "short", "container_id": "foreign", "container_started_at": "2099-01-01T00:00:00Z"} {
		previous := config[key]
		config[key] = value
		if _, err := decode(strings.NewReader(encode())); err == nil {
			t.Fatal("invalid private target accepted", key)
		}
		config[key] = previous
	}
	for _, data := range []string{strings.TrimSuffix(encode(), "}") + `,"scope":"business"}`, strings.TrimSuffix(encode(), "}") + `,"root_password":"must-not-be-readable"}`, encode() + " {}", encode() + strings.Repeat(" ", 32768)} {
		if _, err := decode(strings.NewReader(data)); err == nil {
			t.Fatal("duplicate, unknown or oversized configuration accepted")
		}
	}
	if err := run([]string{"arbitrary-target"}); err == nil {
		t.Fatal("source opened a command surface")
	}
}
