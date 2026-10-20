package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"github.com/addp/manager/internal/repository"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/engine/workflowaccess"
	commonExecution "github.com/addp/common/execution"
	_ "github.com/addp/common/format/plugins/threeds"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
)

type stagingModelStore struct {
	sgmObjectStore
	objects     map[string]string
	puts        int
	putErr      error
	cancelOnPut context.CancelFunc
}

func (s *stagingModelStore) PutObject(ctx context.Context, bucket, name string, r io.Reader, size int64, _ minio.PutObjectOptions) (minio.UploadInfo, error) {
	s.puts++
	if size < 0 {
		return minio.UploadInfo{}, errors.New("input size must be known")
	}
	data, err := io.ReadAll(io.LimitReader(r, size))
	if s.objects == nil {
		s.objects = map[string]string{}
	}
	s.objects[bucket+"/"+name] = string(data)
	if s.cancelOnPut != nil {
		s.cancelOnPut()
	}
	return minio.UploadInfo{Size: int64(len(data))}, errors.Join(err, s.putErr)
}

func (s *stagingModelStore) RemoveObject(ctx context.Context, bucket, name string, options minio.RemoveObjectOptions) error {
	if err := s.sgmObjectStore.RemoveObject(ctx, bucket, name, options); err != nil {
		return err
	}
	delete(s.objects, bucket+"/"+name)
	return nil
}

func stagingModelExecutor(store *stagingModelStore) *ManagerModel3DGLBExecutor {
	return &ManagerModel3DGLBExecutor{objectStore: store, defaultBucket: "manager", minioEndpoint: "127.0.0.1:19000", minioAccessKey: "infra-test-key", minioSecretKey: "infra-test-secret"}
}

func stagingModelFile(name, text string) model3DStagingFile {
	return model3DStagingFile{RelativePath: name, SizeBytes: int64(len(text)), Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(text)), nil }}
}

func TestModel3DStagingCompositionAndAttemptIsolation(t *testing.T) {
	store := &stagingModelStore{}
	executor := stagingModelExecutor(store)
	lease := commonExecution.Lease{TenantID: 7, ExecutionID: uuid.NewString(), Attempt: 1}
	var prefixes []string
	for _, attempt := range []int{1, 1, 2} {
		lease.Attempt = attempt
		files := []model3DStagingFile{stagingModelFile("模型.3DS", "model"), stagingModelFile("textures/TEST.PNG", "texture")}
		err := executor.withStagedModel3DSource(context.Background(), lease, "3ds", workflowaccess.KindDirectory, "模型.3DS", files, &recordingModel3DStagingOwner{},
			func(_ context.Context, source workflowaccess.Source) error {
				if source.Access.Method != workflowaccess.MethodObjectStore || source.Entrypoint != "模型.3DS" || source.Access.Path != "" {
					t.Fatalf("not an object store source: %+v", source)
				}
				prefix := source.Access.Prefix
				if !strings.HasPrefix(prefix, "temp/model3d-input/tenant_7/"+lease.ExecutionID+"/") || !strings.Contains(prefix, "/attempt_") {
					t.Fatal("temporary input is not execution scoped")
				}
				if len(store.objects) != 2 || store.objects["manager/"+prefix+"/模型.3DS"] != "model" || store.objects["manager/"+prefix+"/textures/TEST.PNG"] != "texture" {
					t.Fatal("model dependencies or relative paths changed")
				}
				prefixes = append(prefixes, prefix)
				return nil
			})
		if err != nil || len(store.objects) != 0 {
			t.Fatalf("err=%v residual objects=%d", err, len(store.objects))
		}
	}
	if prefixes[0] == prefixes[1] || prefixes[0] == prefixes[2] || prefixes[1] == prefixes[2] {
		t.Fatal("overlapping preparations or attempts reused an input scope")
	}
}

func TestModel3DStagingFailureAndCancellationCleanup(t *testing.T) {
	for _, scenario := range []string{"conversion", "upload", "short", "long", "open-second", "close", "cancel", "cleanup", "lifecycle", "versioning"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &stagingModelStore{}
			files := []model3DStagingFile{stagingModelFile("wall.ifc", "model")}
			failure := errors.New("controlled failure")
			switch scenario {
			case "upload":
				store.putErr = failure
			case "short":
				files[0].SizeBytes++
			case "long":
				files[0].SizeBytes--
			case "open-second":
				files = append(files, model3DStagingFile{RelativePath: "texture.png", SizeBytes: 3, Open: func(context.Context) (io.ReadCloser, error) { return nil, failure }})
			case "close":
				files[0].Open = func(context.Context) (io.ReadCloser, error) {
					return stagingModelCloseError{Reader: strings.NewReader("model"), err: failure}, nil
				}
			case "cancel":
				store.cancelOnPut = cancel
			case "cleanup":
				store.cleanupErr = failure
			case "lifecycle":
				store.expirationErr = failure
			case "versioning":
				store.versioningStatus = "Enabled"
			}
			consumed := false
			kind := workflowaccess.KindFile
			if scenario == "open-second" {
				kind = workflowaccess.KindDirectory
			}
			err := stagingModelExecutor(store).withStagedModel3DSource(ctx, commonExecution.Lease{TenantID: 7, ExecutionID: uuid.NewString(), Attempt: 1}, "ifc", kind, "wall.ifc", files, &recordingModel3DStagingOwner{},
				func(_ context.Context, source workflowaccess.Source) error {
					consumed = true
					if source.Access.Object == "" || source.Access.Prefix != "" {
						t.Fatal("IFC must use one exact object")
					}
					if scenario == "conversion" {
						return failure
					}
					return nil
				})
			if err == nil {
				t.Fatal("failure was reported as success")
			}
			if scenario != "conversion" && scenario != "cleanup" && consumed {
				t.Fatal("unprepared input reached the converter")
			}
			if scenario == "cleanup" {
				if len(store.objects) != 1 || !errors.Is(err, failure) {
					t.Fatal("cleanup failure lost its evidence")
				}
			} else if len(store.objects) != 0 {
				t.Fatal("failed input left temporary objects")
			}
			if store.cleanupCancelled {
				t.Fatal("cleanup used a cancelled execution context")
			}
			if (scenario == "lifecycle" || scenario == "versioning") && store.puts != 0 {
				t.Fatal("source was read before expiration policy preflight")
			}
			if scenario == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation cause was lost")
			}
		})
	}
}

func TestModel3DStagingFreezesCompositionBeforeOpening(t *testing.T) {
	store := &stagingModelStore{}
	files := []model3DStagingFile{stagingModelFile("model.obj", "model"), stagingModelFile("model.mtl", "material")}
	files[0].Open = func(context.Context) (io.ReadCloser, error) {
		files[1].RelativePath = "../unrelated"
		return io.NopCloser(strings.NewReader("model")), nil
	}
	err := stagingModelExecutor(store).withStagedModel3DSource(context.Background(), commonExecution.Lease{TenantID: 7, ExecutionID: uuid.NewString(), Attempt: 1}, "obj", workflowaccess.KindDirectory, "model.obj", files, &recordingModel3DStagingOwner{},
		func(_ context.Context, source workflowaccess.Source) error {
			if store.objects["manager/"+source.Access.Prefix+"/model.mtl"] != "material" {
				t.Fatal("validated input composition was mutated")
			}
			return nil
		})
	if err != nil || len(store.objects) != 0 {
		t.Fatalf("err=%v residual=%d", err, len(store.objects))
	}
}

// T1 adapter check against an in-process S3 HTTP fixture; not a real MinIO T2.
func TestModel3DStagingWithMinioClient(t *testing.T) {
	var reads, writes, removes int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Query().Has("versioning"):
			_, _ = io.WriteString(w, `<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"/>`)
		case r.Method == http.MethodGet && r.URL.Query().Has("lifecycle"):
			_ = xml.NewEncoder(w).Encode(lifecycle.Configuration{Rules: []lifecycle.Rule{{Status: "Enabled", RuleFilter: lifecycle.Filter{Prefix: "temp/"}, Expiration: lifecycle.Expiration{Days: 7}}}})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/manager/temp/model3d-input/tenant_7/"):
			writes++
			if _, err := io.Copy(io.Discard, r.Body); err != nil {
				t.Error(err)
			}
			w.Header().Set("ETag", `"test-etag"`)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/manager/temp/model3d-input/tenant_7/"):
			removes++
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected storage operation: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	client, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{Creds: credentials.NewStaticV4("test-key", "test-secret", ""), Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	executor := &ManagerModel3DGLBExecutor{objectStore: client, defaultBucket: "manager", minioEndpoint: strings.TrimPrefix(server.URL, "http://"), minioAccessKey: "test-key", minioSecretKey: "test-secret"}
	file := model3DStagingFile{RelativePath: "wall.ifc", SizeBytes: 5, Open: func(context.Context) (io.ReadCloser, error) {
		reads++
		return io.NopCloser(strings.NewReader("model")), nil
	}}
	err = executor.withStagedModel3DSource(context.Background(), commonExecution.Lease{TenantID: 7, ExecutionID: uuid.NewString(), Attempt: 1}, "ifc", workflowaccess.KindFile, "wall.ifc", []model3DStagingFile{file}, &recordingModel3DStagingOwner{},
		func(context.Context, workflowaccess.Source) error { return nil })
	if err != nil || reads != 1 || writes != 1 || removes != 1 {
		t.Fatalf("err=%v reads=%d writes=%d removes=%d", err, reads, writes, removes)
	}
}

type stagingModelCloseError struct {
	io.Reader
	err error
}

func (r stagingModelCloseError) Close() error { return r.err }

func TestModel3DStagingRejectsInvalidCompositionBeforeSourceRead(t *testing.T) {
	for _, scenario := range []string{"traversal", "absolute", "backslash", "duplicate", "ancestor", "missing", "single-sidecar", "size", "execution", "attempt"} {
		t.Run(scenario, func(t *testing.T) {
			store := &stagingModelStore{}
			files := []model3DStagingFile{stagingModelFile("model.obj", "model")}
			lease := commonExecution.Lease{TenantID: 7, ExecutionID: uuid.NewString(), Attempt: 1}
			kind := workflowaccess.KindDirectory
			switch scenario {
			case "traversal":
				files = append(files, stagingModelFile("../secret", "x"))
			case "absolute":
				files = append(files, stagingModelFile("/secret", "x"))
			case "backslash":
				files = append(files, stagingModelFile("..\\secret", "x"))
			case "duplicate":
				files = append(files, files[0])
			case "ancestor":
				files = append(files, stagingModelFile("model.obj/secret", "x"))
			case "missing":
				files[0].RelativePath = "other.obj"
			case "single-sidecar":
				kind = workflowaccess.KindFile
				files = append(files, stagingModelFile("model.mtl", "x"))
			case "size":
				files[0].SizeBytes = -1
			case "execution":
				lease.ExecutionID = "../other-execution"
			case "attempt":
				lease.Attempt = 0
			}
			reads := 0
			for i := range files {
				files[i].Open = func(context.Context) (io.ReadCloser, error) {
					reads++
					return io.NopCloser(strings.NewReader("model")), nil
				}
			}
			err := stagingModelExecutor(store).withStagedModel3DSource(context.Background(), lease, "obj", kind, "model.obj", files, &recordingModel3DStagingOwner{},
				func(context.Context, workflowaccess.Source) error {
					t.Fatal("invalid composition reached the converter")
					return nil
				})
			if err == nil || reads != 0 || store.puts != 0 || len(store.removed) != 0 {
				t.Fatalf("err=%v reads=%d puts=%d", err, reads, store.puts)
			}
		})
	}
}

func threeDSStagingInput(refs ...string) []byte {
	chunk := func(id uint16, body []byte) []byte {
		data := make([]byte, 6+len(body))
		binary.LittleEndian.PutUint16(data, id)
		binary.LittleEndian.PutUint32(data[2:], uint32(len(data)))
		copy(data[6:], body)
		return data
	}
	var maps []byte
	for _, ref := range refs {
		maps = append(maps, chunk(0xa200, chunk(0xa300, append([]byte(ref), 0)))...)
	}
	return chunk(0x4d4d, chunk(0x3d3d, chunk(0xafff, maps)))
}

func Test3DSStagingFreezesParsedBytesAndExactDeclarations(t *testing.T) {
	for _, refs := range [][]string{{}, {"TEST.PNG"}, {"贴图%20.PNG", "textures/detail.png"}} {
		input := threeDSStagingInput(refs...)
		expected := append([]string{}, refs...)
		if len(expected) > 1 {
			expected[0], expected[1] = expected[1], expected[0]
		}
		buffered, err := buffer3DSStagingEntrypoint(context.Background(), bytes.NewReader(input), expected)
		if err != nil || !bytes.Equal(input, buffered) {
			t.Fatalf("parsed main-file bytes changed: %v", err)
		}
		input[len(input)-1] = 42
		if bytes.Equal(input, buffered) {
			t.Fatal("buffer aliases mutable caller input")
		}
	}
}

func Test3DSStagingRejectsChangedReferencesBeforeAnyStaging(t *testing.T) {
	for _, changed := range [][]string{{"EVIL.PNG"}, {"test.png"}, {"TEST.PNG", "extra.png"}, {}} {
		buffered, err := buffer3DSStagingEntrypoint(context.Background(), bytes.NewReader(threeDSStagingInput(changed...)), []string{"TEST.PNG"})
		if err == nil || buffered != nil {
			t.Fatalf("changed references accepted: %v", changed)
		}
	}
	// Same byte length defeats size-only checks; source mtime is intentionally
	// not an input to this content-based declaration check.
	if len(threeDSStagingInput("TEST.PNG")) != len(threeDSStagingInput("EVIL.PNG")) {
		t.Fatal("reference mutation fixture changed byte length")
	}
	buffered, err := buffer3DSStagingEntrypoint(context.Background(), bytes.NewReader(threeDSStagingInput("图%20.PNG")), []string{"图 .PNG"})
	if err == nil || buffered != nil {
		t.Fatal("URI-decoded texture reference was accepted")
	}
}

func Test3DSStagingRejectsIncompleteOrFailedMainRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		ctx    context.Context
		reader io.Reader
		refs   []string
	}{
		{context.Background(), bytes.NewReader(threeDSStagingInput()), nil},
		{context.Background(), nil, []string{}},
		{context.Background(), strings.NewReader("not a model"), []string{}},
		{context.Background(), bytes.NewReader(threeDSStagingInput("TEST.PNG")), []string{"TEST.PNG", "TEST.PNG"}},
		{context.Background(), bytes.NewReader(threeDSStagingInput()), []string{""}},
		{ctx, bytes.NewReader(threeDSStagingInput()), []string{}},
		{context.Background(), failed3DSStagingReader{}, []string{}},
	} {
		buffered, err := buffer3DSStagingEntrypoint(tc.ctx, tc.reader, tc.refs)
		if err == nil || buffered != nil {
			t.Fatal("incomplete main input accepted")
		}
	}
}

type failed3DSStagingReader struct{}

func (failed3DSStagingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// This records persistence calls, not source authorization decisions.
type recordingModel3DStagingOwner struct {
	prepared              []repository.Model3DGLBInputPreparation
	prepareErr, finishErr error
	settled, cleaned      bool
}

func (o *recordingModel3DStagingOwner) prepare(_ context.Context, p repository.Model3DGLBInputPreparation) error {
	o.prepared = append(o.prepared, p)
	return o.prepareErr
}
func (o *recordingModel3DStagingOwner) finish(_ context.Context, _ repository.Model3DGLBInputPreparation, settled, cleaned bool) error {
	o.settled, o.cleaned = settled, cleaned
	return o.finishErr
}

func TestModel3DStagingPersistsAllObjectsBeforeAnyUpload(t *testing.T) {
	for _, scenario := range []string{"success", "persist failure", "upload unknown", "delete failure", "ack failure", "no owner"} {
		t.Run(scenario, func(t *testing.T) {
			failure := errors.New("controlled ownership failure")
			owner := &recordingModel3DStagingOwner{}
			store := &stagingModelStore{}
			lease := commonExecution.Lease{TenantID: 7, ExecutionID: uuid.NewString(), Attempt: 2}
			files := []model3DStagingFile{stagingModelFile("Scene.3DS", "main"), stagingModelFile("textures/Color.PNG ", "image")}
			files[0].Open = func(context.Context) (io.ReadCloser, error) {
				if len(owner.prepared) != 1 || len(owner.prepared[0].Objects) != 2 || store.puts != 0 {
					t.Fatal("I/O preceded complete durable ownership")
				}
				if err := owner.prepared[0].Validate(7); err != nil {
					t.Fatal(err)
				}
				if !strings.HasSuffix(owner.prepared[0].Objects[1].Object, "/textures/Color.PNG ") {
					t.Fatal("raw object name changed")
				}
				return io.NopCloser(strings.NewReader("main")), nil
			}
			switch scenario {
			case "persist failure":
				owner.prepareErr = failure
			case "upload unknown":
				store.putErr = failure
			case "delete failure":
				store.cleanupErr = failure
			case "ack failure":
				owner.finishErr = failure
			}
			var ownership model3DStagingOwnership = owner
			if scenario == "no owner" {
				ownership = nil
			}
			err := stagingModelExecutor(store).withStagedModel3DSource(t.Context(), lease, "3ds", workflowaccess.KindDirectory, "Scene.3DS", files, ownership, func(context.Context, workflowaccess.Source) error { return nil })
			if (err != nil) != (scenario != "success") {
				t.Fatalf("error=%v", err)
			}
			if (scenario == "persist failure" || scenario == "no owner") && store.puts != 0 {
				t.Fatal("missing ownership allowed upload")
			}
			if scenario == "upload unknown" && owner.settled {
				t.Fatal("ambiguous PUT acknowledged as settled")
			}
			if scenario == "delete failure" && owner.cleaned {
				t.Fatal("failed DELETE acknowledged as cleaned")
			}
			if scenario == "success" && (!owner.settled || !owner.cleaned || len(store.objects) != 0) {
				t.Fatal("successful cleanup not acknowledged")
			}
		})
	}
}
