package bootstrap

import (
	"encoding/json"
	"os"
	"testing"
)

func TestCrossLanguageBindingContract(t *testing.T) {
	data, err := os.ReadFile("../testdata/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases struct {
		Bindings []struct {
			Raw, Area, Bucket string
			Error             bool
		} `json:"bindings"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases.Bindings {
		got, err := resolveBucket(c.Area, c.Raw)
		if (err != nil) != c.Error || err == nil && got != c.Bucket {
			t.Fatalf("%+v => %s %v", c, got, err)
		}
	}
}
