package objectstore

import (
	"encoding/json"
	"os"
	"testing"
)

func TestCrossLanguageReferenceContract(t *testing.T) {
	data, err := os.ReadFile("testdata/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases struct {
		Valid   []string `json:"valid_refs"`
		Invalid []string `json:"invalid_refs"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, value := range cases.Valid {
		ref, err := Parse(value)
		if err != nil {
			t.Fatal(value, err)
		}
		round, err := ref.URI()
		if err != nil || round != value {
			t.Fatal(value, round, err)
		}
	}
	for _, value := range cases.Invalid {
		if _, err := Parse(value); err == nil {
			t.Fatal("accepted", value)
		}
	}
}
