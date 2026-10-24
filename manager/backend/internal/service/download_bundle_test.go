package service

import (
	"errors"
	"reflect"
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
)

func TestDownloadBundleNamesPreserveRelativePaths(t *testing.T) {
	for _, test := range []struct {
		name        string
		paths, want []string
	}{
		{"nested same basename", []string{"bucket/models/model.obj", "bucket/models/paint red.mtl", "bucket/models/red/color.png", "bucket/models/blue/color.png"}, []string{"model.obj", "paint red.mtl", "red/color.png", "blue/color.png"}},
		{"segment boundary", []string{"a/one/file.bin", "a/one-other/file.bin"}, []string{"one/file.bin", "one-other/file.bin"}},
		{"root files", []string{"a.bin", "folder/b.bin"}, []string{"a.bin", "folder/b.bin"}},
		{"physical whitespace case and percent", []string{" models / A%20.PNG ", " models /a%20.png", " models / / .png "}, []string{" A%20.PNG ", "a%20.png", " / .png "}},
		{"same directory shapefile", []string{"shapes/roads.shp", "shapes/roads.shx", "shapes/roads.dbf"}, []string{"roads.shp", "roads.shx", "roads.dbf"}},
		{"optional member still determines root", []string{"models/deep/a.obj", "models/missing.png"}, []string{"deep/a.obj", "missing.png"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var refs []models.DownloadRef
			for _, p := range test.paths {
				refs = append(refs, models.DownloadRef{StorageRef: p, FileName: "ignored-name"})
			}
			got, err := downloadBundleNames(refs)
			if err != nil || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("names=%q want=%q err=%v", got, test.want, err)
			}
		})
	}
}

func TestDownloadBundleRejectsInvalidNamesBeforeOpeningAnySource(t *testing.T) {
	cases := [][]string{{"same/a", "same/a"}, {"same/a", "same/a/file"}, {"good/file", "../outside"}, {"good/file", "a/../outside"}, {"good/file", "/absolute"}, {"good/file", "a//b"}, {"good/file", "a/./b"}, {"good/file", "a/b/"}, {"good/file", "a\\b"}, {"good/file", "C:/file"}, {"good/file", "a\x00b"}, {"good/file", "a/\xff"}, {"good/file", ""}}
	for _, paths := range cases {
		t.Run(paths[1], func(t *testing.T) {
			var refs []models.DownloadRef
			for i, p := range paths {
				refs = append(refs, models.DownloadRef{StorageRef: p, Primary: i == 0})
			}
			// A service with no client cannot perform I/O. Both entry points must
			// reject the entire plan before attempting to obtain source information.
			svc := &MetadataService{}
			plan := &models.DownloadPlan{Kind: models.DownloadKindBundle, Refs: refs}
			if _, err := svc.OpenStorageDownloadPlan(t.Context(), nil, plan, nil, "", false); !errors.Is(err, ErrInvalidDownloadBundle) {
				t.Fatalf("Open: %v", err)
			}
			if err := svc.StreamStorageDownloadPlan(t.Context(), nil, plan, nil, "", false, nil); !errors.Is(err, ErrInvalidDownloadBundle) {
				t.Fatalf("Stream: %v", err)
			}
		})
	}
}

func TestDownloadRawMetaRefsAreValidatedBeforeSharedDeduplication(t *testing.T) {
	for _, paths := range [][]string{{"models/a", "models/a"}, {"models/a", "/models/b"}, {"models/a", "models/b/"}, {"models/a", "models/../b"}} {
		var refs []interface{}
		for i, p := range paths {
			refs = append(refs, map[string]interface{}{"path": p, "primary": i == 0, "required": true})
		}
		item := &commonModels.MetaItem{Attributes: map[string]interface{}{"item": map[string]interface{}{"layout": "single", "refs": refs}}}
		svc := &MetadataService{}
		if _, err := svc.resolveStorageDownloadPlan(t.Context(), 9, "minio", "models/a", item, nil); !errors.Is(err, ErrInvalidDownloadBundle) {
			t.Fatalf("paths=%q err=%v", paths, err)
		}
	}
}
