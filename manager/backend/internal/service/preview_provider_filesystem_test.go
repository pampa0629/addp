package service

import (
	"reflect"
	"testing"
)

func TestCandidateSiblingPathVariants(t *testing.T) {
	t.Parallel()

	got := candidateSiblingPathVariants("/shp/farmland.dbf")
	want := []string{"/shp/farmland.dbf", "shp/farmland.dbf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidateSiblingPathVariants = %+v, want %+v", got, want)
	}

	got = candidateSiblingPathVariants("shp/farmland.dbf")
	want = []string{"shp/farmland.dbf", "/shp/farmland.dbf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidateSiblingPathVariants = %+v, want %+v", got, want)
	}
}

func TestIsFileSystemNotFoundErr(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		errMsg string
		want   bool
	}{
		{"failed to open NFS file /demo.prj: no such file or directory", true},
		{"object not found", true},
		{"permission denied", false},
	}

	for _, tc := range testcases {
		tc := tc
		t.Run(tc.errMsg, func(t *testing.T) {
			t.Parallel()
			got := isFileSystemNotFoundErr(errString(tc.errMsg))
			if got != tc.want {
				t.Fatalf("isFileSystemNotFoundErr(%q) = %v, want %v", tc.errMsg, got, tc.want)
			}
		})
	}
}

type errString string

func (e errString) Error() string {
	return string(e)
}
