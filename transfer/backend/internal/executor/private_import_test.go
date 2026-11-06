package executor

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"path"
	"strings"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/common/contentio"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	shapefileformat "github.com/addp/common/format/plugins/shapefile"
	"github.com/google/uuid"
	"github.com/jonas-p/go-shp"
)

type importBytes struct {
	contentio.Reader
	files map[string]string
}

func (r importBytes) Open(_ context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	data, ok := r.files[ref.Path]
	if !ok {
		return nil, contentio.ErrContentNotFound
	}
	return io.NopCloser(strings.NewReader(data)), nil
}

func TestPrivateImportProofRequiresEveryActualComponent(t *testing.T) {
	main := format.NewRelatedRef(contentio.NewRef("private/shape.shp", "main"), true, true)
	side := format.NewRelatedRef(contentio.NewRef("private/shape.cpg", "encoding"), false, false)
	extra := format.NewRelatedRef(contentio.NewRef("private/shape.prj", "projection"), false, false)
	refs := []format.RelatedRef{main, side, extra}
	source := &client.ManagerImportSource{UploadID: uuid.NewString(), TaskID: 9, UploaderID: 31, Files: map[string]string{
		"shape.shp": fmt.Sprintf("%x", sha256.Sum256([]byte("shape"))),
		"shape.cpg": fmt.Sprintf("%x", sha256.Sum256([]byte("UTF-8"))),
	}}
	for _, tc := range []struct {
		name      string
		cpg       string
		readSide  bool
		reread    bool
		wantProof bool
	}{
		{"complete", "UTF-8", true, false, true},
		{"substituted sidecar", "ASCII", true, false, false},
		{"correct reread cannot erase a mismatch", "ASCII", true, true, false},
		{"unread sidecar", "UTF-8", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := importBytes{files: map[string]string{main.Ref.Path: "shape", side.Ref.Path: tc.cpg, extra.Ref.Path: "foreign"}}
			reader, selected, err := newVerifiedImportReader(raw, refs, source)
			if err != nil || len(selected) != 2 {
				t.Fatal(selected, err)
			}
			if _, err := reader.Open(t.Context(), extra.Ref); err == nil {
				t.Fatal("an unuploaded sidecar was readable")
			}
			consume := func(ref contentio.Ref) {
				t.Helper()
				stream, err := reader.Open(t.Context(), ref)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := io.Copy(io.Discard, stream); err != nil {
					t.Fatal(err)
				}
				if err := stream.Close(); err != nil {
					t.Fatal(err)
				}
			}
			for _, ref := range selected {
				if ref.Ref != side.Ref || tc.readSide {
					consume(ref.Ref)
				}
			}
			if tc.reread {
				raw.files[side.Ref.Path] = "UTF-8"
				consume(side.Ref)
			}
			proof, err := reader.proof()
			if (err == nil) != tc.wantProof || (proof != nil) != tc.wantProof {
				t.Fatal(proof, err)
			}
		})
	}
}

func TestPrivateImportVerifiesActualShapefileReadSession(t *testing.T) {
	sourcePath := engineplugin.ObjectItemPath(0, "manager", "tenant_7/import/test/points.shp")
	source := shapefileContent(t, sourcePath.StringPath(), "points", shp.POINT, &shp.Point{X: 10, Y: 20})
	source.files[path.Join(path.Dir(sourcePath.StringPath()), "points.cpg")] = []byte("UTF-8")
	manifest := make(map[string]string, len(source.files))
	for name, content := range source.files {
		manifest[path.Base(name)] = fmt.Sprintf("%x", sha256.Sum256(content))
	}
	upload := &client.ManagerImportSource{UploadID: uuid.NewString(), TaskID: 42, UploaderID: 31, Files: manifest}
	plugin := shapefileformat.NewPlugin(nil)
	executor := TableTransferExecutor{SourceContentReader: source, SourceMultiReadProvider: plugin, PrivateImportSource: upload}
	plan := TableSourcePlan{Kind: TableEndpointEncoded, Format: format.FormatShapefile, Path: sourcePath}
	batchSource, err := executor.openSource(plan)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := batchSource.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(t.Context())
	actual := reader.(*multiTableBatchReader).privateImport
	if actual == nil || actual.UploadID != upload.UploadID {
		t.Fatal("Shapefile parser did not produce private upload evidence")
	}
	batch, err := reader.ReadBatch(t.Context(), 10)
	if err != nil || len(batch.Rows) != 1 {
		t.Fatal(batch, err)
	}
}
