package plugin

import "testing"

func TestNormalizeWatermarkFields(t *testing.T) {
	fields, err := NormalizeWatermarkFields(" updated_at ", []string{"tenant_id", "id"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"updated_at", "tenant_id", "id"}
	for index := range want {
		if fields[index] != want[index] {
			t.Fatalf("fields = %#v, want %#v", fields, want)
		}
	}
	single, err := NormalizeWatermarkFields(" id ", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(single) != 1 || single[0] != "id" {
		t.Fatalf("single fields = %#v, want []string{\"id\"}", single)
	}
	for _, testCase := range []struct {
		name        string
		watermark   string
		tieBreakers []string
	}{
		{name: "missing watermark", tieBreakers: []string{"id"}},
		{name: "duplicate", watermark: "updated_at", tieBreakers: []string{"id", "id"}},
		{name: "watermark repeated", watermark: "updated_at", tieBreakers: []string{"updated_at"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := NormalizeWatermarkFields(testCase.watermark, testCase.tieBreakers); err == nil {
				t.Fatal("NormalizeWatermarkFields succeeded, want error")
			}
		})
	}
}
