package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"path"

	"github.com/addp/common/client"
	"github.com/addp/common/contentio"
	"github.com/addp/common/execution"
	"github.com/addp/common/format"
)

var errPrivateImportManifest = errors.New("private import bytes do not match the upload manifest")

// Shapefile materializes its refs completely before returning its table reader.
// Only bytes delivered to that parser are hashed here.
type verifiedImportReader struct {
	contentio.Reader
	source   *client.ManagerImportSource
	expected map[string]string
	verified map[string]bool
	failed   bool
}

func newVerifiedImportReader(reader contentio.Reader, refs []format.RelatedRef, source *client.ManagerImportSource) (*verifiedImportReader, []format.RelatedRef, error) {
	expected := make(map[string]string, len(source.Files))
	selected := make([]format.RelatedRef, 0, len(source.Files))
	for _, ref := range refs {
		digest, present := source.Files[path.Base(ref.Ref.Path)]
		if !present {
			if ref.Required {
				return nil, nil, errPrivateImportManifest
			}
			continue
		}
		expected[ref.Ref.Path] = digest
		selected = append(selected, ref)
	}
	if len(expected) != len(source.Files) {
		return nil, nil, errPrivateImportManifest
	}
	return &verifiedImportReader{Reader: reader, source: source, expected: expected, verified: make(map[string]bool, len(expected))}, selected, nil
}

func (r *verifiedImportReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	expected, ok := r.expected[ref.Path]
	if !ok {
		return nil, errPrivateImportManifest
	}
	stream, err := r.Reader.Open(ctx, ref)
	if err != nil {
		r.failed = true
		return nil, err
	}
	return &importReadStream{ReadCloser: stream, digest: sha256.New(), record: func(digest string, complete bool) {
		if !complete || digest != expected {
			r.failed = true
			return
		}
		r.verified[ref.Path] = true
	}}, nil
}

type importReadStream struct {
	io.ReadCloser
	digest   hash.Hash
	complete bool
	record   func(string, bool)
}

func (s *importReadStream) Read(p []byte) (int, error) {
	n, err := s.ReadCloser.Read(p)
	_, _ = s.digest.Write(p[:n])
	s.complete = err == io.EOF
	return n, err
}

func (s *importReadStream) Close() error {
	err := s.ReadCloser.Close()
	s.record(hex.EncodeToString(s.digest.Sum(nil)), s.complete && err == nil)
	return err
}

func (r *verifiedImportReader) proof() (*execution.ManagerImportReadProof, error) {
	if r.failed {
		return nil, errPrivateImportManifest
	}
	for name := range r.expected {
		if !r.verified[name] {
			return nil, errPrivateImportManifest
		}
	}
	// encoding/json orders the manifest keys, so the summary is deterministic.
	manifest, err := json.Marshal(r.source.Files)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(manifest)
	proof := &execution.ManagerImportReadProof{
		UploadID: r.source.UploadID, TaskID: r.source.TaskID, UploaderID: r.source.UploaderID,
		ManifestSHA256: hex.EncodeToString(digest[:]),
	}
	if err := proof.Validate(); err != nil {
		return nil, err
	}
	return proof, nil
}
