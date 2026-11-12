package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/repository"
)

const fileInputRuntimeID = "5cb603c1-7544-42ca-a998-6a196a7e31c4"

// All sources/authority/Runtime here are explicit doubles. Real temp files and
// the Common HTTP client exercise the consumer, not NFS/IAM/GeoPython or T4.
func fileInputsFixture(t *testing.T, save bool) (*client.DevelopFileExecutionFacts, client.IssuedDevelopFileAuthorization, execution.Lease, client.DevelopFileSourceAccessObservation) {
	t.Helper()
	_, _, base := fileSourceFixture(t, time.Minute)
	target := (*execution.DevelopFileTargetIntent)(nil)
	tail := ""
	if save {
		target = &execution.DevelopFileTargetIntent{NodeID: "save", Target: plugin.FileItemPath(11, "outputs/result.gpkg"), Mode: "fail"}
		tail = `,{"id":"save","operator":"save","params":{"input_df":{"$ref":"buffer"},"mode":"fail","target_parent_locator":"addp://engine/11/path/outputs?type=directory","target_name":"result.gpkg"},"depends_on":["buffer"]}`
	}
	raw := json.RawMessage(`{"timeout":600,"number":9007199254740993,"content":{"workflow_definition":{"tasks":[{"id":"a","operator":"load","params":{"locator":"addp://engine/11/path/sources/A.gpkg?type=file","geom_column":"shape","source_resource":{"item_id":"not-authority"}},"depends_on":[]},{"id":"b","operator":"load","params":{"locator":"addp://engine/11/path/sources/B.gpkg?type=file"},"depends_on":[]},{"id":"buffer","operator":"buffer","params":{"input_gdf":{"$ref":"a"},"distance":9007199254740993},"depends_on":["a"]}` + tail + `]}}}`)
	intent, err := execution.NewDevelopFileIntent(raw, 1, 600, []execution.DevelopFileSourceIntent{{NodeID: "a", Target: plugin.FileItemPath(11, "sources/A.gpkg")}, {NodeID: "b", Target: plugin.FileItemPath(11, "sources/B.gpkg")}}, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	refs := []execution.DevelopFileSourceReference{}
	for i, source := range intent.Sources {
		refs = append(refs, execution.DevelopFileSourceReference{Target: source.Target, ItemFingerprint: strings.Repeat(string(rune('a'+i)), 64), Format: "geopackage", BoundaryHash: strings.Repeat(string(rune('c'+i)), 64), EngineVersion: "2"})
	}
	version := ""
	if save {
		version = "2"
	}
	scope, err := execution.NewDevelopBoundFileScope(*intent, refs, version)
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := scope.Digest()
	issued := client.IssuedDevelopFileAuthorization{Authorization: client.IssuedExecutionAuthorization{ID: "9", ExecutionID: fileSourceExecution, Audience: "develop", TenantID: "3", ActorPrincipalID: "31", TenantMembershipID: "41", IssuedAuthorizationVersion: "5", SourceType: "user", Accesses: []client.ExecutionEngineAccessScope{{EngineID: "11", Effects: []string{"read"}}}, ExpiresAt: time.Now().Add(time.Hour)}, Scope: *scope, ScopeDigest: digest}
	facts := &client.DevelopFileExecutionFacts{SchemaVersion: client.DevelopFileExecutionSchema, ExecutionID: fileSourceExecution, TenantID: "3", Actor: client.DevelopFileExecutionActor{PrincipalID: "31", TenantMembershipID: "41", AuthorizationVersion: "5"}, ConfigJSON: raw, Intent: *intent, AuthorizationID: "9", BoundScope: scope.Clone(), ScopeDigest: digest, AdmissionState: "ready"}
	if facts.Validate() != nil || issued.Validate() != nil {
		t.Fatal("bad fixture")
	}
	lease := execution.Lease{ExecutionID: fileSourceExecution, TenantID: 3, Attempt: 1, Token: fileSourceLease, Owner: "fixture-owner"}
	return facts, issued, lease, base
}

func fileInputsAuthority(base client.DevelopFileSourceAccessObservation) fileSourceAuthorityFunc {
	return func(_ context.Context, issued client.IssuedDevelopFileAuthorization, request client.DevelopFileSourceAccessRequest) (*client.DevelopFileSourceAccessObservation, error) {
		copy := base
		copy.AuthorizationID = issued.Authorization.ID
		copy.ExecutionID = request.ExecutionID
		copy.ScopeDigest = request.ScopeDigest
		copy.Attempt = request.Attempt
		copy.Source = request.Source
		return &copy, nil
	}
}
func fileInputsProvider(data map[string][]byte) *sourceWindowProvider {
	return &sourceWindowProvider{facts: func(_ context.Context, _ plugin.ConnectionInfo, p plugin.EngineCatalogPath) (*plugin.EngineCatalogFacts, error) {
		content, ok := data[p.StringPath()]
		if !ok {
			return nil, os.ErrNotExist
		}
		return sourceWindowFacts(p, int64(len(content))), nil
	}, open: func(_ context.Context, _ plugin.ConnectionInfo, p plugin.EngineCatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
		content, ok := data[p.StringPath()]
		if !ok {
			return nil, errors.New("unbound source")
		}
		set, _ := plugin.NewQueryReadSet(p)
		return &sourceWindowHandle{Reader: bytes.NewReader(content), set: set}, nil
	}}
}
func cleanupFileInputs(t *testing.T, b *fileWorkflowInputs) {
	t.Helper()
	if b == nil {
		return
	}
	// Wait only for our controlled handles, never a real production RPC.
	for _, s := range b.sources {
		_ = s.Close()
		waitSourceStopped(t, s)
	}
	result, err := b.Cleanup(t.Context())
	if err != nil || !result.LocalRemoved || !result.SourcesStopped {
		t.Error(result, err)
	}
}
func fileInputsData(t *testing.T) map[string][]byte {
	t.Helper()
	name := filepath.Join(t.TempDir(), "fixture.gpkg")
	db, err := sql.Open("sqlite3", name)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
PRAGMA application_id=1196444487;
PRAGMA user_version=10300;
CREATE TABLE gpkg_spatial_ref_sys(srs_name TEXT, srs_id INTEGER PRIMARY KEY, organization TEXT, organization_coordsys_id INTEGER, definition TEXT);
INSERT INTO gpkg_spatial_ref_sys VALUES('WGS84',4326,'EPSG',4326,'fixture');
CREATE TABLE gpkg_contents(table_name TEXT PRIMARY KEY,data_type TEXT NOT NULL,srs_id INTEGER);
INSERT INTO gpkg_contents VALUES('features','features',4326);
CREATE TABLE gpkg_geometry_columns(table_name TEXT PRIMARY KEY,column_name TEXT,geometry_type_name TEXT,srs_id INTEGER,z INTEGER,m INTEGER);
INSERT INTO gpkg_geometry_columns VALUES('features','shape','GEOMETRY',4326,0,0);
CREATE TABLE features(fid INTEGER PRIMARY KEY,shape BLOB, attribute BLOB);
INSERT INTO features VALUES(9007199254740993,NULL,zeroblob(140000));
`)
	if err != nil {
		t.Fatal(err)
	}
	a, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE features SET attribute = x'42'"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return map[string][]byte{"sources/A.gpkg": a, "sources/B.gpkg": b}
}

func TestFileWorkflowInputsSpoolExactBytesAndRawDefinition(t *testing.T) {
	for _, save := range []bool{false, true} {
		t.Run(strconv.FormatBool(save), func(t *testing.T) {
			facts, issued, lease, base := fileInputsFixture(t, save)
			data := fileInputsData(t)
			original := bytes.Clone(facts.ConfigJSON)
			b, err := prepareFileWorkflowInputs(t.Context(), "fixture-instance", fileInputsAuthority(base), fileInputsProvider(data), facts, issued, lease, recordFileInputsDirectory)
			if b != nil {
				defer cleanupFileInputs(t, b)
			}
			if err != nil {
				t.Fatal(err)
			}
			if info, err := os.Stat(b.directory); err != nil || info.Mode().Perm() != 0700 {
				t.Fatal(info, err)
			}
			for i, input := range b.manifest.Inputs {
				info, err := os.Stat(b.inputPath(i))
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal(info, err)
				}
				actual, err := os.ReadFile(b.inputPath(i))
				want := data[facts.Intent.Sources[i].Target.StringPath()]
				sum := sha256.Sum256(want)
				if err != nil || !bytes.Equal(actual, want) || input.Size != int64(len(want)) || input.SHA256 != hex.EncodeToString(sum[:]) || input.NodeID != facts.Intent.Sources[i].NodeID {
					t.Fatal("spool changed source", i, err)
				}
				if filepath.Base(b.inputPath(i)) != "input-"+strconv.Itoa(i)+".gpkg" {
					t.Fatal("caller identity became filename")
				}
			}
			if !bytes.Equal(facts.ConfigJSON, original) || !strings.Contains(b.manifest.WorkflowJSON, `9007199254740993`) {
				t.Fatal("numeric lexeme lost through float projection", b.manifest.WorkflowJSON)
			}
			for _, secret := range []string{"addp://", "sources/A.gpkg", "outputs/result.gpkg", "source_resource", "test-only", b.directory, "fixture-instance"} {
				if strings.Contains(b.manifest.WorkflowJSON, secret) {
					t.Fatal("business identity/connection entered Runtime", secret)
				}
			}
			if !b.manifest.Deadline.Equal(base.BusinessDeadline) || len(b.manifest.Outputs) != map[bool]int{false: 0, true: 1}[save] {
				t.Fatal(b.manifest)
			}
			facts.Intent.Sources[0].Target.EngineID = 99
			facts.ConfigJSON[0] = 'x'
			issued.Scope.Sources[0].Target.EngineID = 99
			if err := b.live(); err != nil {
				t.Fatal("caller mutation changed frozen inputs", err)
			}
		})
	}
}

func TestFileWorkflowInputsRejectOwnerMismatchBeforeOpen(t *testing.T) {
	for _, fault := range []string{"actor", "membership", "authorization-version", "authorization-id", "scope", "not-ready", "lease-execution", "lease-tenant", "lease-attempt", "lease-token", "lease-owner"} {
		t.Run(fault, func(t *testing.T) {
			facts, issued, lease, base := fileInputsFixture(t, false)
			switch fault {
			case "actor":
				facts.Actor.PrincipalID = "32"
			case "membership":
				facts.Actor.TenantMembershipID = "42"
			case "authorization-version":
				facts.Actor.AuthorizationVersion = "6"
			case "authorization-id":
				facts.AuthorizationID = "10"
			case "scope":
				facts.ScopeDigest = strings.Repeat("d", 64)
			case "not-ready":
				facts.AdmissionState = "pending"
			case "lease-execution":
				lease.ExecutionID = fileSourceLease
			case "lease-tenant":
				lease.TenantID = 4
			case "lease-attempt":
				lease.Attempt = 2
			case "lease-token":
				lease.Token = "invalid"
			case "lease-owner":
				lease.Owner = ""
			}
			var calls atomic.Int32
			auth := fileInputsAuthority(base)
			count := fileSourceAuthorityFunc(func(c context.Context, i client.IssuedDevelopFileAuthorization, r client.DevelopFileSourceAccessRequest) (*client.DevelopFileSourceAccessObservation, error) {
				calls.Add(1)
				return auth(c, i, r)
			})
			b, err := prepareFileWorkflowInputs(t.Context(), "fixture-instance", count, fileInputsProvider(fileInputsData(t)), facts, issued, lease, recordFileInputsDirectory)
			if b != nil {
				cleanupFileInputs(t, b)
			}
			if err == nil || calls.Load() != 0 {
				t.Fatal("unbound owner reached source", err, calls.Load())
			}
		})
	}
}

// These remain valid frozen DTOs while the raw DAG is wrong, proving that the
// consumer checks full source coverage rather than trusting an Intent alone.
func TestFileWorkflowInputsRejectRawDAGMismatchBeforeOpen(t *testing.T) {
	for _, fault := range []string{"swapped-locator", "extra-load", "missing-load", "duplicate-node", "unbound-save", "hidden-connection", "hidden-path", "hidden-algorithm-connection", "cycle", "wrong-target", "replace-mode"} {
		t.Run(fault, func(t *testing.T) {
			save := fault == "wrong-target" || fault == "replace-mode"
			facts, _, _, _ := fileInputsFixture(t, save)
			raw := string(facts.ConfigJSON)
			switch fault {
			case "swapped-locator":
				raw = strings.Replace(raw, "sources/A.gpkg", "sources/C.gpkg", 1)
			case "extra-load":
				raw = strings.Replace(raw, `{"id":"buffer"`, `{"id":"x","operator":"load","params":{"locator":"addp://engine/11/path/sources/C.gpkg?type=file"},"depends_on":[]},{"id":"buffer"`, 1)
			case "missing-load":
				raw = strings.Replace(raw, `"id":"b","operator":"load"`, `"id":"b","operator":"buffer"`, 1)
			case "duplicate-node":
				raw = strings.Replace(raw, `"id":"b"`, `"id":"a"`, 1)
			case "unbound-save":
				raw = strings.Replace(raw, `"id":"buffer","operator":"buffer"`, `"id":"buffer","operator":"save"`, 1)
			case "hidden-connection":
				raw = strings.Replace(raw, `"geom_column":"shape"`, `"geom_column":"shape","connection_info":{"server":"hidden"}`, 1)
			case "hidden-path":
				raw = strings.Replace(raw, `"geom_column":"shape"`, `"geom_column":"shape","path":"hidden.gpkg"`, 1)
			case "hidden-algorithm-connection":
				raw = strings.Replace(raw, `"distance":9007199254740993`, `"distance":9007199254740993,"connection_info":{"server":"hidden"}`, 1)
			case "cycle":
				raw = strings.Replace(raw, `"depends_on":["a"]`, `"depends_on":["buffer"]`, 1)
			case "wrong-target":
				raw = strings.Replace(raw, `"target_name":"result.gpkg"`, `"target_name":"neighbor.gpkg"`, 1)
			case "replace-mode":
				raw = strings.Replace(raw, `"mode":"fail"`, `"mode":"replace"`, 1)
			}
			intent, err := execution.NewDevelopFileIntent(json.RawMessage(raw), 1, 600, facts.Intent.Sources, facts.Intent.Target, nil)
			if err != nil {
				t.Fatal(err)
			}
			facts.ConfigJSON = json.RawMessage(raw)
			facts.Intent = *intent
			scope, err := execution.NewDevelopBoundFileScope(*intent, facts.BoundScope.Sources, map[bool]string{false: "", true: "2"}[save])
			if err != nil {
				t.Fatal(err)
			}
			facts.BoundScope = scope
			facts.ScopeDigest, _ = scope.Digest()
			if facts.Validate() != nil {
				t.Fatal("fixture rejected before coverage check")
			}
			if text, err := fileWorkflowRuntimeDefinition(facts); text != "" || err == nil {
				t.Fatal("invalid DAG projected", text, err)
			}
		})
	}
}

type fileInputsHTTP struct {
	server        *httptest.Server
	connection    plugin.ConnectionInfo
	calls         atomic.Int32
	creates       atomic.Int32
	uploads       atomic.Int32
	dispatches    atomic.Int32
	cleanup       atomic.Int32
	registered    atomic.Bool
	mu            sync.Mutex
	manifest      plugin.WorkflowPrivateFileManifest
	data          map[string][]byte
	fault         string
	uploadStarted chan struct{}
	uploadRelease chan struct{}
}

func newFileInputsHTTP(t *testing.T, fault string) *fileInputsHTTP {
	t.Helper()
	h := &fileInputsHTTP{data: fileInputsData(t), fault: fault}
	if fault == "upload-blocked" {
		h.uploadStarted = make(chan struct{})
		h.uploadRelease = make(chan struct{})
	}
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer addp_at_runtime_current" {
			t.Error("missing current machine credential")
		}
		switch {
		case r.Method == "PUT" && r.URL.Path == "/api/private-files/"+fileInputRuntimeID:
			h.creates.Add(1)
			if !h.registered.Load() {
				t.Error("created before durable registration acknowledged")
			}
			var m plugin.WorkflowPrivateFileManifest
			_ = json.NewDecoder(r.Body).Decode(&m)
			h.mu.Lock()
			h.manifest = m
			h.mu.Unlock()
			if h.fault == "create-unknown" {
				w.WriteHeader(503)
				return
			}
			states := make([]string, len(m.Inputs))
			for i := range states {
				states[i] = "missing"
			}
			pending := false
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(plugin.WorkflowPrivateFileStatus{RuntimeExecutionID: fileInputRuntimeID, ExecutionID: m.ExecutionID, Attempt: m.Attempt, ScopeDigest: m.ScopeDigest, Inputs: states, Outputs: []plugin.WorkflowPrivateFile{}, CleanupPending: &pending, Execution: json.RawMessage(`{"status":"pending"}`)})
		case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/api/private-files/"+fileInputRuntimeID+"/inputs/"):
			index, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/api/private-files/"+fileInputRuntimeID+"/inputs/"))
			h.uploads.Add(1)
			if h.fault == "upload-blocked" {
				close(h.uploadStarted)
				<-h.uploadRelease
				w.WriteHeader(409)
				return
			}
			body, err := io.ReadAll(r.Body)
			h.mu.Lock()
			input := h.manifest.Inputs[index]
			h.mu.Unlock()
			sum := sha256.Sum256(body)
			if err != nil || int64(len(body)) != input.Size || hex.EncodeToString(sum[:]) != input.SHA256 || !bytes.Equal(body, h.data[map[int]string{0: "sources/A.gpkg", 1: "sources/B.gpkg"}[index]]) {
				w.WriteHeader(409)
				return
			}
			if h.fault == "upload-unknown" {
				w.WriteHeader(503)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"runtime_execution_id": fileInputRuntimeID, "input_index": index, "state": "ready"})
		case r.Method == "POST" && r.URL.Path == "/api/workflow":
			h.dispatches.Add(1)
			raw, _ := io.ReadAll(r.Body)
			if bytes.Contains(raw, []byte("connection_info")) || bytes.Contains(raw, []byte("workflow_definition")) || bytes.Contains(raw, []byte("addp://")) {
				t.Error("dispatch bypassed private reference")
			}
			if h.fault == "execute-unknown" {
				w.WriteHeader(503)
				return
			}
			w.WriteHeader(202)
			_ = json.NewEncoder(w).Encode(map[string]string{"execution_id": fileInputRuntimeID, "status": "running"})
		case r.Method == "DELETE" && r.URL.Path == "/api/private-files/"+fileInputRuntimeID:
			h.cleanup.Add(1)
			if h.fault == "cleanup-unknown" {
				w.WriteHeader(404)
				return
			}
			if h.fault == "cleanup-pending" {
				w.WriteHeader(202)
				_, _ = w.Write([]byte(`{"cleanup":"pending"}`))
				return
			}
			_, _ = w.Write([]byte(`{"cleanup":"passed"}`))
		default:
			t.Error("unexpected private transport path", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(h.server.Close)
	u, _ := url.Parse(h.server.URL)
	port, _ := strconv.Atoi(u.Port())
	h.connection = plugin.ConnectionInfo{"host": u.Hostname(), "port": port, "protocol": "http"}
	return h
}
func (h *fileInputsHTTP) record(ctx context.Context, record repository.PrivateFileRuntimeRecord) error {
	id, m := record.RuntimeExecutionID, record.Manifest
	if ctx.Err() != nil || id != fileInputRuntimeID || m.ExecutionID != fileSourceExecution {
		return errors.New("invalid registration")
	}
	h.registered.Store(true)
	return nil
}
func runtimeFileInputsCredential(context.Context) (string, error) {
	return "addp_at_runtime_current", nil
}

func TestFileWorkflowInputsActualSDKUploadsAndCanonicalDispatchOnce(t *testing.T) {
	facts, issued, lease, base := fileInputsFixture(t, true)
	b, err := prepareFileWorkflowInputs(t.Context(), "fixture-instance", fileInputsAuthority(base), fileInputsProvider(fileInputsData(t)), facts, issued, lease, recordFileInputsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	h := newFileInputsHTTP(t, "")
	if err := b.Upload(t.Context(), h.connection, fileInputRuntimeID, runtimeFileInputsCredential, h.record); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	manifest := h.manifest
	h.mu.Unlock()
	received, _ := json.Marshal(manifest)
	expected, _ := json.Marshal(b.manifest)
	if !bytes.Equal(received, expected) || !manifest.Deadline.Equal(base.BusinessDeadline) {
		t.Fatal("registered/transferred manifest differed")
	}
	if _, err := b.Execute(t.Context(), "en"); err != nil {
		t.Fatal(err)
	}
	if err := b.Upload(t.Context(), h.connection, fileSourceLease, runtimeFileInputsCredential, h.record); err == nil {
		t.Fatal("second identity/create allowed")
	}
	if _, err := b.Execute(t.Context(), "en"); err == nil {
		t.Fatal("computation replayed")
	}
	result, err := b.Cleanup(t.Context())
	if err != nil || !result.LocalRemoved || !result.SourcesStopped || result.RuntimeCleanup != "passed" {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(b.directory); !os.IsNotExist(err) {
		t.Fatal("private directory remains")
	}
	if h.creates.Load() != 1 || h.uploads.Load() != 2 || h.dispatches.Load() != 1 || h.cleanup.Load() != 1 {
		t.Fatal("unexpected mutation counts", h.creates.Load(), h.uploads.Load(), h.dispatches.Load(), h.cleanup.Load())
	}
}

func TestFileWorkflowInputsUnknownOutcomesNeverReplay(t *testing.T) {
	for _, fault := range []string{"register-unknown", "create-unknown", "upload-unknown", "execute-unknown"} {
		t.Run(fault, func(t *testing.T) {
			facts, issued, lease, base := fileInputsFixture(t, false)
			b, err := prepareFileWorkflowInputs(t.Context(), "fixture-instance", fileInputsAuthority(base), fileInputsProvider(fileInputsData(t)), facts, issued, lease, recordFileInputsDirectory)
			if err != nil {
				t.Fatal(err)
			}
			h := newFileInputsHTTP(t, fault)
			record := fileWorkflowRuntimeRecorder(h.record)
			if fault == "register-unknown" {
				record = func(context.Context, repository.PrivateFileRuntimeRecord) error {
					return errors.New("lost registration acknowledgement")
				}
			}
			err = b.Upload(t.Context(), h.connection, fileInputRuntimeID, runtimeFileInputsCredential, record)
			if fault == "execute-unknown" {
				if err != nil {
					t.Fatal(err)
				}
				_, err = b.Execute(t.Context(), "zh")
				if _, second := b.Execute(t.Context(), "zh"); second == nil {
					t.Fatal("execute replayed")
				}
			}
			if err == nil {
				t.Fatal("unknown outcome claimed successful")
			}
			before := h.calls.Load()
			if err := b.Upload(t.Context(), h.connection, fileSourceLease, runtimeFileInputsCredential, h.record); err == nil || h.calls.Load() != before {
				t.Fatal("unknown mutation replaced/replayed")
			}
			result, err := b.Cleanup(t.Context())
			if err != nil || !result.LocalRemoved {
				t.Fatal(result, err)
			}
			if fault == "register-unknown" && h.calls.Load() != 0 {
				t.Fatal("Runtime touched before confirmed journal")
			}
		})
	}
}

func TestFileWorkflowInputsTransferIntegrityCannotDispatch(t *testing.T) {
	for _, fault := range []string{"size", "same-size-content"} {
		t.Run(fault, func(t *testing.T) {
			facts, issued, lease, base := fileInputsFixture(t, false)
			b, err := prepareFileWorkflowInputs(t.Context(), "fixture-instance", fileInputsAuthority(base), fileInputsProvider(fileInputsData(t)), facts, issued, lease, recordFileInputsDirectory)
			if err != nil {
				t.Fatal(err)
			}
			p := b.inputPath(0)
			switch fault {
			case "size":
				_ = os.WriteFile(p, []byte("short"), 0600)
			case "same-size-content":
				data := fileInputsData(t)["sources/A.gpkg"]
				data[0] ^= 0xff
				_ = os.WriteFile(p, data, 0600)
			}
			h := newFileInputsHTTP(t, "")
			if err := b.Upload(t.Context(), h.connection, fileInputRuntimeID, runtimeFileInputsCredential, h.record); err == nil {
				t.Fatal("tampered file accepted")
			}
			if _, err := b.Execute(t.Context(), "zh"); err == nil || h.dispatches.Load() != 0 {
				t.Fatal("tampering reached computation")
			}
			result, err := b.Cleanup(t.Context())
			if err != nil || !result.LocalRemoved {
				t.Fatal(result, err)
			}
		})
	}
}

func TestFileWorkflowInputsSourceFailureRetainsPendingCleanup(t *testing.T) {
	facts, issued, lease, base := fileInputsFixture(t, false)
	entered := make(chan struct{})
	revoked := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	var calls atomic.Int32
	auth := fileSourceAuthorityFunc(func(c context.Context, i client.IssuedDevelopFileAuthorization, r client.DevelopFileSourceAccessRequest) (*client.DevelopFileSourceAccessObservation, error) {
		if calls.Add(1) == 1 {
			// The window starts when the fixture returns the authorization.
			observation := base
			observation.ObservedAt = time.Now()
			observation.ExpiresAt = observation.ObservedAt.Add(220 * time.Millisecond)
			return fileInputsAuthority(observation)(c, i, r)
		}
		// Revoke only after Open is in flight so this exercises pending cleanup.
		select {
		case <-entered:
			close(revoked)
			return nil, errors.New("revoked")
		case <-c.Done():
			return nil, c.Err()
		}
	})
	provider := fileInputsProvider(fileInputsData(t))
	provider.open = func(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
		close(entered)
		<-release
		set, _ := plugin.NewQueryReadSet(facts.Intent.Sources[0].Target)
		return &sourceWindowHandle{Reader: strings.NewReader("late"), set: set}, nil
	}
	type result struct {
		b   *fileWorkflowInputs
		err error
	}
	done := make(chan result, 1)
	go func() {
		b, e := prepareFileWorkflowInputs(t.Context(), "fixture-instance", auth, provider, facts, issued, lease, recordFileInputsDirectory)
		done <- result{b, e}
	}()
	var r result
	select {
	case r = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("blocked RPC held preparation beyond observation")
	}
	if r.b == nil || r.err == nil {
		t.Fatal("invalid partial batch accepted")
	}
	t.Cleanup(func() { _ = os.RemoveAll(r.b.directory) })
	select {
	case <-revoked:
	default:
		t.Fatalf("preparation returned before revoking an in-flight Open: %v", r.err)
	}
	state, err := r.b.Cleanup(t.Context())
	if err == nil || state.SourcesStopped || state.LocalRemoved {
		t.Fatal("blocked source falsely cleaned", state, err)
	}
	unblock()
	waitSourceStopped(t, r.b.sources[0])
	cleanupFileInputs(t, r.b)
}

func TestFileWorkflowInputsRuntimeCleanupPendingIsNotLocalSuccess(t *testing.T) {
	for _, fault := range []string{"cleanup-pending", "cleanup-unknown"} {
		t.Run(fault, func(t *testing.T) {
			facts, issued, lease, base := fileInputsFixture(t, false)
			b, err := prepareFileWorkflowInputs(t.Context(), "fixture-instance", fileInputsAuthority(base), fileInputsProvider(fileInputsData(t)), facts, issued, lease, recordFileInputsDirectory)
			if err != nil {
				t.Fatal(err)
			}
			// Only the HTTP double exists; this final removal is test fixture disposal,
			// not a claim that the consumer's pending Runtime cleanup succeeded.
			t.Cleanup(func() { _ = os.RemoveAll(b.directory) })
			h := newFileInputsHTTP(t, fault)
			if err := b.Upload(t.Context(), h.connection, fileInputRuntimeID, runtimeFileInputsCredential, h.record); err != nil {
				t.Fatal(err)
			}
			state, err := b.Cleanup(t.Context())
			if err == nil || state.RuntimeCleanup != "pending" || state.LocalRemoved || !state.SourcesStopped {
				t.Fatal("unknown Runtime cleanup claimed success", state, err)
			}
			if _, err := os.Stat(b.directory); err != nil {
				t.Fatal("pending evidence removed early", err)
			}
		})
	}
}

func TestFileWorkflowInputsRevocationCancelsUploadAndPreventsDispatch(t *testing.T) {
	facts, issued, lease, base := fileInputsFixture(t, false)
	base.ExpiresAt = time.Now().Add(600 * time.Millisecond)
	var revoked atomic.Bool
	original := fileInputsAuthority(base)
	auth := fileSourceAuthorityFunc(func(c context.Context, i client.IssuedDevelopFileAuthorization, r client.DevelopFileSourceAccessRequest) (*client.DevelopFileSourceAccessObservation, error) {
		if revoked.Load() {
			return nil, errors.New("revoked")
		}
		return original(c, i, r)
	})
	b, err := prepareFileWorkflowInputs(t.Context(), "fixture-instance", auth, fileInputsProvider(fileInputsData(t)), facts, issued, lease, recordFileInputsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	h := newFileInputsHTTP(t, "upload-blocked")
	done := make(chan error, 1)
	go func() {
		done <- b.Upload(t.Context(), h.connection, fileInputRuntimeID, runtimeFileInputsCredential, h.record)
	}()
	select {
	case <-h.uploadStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("upload did not begin")
	}
	revoked.Store(true)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("revocation accepted private staging")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("upload ignored renewed source denial")
	}
	close(h.uploadRelease)
	if _, err := b.Execute(t.Context(), "zh"); err == nil || h.dispatches.Load() != 0 {
		t.Fatal("invalid input dispatched")
	}
	state, err := b.Cleanup(t.Context())
	if err != nil || !state.LocalRemoved || !state.SourcesStopped {
		t.Fatal(state, err)
	}
}

func TestFileWorkflowInputsDifferentSourceDeadlinesRejectWholeBatch(t *testing.T) {
	facts, issued, lease, base := fileInputsFixture(t, false)
	original := fileInputsAuthority(base)
	auth := fileSourceAuthorityFunc(func(c context.Context, i client.IssuedDevelopFileAuthorization, r client.DevelopFileSourceAccessRequest) (*client.DevelopFileSourceAccessObservation, error) {
		response, err := original(c, i, r)
		if r.Source.StringPath() == "sources/B.gpkg" {
			response.BusinessDeadline = response.BusinessDeadline.Add(time.Second)
		}
		return response, err
	})
	b, err := prepareFileWorkflowInputs(t.Context(), "fixture-instance", auth, fileInputsProvider(fileInputsData(t)), facts, issued, lease, recordFileInputsDirectory)
	if b != nil {
		defer cleanupFileInputs(t, b)
	}
	if b == nil || err == nil || b.prepared {
		t.Fatal("different source deadline accepted", err)
	}
	h := newFileInputsHTTP(t, "")
	if err := b.Upload(t.Context(), h.connection, fileInputRuntimeID, runtimeFileInputsCredential, h.record); err == nil || h.calls.Load() != 0 {
		t.Fatal("partial or inconsistent batch reached Runtime")
	}
}

func TestFileWorkflowInputsRecordedCleanupRequiresOriginalInstanceAndBatch(t *testing.T) {
	facts, issued, lease, base := fileInputsFixture(t, false)
	b, err := prepareFileWorkflowInputs(t.Context(), "fixture-instance", fileInputsAuthority(base), fileInputsProvider(fileInputsData(t)), facts, issued, lease, recordFileInputsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	h := newFileInputsHTTP(t, "")
	var registered repository.PrivateFileRuntimeRecord
	record := func(ctx context.Context, r repository.PrivateFileRuntimeRecord) error {
		registered = r
		return h.record(ctx, r)
	}
	if err := b.Upload(t.Context(), h.connection, fileInputRuntimeID, runtimeFileInputsCredential, record); err != nil {
		t.Fatal(err)
	}
	if registered.OwnerInstanceID != "fixture-instance" || registered.InputDirectory != b.directory {
		t.Fatal("local ownership not registered")
	}
	for _, fault := range []string{"current-instance", "record-instance", "directory", "runtime", "execution", "attempt", "scope", "tenant", "authorization"} {
		bad := registered
		current := "fixture-instance"
		switch fault {
		case "current-instance":
			current = "other-instance"
		case "record-instance":
			bad.OwnerInstanceID = "other-instance"
		case "directory":
			bad.InputDirectory = t.TempDir()
		case "runtime":
			bad.RuntimeExecutionID = fileSourceLease
		case "execution":
			bad.Manifest.ExecutionID = fileSourceLease
		case "attempt":
			bad.Manifest.Attempt = 2
		case "scope":
			bad.Manifest.ScopeDigest = strings.Repeat("f", 64)
		case "tenant":
			bad.Manifest.TenantID = 4
		case "authorization":
			bad.Manifest.AuthorizationID = 10
		}
		state, err := b.CleanupRecorded(t.Context(), current, bad)
		if err == nil || state.LocalRemoved || state.RuntimeCleanup != "pending" || h.cleanup.Load() != 0 {
			t.Fatal("unproven cleanup changed resources", fault, state, err)
		}
		if _, err := os.Stat(b.directory); err != nil {
			t.Fatal("foreign recovery deleted original directory", fault, err)
		}
	}
	state, err := b.CleanupRecorded(t.Context(), "fixture-instance", registered)
	if err != nil || !state.LocalRemoved || !state.SourcesStopped || state.RuntimeCleanup != "passed" || h.cleanup.Load() != 1 {
		t.Fatal(state, err)
	}
}

// Explicit persistence double for tests that isolate preparation/transport.
func recordFileInputsDirectory(ctx context.Context, instanceID, directory string) error {
	return ctx.Err()
}

func TestFileWorkflowInputsDirectoryFailureStopsBeforeSourceAccess(t *testing.T) {
	facts, issued, lease, base := fileInputsFixture(t, false)
	var checks atomic.Int32
	original := fileInputsAuthority(base)
	authority := fileSourceAuthorityFunc(func(c context.Context, i client.IssuedDevelopFileAuthorization, r client.DevelopFileSourceAccessRequest) (*client.DevelopFileSourceAccessObservation, error) {
		checks.Add(1)
		return original(c, i, r)
	})
	b, err := prepareFileWorkflowInputs(t.Context(), "fixture-instance", authority, fileInputsProvider(fileInputsData(t)), facts, issued, lease, func(context.Context, string, string) error { return errors.New("directory registration unknown") })
	if b != nil {
		defer cleanupFileInputs(t, b)
	}
	if b == nil || err == nil || checks.Load() != 0 {
		t.Fatal("unconfirmed directory reached source IO", err, checks.Load())
	}
}

func TestFileWorkflowInputsDirectoryOnlyRecoveryDoesNotInventRuntime(t *testing.T) {
	facts, issued, lease, base := fileInputsFixture(t, false)
	b, err := prepareFileWorkflowInputs(t.Context(), "fixture-instance", fileInputsAuthority(base), fileInputsProvider(fileInputsData(t)), facts, issued, lease, recordFileInputsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	r := repository.PrivateInputDirectoryRecord{ExecutionID: facts.ExecutionID, TenantID: 3, Attempt: 1, AuthorizationID: 9, ScopeDigest: facts.ScopeDigest, OwnerInstanceID: "fixture-instance", InputDirectory: b.directory}
	state, err := b.CleanupInputDirectory(t.Context(), "other-instance", r)
	if err == nil || state.LocalRemoved {
		t.Fatal("cross-instance directory cleanup allowed")
	}
	state, err = b.CleanupInputDirectory(t.Context(), "fixture-instance", r)
	if err != nil || !state.LocalRemoved || !state.SourcesStopped || state.RuntimeCleanup != "not_created" {
		t.Fatal(state, err)
	}
}

func TestFileWorkflowInputsRejectChangedGeoPackageBeforeRuntimeRegistration(t *testing.T) {
	for _, fault := range []string{"early sidecar", "late sidecar", "unknown sidecar", "version changed", "short read", "invalid staged bytes", "WAL header"} {
		t.Run(fault, func(t *testing.T) {
			facts, issued, lease, base := fileInputsFixture(t, false)
			data := fileInputsData(t)
			key := facts.Intent.Sources[0].Target.StringPath()
			switch fault {
			case "invalid staged bytes":
				data[key][0] = 0
			case "WAL header":
				data[key][18], data[key][19] = 2, 2
			}
			provider := fileInputsProvider(data)
			originalFacts, originalOpen := provider.facts, provider.open
			var opens atomic.Int32
			provider.facts = func(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogFacts, error) {
				if conn["server"] != "test-only" || path.EngineID != 11 {
					t.Error("NFS facts escaped the authorized connection")
				}
				name := path.StringPath()
				if name != key && name != key+"-wal" && name != key+"-shm" && name != key+"-journal" {
					t.Error("probe escaped the first exact source", name)
				}
				if name == key+"-wal" && (fault == "early sidecar" || fault == "late sidecar" && opens.Load() > 0) {
					return &plugin.EngineCatalogFacts{Path: path}, nil
				}
				if name == key+"-shm" && fault == "unknown sidecar" {
					return nil, os.ErrPermission
				}
				f, err := originalFacts(ctx, conn, path)
				if f != nil && fault == "version changed" && opens.Load() > 0 {
					f.Storage.ETag = "changed after Open"
				}
				return f, err
			}
			provider.open = func(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
				opens.Add(1)
				if path.StringPath() != key {
					t.Error("opened an unproven next source or sidecar")
				}
				if fault == "short read" {
					set, _ := plugin.NewQueryReadSet(path)
					return &sourceWindowHandle{Reader: bytes.NewReader(data[key][:len(data[key])-1]), set: set}, nil
				}
				return originalOpen(ctx, conn, path, opts)
			}
			b, err := prepareFileWorkflowInputs(t.Context(), "fixture-instance", fileInputsAuthority(base), provider, facts, issued, lease, recordFileInputsDirectory)
			if b != nil {
				defer cleanupFileInputs(t, b)
			}
			if b == nil || err == nil || b.prepared || len(b.manifest.Inputs) != 0 {
				t.Fatal("unproven file entered the manifest", err)
			}
			wantOpens := int32(1)
			if fault == "early sidecar" || fault == "unknown sidecar" {
				wantOpens = 0
			}
			if opens.Load() != wantOpens {
				t.Fatal("incorrect source Open count", opens.Load())
			}
			if err := b.Upload(t.Context(), nil, fileInputRuntimeID, runtimeFileInputsCredential, func(context.Context, repository.PrivateFileRuntimeRecord) error {
				t.Error("unproven file reached Runtime registration")
				return nil
			}); err == nil || b.attempted {
				t.Fatal("unproven input accepted for upload")
			}
		})
	}
}

func TestFileWorkflowInputsBlockedPostReadFactsRemainPending(t *testing.T) {
	facts, issued, lease, base := fileInputsFixture(t, false)
	provider := fileInputsProvider(fileInputsData(t))
	original := provider.facts
	path := facts.Intent.Sources[0].Target.StringPath()
	var mainProbes atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	provider.facts = func(ctx context.Context, conn plugin.ConnectionInfo, target plugin.EngineCatalogPath) (*plugin.EngineCatalogFacts, error) {
		if target.StringPath() == path && mainProbes.Add(1) == 2 {
			close(entered)
			<-release
		}
		return original(ctx, conn, target)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type preparation struct {
		b   *fileWorkflowInputs
		err error
	}
	done := make(chan preparation, 1)
	go func() {
		b, err := prepareFileWorkflowInputs(ctx, "fixture-instance", fileInputsAuthority(base), provider, facts, issued, lease, recordFileInputsDirectory)
		done <- preparation{b, err}
	}()
	select {
	case <-entered:
	case r := <-done:
		cleanupFileInputs(t, r.b)
		t.Fatal("no post-read observation", r.err)
	case <-time.After(3 * time.Second):
		t.Fatal("post-read observation did not start")
	}
	cancel()
	select {
	case r := <-done:
		if r.err == nil || r.b == nil || r.b.prepared {
			t.Fatal("cancelled observation allowed preparation", r.err)
		}
		state, err := r.b.Cleanup(t.Context())
		if err == nil || state.SourcesStopped || state.LocalRemoved {
			t.Fatal("blocked NFS observation falsely cleaned", state, err)
		}
		unblock()
		cleanupFileInputs(t, r.b)
	case <-time.After(3 * time.Second):
		t.Fatal("blocked observation delayed cancellation")
	}
}

// The removed inline-GeoJSON path's content contract now belongs to Owner
// staging. A rejected file must never be handed to the private Runtime.
func TestPrivateGeoJSONStagingRetainsOriginalContentContract(t *testing.T) {
	for _, value := range []struct {
		name, body string
		valid      bool
	}{
		{"empty collection", `{"type":"FeatureCollection","features":[]}`, true},
		{"declared CRS", `{"type":"FeatureCollection","features":[],"crs":{"type":"name","properties":{"name":"EPSG:3857"}}}`, false},
		{"null features", `{"type":"FeatureCollection","features":null}`, false},
		{"other object", `{"type":"Feature","geometry":null}`, false},
		{"truncated", `{"type":"FeatureCollection"`, false},
		{"oversized", strings.Repeat(" ", maxWorkflowGeoJSONBytes+1), false},
	} {
		t.Run(value.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "input-0.geojson")
			if err := os.WriteFile(file, []byte(value.body), 0600); err != nil {
				t.Fatal(err)
			}
			err := validatePrivateFileContent(t.Context(), "geojson", file)
			if (err == nil) != value.valid {
				t.Fatal("GeoJSON content contract", err)
			}
		})
	}
}
