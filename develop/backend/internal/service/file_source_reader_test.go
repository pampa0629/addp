package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/nfs"
	"github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
)

const fileSourceExecution = "fa6c2d0e-8b34-4a16-b41c-d645619ea782"
const fileSourceLease = "05b4b5f6-3343-41c4-97d3-a0aa527e42dc"

type fileSourceAuthorityFunc func(context.Context, client.IssuedDevelopFileAuthorization, client.DevelopFileSourceAccessRequest) (*client.DevelopFileSourceAccessObservation, error)

func (f fileSourceAuthorityFunc) CheckDevelopFileSourceAccess(ctx context.Context, issued client.IssuedDevelopFileAuthorization, req client.DevelopFileSourceAccessRequest) (*client.DevelopFileSourceAccessObservation, error) {
	return f(ctx, issued, req)
}

type sourceWindowProvider struct {
	nfs.NFSPlugin
	open  func(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error)
	facts func(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath) (*plugin.EngineCatalogFacts, error)
}

func (p *sourceWindowProvider) OpenContent(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath, o plugin.ReadOptions) (io.ReadCloser, error) {
	return p.open(ctx, c, path, o)
}

func (p *sourceWindowProvider) DescribeEngineCatalogFacts(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, _ plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	if p.facts != nil {
		return p.facts(ctx, conn, path)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if strings.HasSuffix(path.StringPath(), suffix) {
			return nil, os.ErrNotExist
		}
	}
	return sourceWindowFacts(path, 0), nil
}

func sourceWindowFacts(path plugin.EngineCatalogPath, size int64) *plugin.EngineCatalogFacts {
	return &plugin.EngineCatalogFacts{Path: path, Kind: "file", Storage: &plugin.EngineCatalogStorageFacts{
		Path: path.StringPath(), SizeBytes: &size, ETag: "fixture-version",
	}}
}

type sourceWindowHandle struct {
	io.Reader
	set   *plugin.QueryReadSet
	close func() error
}

func (h *sourceWindowHandle) ReadSet(context.Context) (*plugin.QueryReadSet, error) {
	return h.set.Clone(), nil
}
func (h *sourceWindowHandle) Close() error {
	if h.close != nil {
		return h.close()
	}
	return nil
}

type sourceWindowReaderFunc func([]byte) (int, error)

func (f sourceWindowReaderFunc) Read(p []byte) (int, error) { return f(p) }

func fileSourceFixture(t *testing.T, window time.Duration) (client.IssuedDevelopFileAuthorization, client.DevelopFileSourceAccessRequest, client.DevelopFileSourceAccessObservation) {
	t.Helper()
	intent, err := execution.NewDevelopFileIntent(json.RawMessage(`{"task":"load","number":9007199254740993}`), 1, 600, []execution.DevelopFileSourceIntent{{NodeID: "load", Target: plugin.FileItemPath(11, "sources/ 原始.gpkg ")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := execution.NewDevelopBoundFileScope(*intent, []execution.DevelopFileSourceReference{{Target: intent.Sources[0].Target, ItemFingerprint: strings.Repeat("a", 64), BoundaryHash: strings.Repeat("b", 64), EngineVersion: "2"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := scope.Digest()
	issued := client.IssuedDevelopFileAuthorization{Authorization: client.IssuedExecutionAuthorization{ID: "9", ExecutionID: fileSourceExecution, Audience: "develop", TenantID: "3", ActorPrincipalID: "31", TenantMembershipID: "41", IssuedAuthorizationVersion: "5", SourceType: "user", Accesses: []client.ExecutionEngineAccessScope{{EngineID: "11", Effects: []string{"read"}}}, ExpiresAt: time.Now().Add(time.Hour)}, Scope: *scope, ScopeDigest: digest}
	req := client.DevelopFileSourceAccessRequest{ExecutionID: fileSourceExecution, Attempt: 1, ScopeDigest: digest, LeaseToken: fileSourceLease, Source: scope.Sources[0].Target}
	tenant := uint(3)
	obs := client.DevelopFileSourceAccessObservation{AuthorizationID: "9", ExecutionID: req.ExecutionID, Attempt: 1, ScopeDigest: digest, Source: req.Source, ObservedAt: time.Now(), ExpiresAt: time.Now().Add(window), BusinessDeadline: time.Now().Add(10 * time.Minute), Engine: &commonModels.Engine{ID: 11, TenantID: &tenant, EngineType: "nfs", Version: 2, LifecycleState: commonModels.EngineLifecycleActive, ConnectionInfo: commonModels.ConnectionInfo{"server": "test-only", "export_path": "/owned"}}}
	return issued, req, obs
}

func fixedSourceAuthority(obs client.DevelopFileSourceAccessObservation) fileSourceAuthorityFunc {
	return func(context.Context, client.IssuedDevelopFileAuthorization, client.DevelopFileSourceAccessRequest) (*client.DevelopFileSourceAccessObservation, error) {
		copy := obs
		return &copy, nil
	}
}
func sourceWindowData(req client.DevelopFileSourceAccessRequest, data []byte) *sourceWindowProvider {
	set, _ := plugin.NewQueryReadSet(req.Source)
	return &sourceWindowProvider{
		facts: func(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogFacts, error) {
			if !reflect.DeepEqual(path, req.Source) {
				return nil, os.ErrNotExist
			}
			return sourceWindowFacts(path, int64(len(data))), nil
		},
		open: func(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
			return &sourceWindowHandle{Reader: bytes.NewReader(data), set: set}, nil
		},
	}
}
func waitSourceStopped(t *testing.T, s *fileSourceReader) {
	t.Helper()
	select {
	case <-s.stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("actual provider did not stop")
	}
}

func TestFileSourceWindowFullEOFAndBoundEvidence(t *testing.T) {
	issued, req, obs := fileSourceFixture(t, time.Minute)
	data := bytes.Repeat([]byte("complete raw GPKG bytes fixture"), 4000)
	provider := sourceWindowData(req, data)
	var opened atomic.Int32
	base := provider.open
	provider.open = func(ctx context.Context, c plugin.ConnectionInfo, p plugin.EngineCatalogPath, o plugin.ReadOptions) (io.ReadCloser, error) {
		opened.Add(1)
		if !reflect.DeepEqual(p, req.Source) || c["server"] != "test-only" || o.Offset != 0 || o.Length != 0 {
			t.Error("source path, connection or full read changed")
		}
		if deadline, ok := ctx.Deadline(); !ok || !deadline.Equal(obs.BusinessDeadline) {
			t.Error("short observation used as fixed stream deadline")
		}
		return base(ctx, c, p, o)
	}
	s, err := newFileSourceReader(t.Context(), fixedSourceAuthority(obs), provider, issued, req)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if r, err := s.Receipt(t.Context()); r != nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("pre-EOF source closure passed as completion", r, err)
	}
	actual, err := io.ReadAll(s)
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal(len(actual), err)
	}
	r, err := s.Receipt(t.Context())
	sum := sha256.Sum256(data)
	if err != nil || r.Size != int64(len(data)) || r.SHA256 != hex.EncodeToString(sum[:]) || !r.BusinessDeadline.Equal(obs.BusinessDeadline) || !reflect.DeepEqual(r.ReadSet.Paths, []plugin.EngineCatalogPath{req.Source}) {
		t.Fatal(r, err)
	}
	if opened.Load() != 1 {
		t.Fatal("source reopened", opened.Load())
	}
	r.ReadSet.Paths[0].EngineID = 99
	again, err := s.Receipt(t.Context())
	if err != nil || again.ReadSet.Paths[0].EngineID != 11 {
		t.Fatal("receipt source proof mutated")
	}
}

func TestFileSourceWindowRejectsInitialBindingBeforeOpen(t *testing.T) {
	for _, fault := range []string{"denied", "missing", "authorization", "execution", "attempt", "digest", "source", "tenant", "version", "engine", "lifecycle", "connection", "expired", "future-observed", "missing-business", "window-over-business", "business-over-ea"} {
		t.Run(fault, func(t *testing.T) {
			issued, req, obs := fileSourceFixture(t, time.Minute)
			switch fault {
			case "authorization":
				obs.AuthorizationID = "10"
			case "execution":
				obs.ExecutionID = fileSourceLease
			case "attempt":
				obs.Attempt = 2
			case "digest":
				obs.ScopeDigest = strings.Repeat("c", 64)
			case "source":
				obs.Source = plugin.FileItemPath(11, "neighbor.gpkg")
			case "tenant":
				v := uint(4)
				obs.Engine.TenantID = &v
			case "version":
				obs.Engine.Version++
			case "engine":
				obs.Engine.ID = 12
			case "lifecycle":
				obs.Engine.LifecycleState = "offline"
			case "connection":
				obs.Engine.ConnectionInfo = nil
			case "expired":
				obs.ExpiresAt = time.Now().Add(-time.Second)
			case "future-observed":
				obs.ObservedAt = time.Now().Add(time.Hour)
			case "missing-business":
				obs.BusinessDeadline = time.Time{}
			case "window-over-business":
				obs.ExpiresAt = obs.BusinessDeadline.Add(time.Second)
			case "business-over-ea":
				obs.BusinessDeadline = issued.Authorization.ExpiresAt.Add(time.Second)
			}
			var opened atomic.Int32
			provider := sourceWindowData(req, []byte("never"))
			provider.open = func(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
				opened.Add(1)
				return nil, errors.New("must not open")
			}
			auth := fileSourceAuthorityFunc(func(context.Context, client.IssuedDevelopFileAuthorization, client.DevelopFileSourceAccessRequest) (*client.DevelopFileSourceAccessObservation, error) {
				if fault == "denied" {
					return nil, errors.New("denied")
				}
				if fault == "missing" {
					return nil, nil
				}
				return &obs, nil
			})
			if s, err := newFileSourceReader(t.Context(), auth, provider, issued, req); s != nil || err == nil {
				t.Fatal("bad eligibility accepted", s, err)
			}
			if opened.Load() != 0 {
				t.Fatal("opened before current authorization")
			}
		})
	}
}

func TestFileSourceWindowRenewalRetainsFixedDeadlineAndOriginalBinding(t *testing.T) {
	issued, req, obs := fileSourceFixture(t, 400*time.Millisecond)
	var calls atomic.Int32
	renewed := make(chan struct{})
	auth := fileSourceAuthorityFunc(func(ctx context.Context, i client.IssuedDevelopFileAuthorization, r client.DevelopFileSourceAccessRequest) (*client.DevelopFileSourceAccessObservation, error) {
		if !reflect.DeepEqual(i, issued) || !reflect.DeepEqual(r, req) {
			t.Error("renewal changed original binding")
		}
		copy := obs
		if calls.Add(1) > 1 {
			copy.ObservedAt = time.Now()
			copy.ExpiresAt = time.Now().Add(time.Minute)
			close(renewed)
		}
		return &copy, nil
	})
	started := make(chan struct{})
	release := make(chan struct{})
	provider := sourceWindowData(req, []byte("late but authorized"))
	set, _ := plugin.NewQueryReadSet(req.Source)
	provider.open = func(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
		return &sourceWindowHandle{set: set, Reader: sourceWindowReaderFunc(func(p []byte) (int, error) {
			close(started)
			<-release
			return copy(p, []byte("late but authorized")), io.EOF
		})}, nil
	}
	s, err := newFileSourceReader(t.Context(), auth, provider, issued, req)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	done := make(chan error, 1)
	go func() { _, err := io.ReadAll(s); done <- err }()
	<-started
	select {
	case <-renewed:
	case <-time.After(3 * time.Second):
		t.Fatal("did not renew during blocked Read")
	}
	// Deliberately cross the original short window while retaining fixed budget.
	timer := time.NewTimer(time.Until(obs.ExpiresAt.Add(20 * time.Millisecond)))
	<-timer.C
	close(release)
	if err := <-done; err != nil {
		t.Fatal("valid renewal did not preserve read", err)
	}
	receipt, err := s.Receipt(t.Context())
	if err != nil || !receipt.BusinessDeadline.Equal(obs.BusinessDeadline) || calls.Load() != 2 {
		t.Fatal(receipt, err, calls.Load())
	}
}

func TestFileSourceWindowRejectsRenewalAndDiscardsBlockedRead(t *testing.T) {
	for _, fault := range []string{"revoked", "timeout", "changed-deadline", "changed-source", "changed-connection", "expired"} {
		t.Run(fault, func(t *testing.T) {
			issued, req, obs := fileSourceFixture(t, 200*time.Millisecond)
			var calls atomic.Int32
			auth := fileSourceAuthorityFunc(func(ctx context.Context, _ client.IssuedDevelopFileAuthorization, _ client.DevelopFileSourceAccessRequest) (*client.DevelopFileSourceAccessObservation, error) {
				next := obs
				if calls.Add(1) == 1 {
					return &next, nil
				}
				switch fault {
				case "revoked":
					return nil, errors.New("revoked")
				case "timeout":
					<-ctx.Done()
					return nil, ctx.Err()
				case "changed-deadline":
					next.BusinessDeadline = next.BusinessDeadline.Add(time.Second)
				case "changed-source":
					next.Source = plugin.FileItemPath(11, "neighbor.gpkg")
				case "changed-connection":
					v := *next.Engine
					v.ConnectionInfo = commonModels.ConnectionInfo{"server": "neighbor"}
					next.Engine = &v
				case "expired":
					next.ExpiresAt = time.Now().Add(-time.Second)
				}
				next.ObservedAt = time.Now()
				if fault != "expired" {
					next.ExpiresAt = time.Now().Add(time.Minute)
				}
				return &next, nil
			})
			set, _ := plugin.NewQueryReadSet(req.Source)
			started := make(chan struct{})
			release := make(chan struct{})
			var reads atomic.Int32
			provider := &sourceWindowProvider{open: func(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
				return &sourceWindowHandle{set: set, Reader: sourceWindowReaderFunc(func(p []byte) (int, error) {
					reads.Add(1)
					close(started)
					<-release
					return copy(p, []byte("must not escape")), io.EOF
				})}, nil
			}}
			s, err := newFileSourceReader(t.Context(), auth, provider, issued, req)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			output := bytes.Repeat([]byte{0xaa}, 32)
			done := make(chan error, 1)
			go func() {
				n, err := s.Read(output)
				if n != 0 {
					t.Error("late bytes delivered", n)
				}
				done <- err
			}()
			<-started
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("failed renewal continued")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("blocked Read held caller past expired window")
			}
			if s.State().Stopped || !s.State().CleanupPending {
				t.Fatal("cancellation falsely proved RPC stopped", s.State())
			}
			if err := s.Close(); !errors.Is(err, errFileSourceCleanupPending) {
				t.Fatal("close falsely completed", err)
			}
			if receipt, err := s.Receipt(t.Context()); receipt != nil || err == nil {
				t.Fatal("invalid partial staging accepted")
			}
			close(release)
			waitSourceStopped(t, s)
			if !bytes.Equal(output, bytes.Repeat([]byte{0xaa}, 32)) || reads.Load() != 1 || !s.State().Stopped || s.State().CleanupPending {
				t.Fatal("late worker mutated caller buffer/restarted I/O", output, reads.Load(), s.State())
			}
		})
	}
}

func TestFileSourceWindowBlockedOpenAndCloseRemainPending(t *testing.T) {
	for _, stage := range []string{"open", "close"} {
		t.Run(stage, func(t *testing.T) {
			issued, req, obs := fileSourceFixture(t, time.Minute)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			entered := make(chan struct{})
			release := make(chan struct{})
			set, _ := plugin.NewQueryReadSet(req.Source)
			provider := &sourceWindowProvider{open: func(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
				if stage == "open" {
					close(entered)
					<-release
				}
				return &sourceWindowHandle{set: set, Reader: bytes.NewReader(nil), close: func() error {
					if stage == "close" {
						close(entered)
						<-release
					}
					return nil
				}}, nil
			}}
			s, err := newFileSourceReader(ctx, fixedSourceAuthority(obs), provider, issued, req)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if stage == "close" {
				if _, err := io.ReadAll(s); err != nil {
					t.Fatal(err)
				}
			}
			<-entered
			cancel()
			if err := s.Close(); !errors.Is(err, errFileSourceCleanupPending) || s.State().Stopped || !s.State().CleanupPending {
				t.Fatal("blocked "+stage+" falsely stopped", err, s.State())
			}
			close(release)
			waitSourceStopped(t, s)
		})
	}
}

func TestFileSourceWindowRejectsMissingOrChangedSessionEvidence(t *testing.T) {
	for _, fault := range []string{"missing", "neighbor", "extra", "open-error", "read-error", "close-error"} {
		t.Run(fault, func(t *testing.T) {
			issued, req, obs := fileSourceFixture(t, time.Minute)
			set, _ := plugin.NewQueryReadSet(req.Source)
			if fault == "neighbor" {
				set, _ = plugin.NewQueryReadSet(plugin.FileItemPath(11, "neighbor.gpkg"))
			}
			if fault == "extra" {
				set, _ = plugin.NewQueryReadSet(req.Source, plugin.FileItemPath(11, "neighbor.gpkg"))
			}
			provider := &sourceWindowProvider{open: func(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
				if fault == "missing" {
					return io.NopCloser(strings.NewReader("bad")), nil
				}
				if fault == "open-error" {
					return nil, errors.New("open")
				}
				h := &sourceWindowHandle{set: set, Reader: strings.NewReader("private")}
				if fault == "read-error" {
					h.Reader = sourceWindowReaderFunc(func(p []byte) (int, error) { return copy(p, []byte("bad")), io.ErrUnexpectedEOF })
				}
				if fault == "close-error" {
					h.close = func() error { return errors.New("close") }
				}
				return h, nil
			}}
			s, err := newFileSourceReader(t.Context(), fixedSourceAuthority(obs), provider, issued, req)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			data, readErr := io.ReadAll(s)
			if fault != "close-error" && (len(data) != 0 || readErr == nil) {
				t.Fatal("unproven/failed source bytes escaped", data, readErr)
			}
			if receipt, err := s.Receipt(t.Context()); receipt != nil || err == nil {
				t.Fatal("bad source produced receipt", receipt, err)
			}
			waitSourceStopped(t, s)
		})
	}
}

// Actual SDK wire consumption with an explicit System HTTP double. This is not
// a real IAM grant, actual NFS session, business workflow or Hosted acceptance.
func TestFileSourceWindowUsesCurrentSDKCredentialForEachObservation(t *testing.T) {
	issued, req, obs := fileSourceFixture(t, 400*time.Millisecond)
	var calls atomic.Int32
	renewed := make(chan struct{})
	var lock sync.Mutex
	var credentials []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var actual client.DevelopFileSourceAccessRequest
		if r.URL.Path != "/api/v1/system/execution-authorizations/9/develop-geopackage-source-accesses" || r.Method != "POST" || json.NewDecoder(r.Body).Decode(&actual) != nil || !reflect.DeepEqual(actual, req) {
			t.Error("wire binding changed")
		}
		lock.Lock()
		credentials = append(credentials, r.Header.Get("Authorization"))
		lock.Unlock()
		copy := obs
		n := calls.Add(1)
		if n > 1 {
			copy.ObservedAt = time.Now()
			copy.ExpiresAt = time.Now().Add(time.Minute)
		}
		_ = json.NewEncoder(w).Encode(copy)
		if n == 2 {
			close(renewed)
		}
	}))
	defer server.Close()
	tokens := &sourceWindowTokens{}
	sdk := client.NewSystemServiceClient(server.URL, tokens, server.Client()).WithTenantID(3)
	s, err := newFileSourceReader(t.Context(), sdk, sourceWindowData(req, []byte("exact")), issued, req)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	<-renewed
	if _, err := io.ReadAll(s); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Receipt(t.Context()); err != nil {
		t.Fatal(err)
	}
	lock.Lock()
	defer lock.Unlock()
	if len(credentials) != 2 || credentials[0] != "Bearer addp_at_first" || credentials[1] != "Bearer addp_at_current" {
		t.Fatal("old machine credential cached", credentials)
	}
}

type sourceWindowTokens struct{ calls atomic.Int32 }

func (s *sourceWindowTokens) Token(context.Context, uint) (string, error) {
	if s.calls.Add(1) == 1 {
		return "addp_at_first", nil
	}
	return "addp_at_current", nil
}
func (s *sourceWindowTokens) PlatformToken(context.Context) (string, error) {
	return "", errors.New("must not use platform token")
}

func TestFileSourceWindowLateAuthorizationCannotReviveExpiredSession(t *testing.T) {
	issued, req, obs := fileSourceFixture(t, 200*time.Millisecond)
	checkStarted := make(chan struct{})
	checkRelease := make(chan struct{})
	checkReturned := make(chan struct{})
	var calls atomic.Int32
	auth := fileSourceAuthorityFunc(func(context.Context, client.IssuedDevelopFileAuthorization, client.DevelopFileSourceAccessRequest) (*client.DevelopFileSourceAccessObservation, error) {
		next := obs
		if calls.Add(1) > 1 {
			close(checkStarted)
			<-checkRelease
			next.ObservedAt = time.Now()
			next.ExpiresAt = time.Now().Add(time.Minute)
			defer close(checkReturned)
		}
		return &next, nil
	})
	readStarted := make(chan struct{})
	readRelease := make(chan struct{})
	set, _ := plugin.NewQueryReadSet(req.Source)
	provider := &sourceWindowProvider{open: func(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
		return &sourceWindowHandle{set: set, Reader: sourceWindowReaderFunc(func(p []byte) (int, error) {
			close(readStarted)
			<-readRelease
			return copy(p, []byte("untrusted late data")), io.EOF
		})}, nil
	}}
	s, err := newFileSourceReader(t.Context(), auth, provider, issued, req)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	done := make(chan error, 1)
	go func() { _, err := s.Read(make([]byte, 32)); done <- err }()
	<-readStarted
	<-checkStarted
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("authorization still pending but bytes allowed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("old window did not cancel independent of blocked authority")
	}
	close(checkRelease)
	<-checkReturned
	close(readRelease)
	waitSourceStopped(t, s)
	if n, err := s.Read(make([]byte, 1)); n != 0 || err == nil {
		t.Fatal("late successful authorization revived old session")
	}
	if receipt, err := s.Receipt(t.Context()); receipt != nil || err == nil {
		t.Fatal("late observation accepted old staging")
	}
}

func TestFileSourceWindowCompletedBytesLoseEligibilityAfterRevocation(t *testing.T) {
	issued, req, obs := fileSourceFixture(t, 200*time.Millisecond)
	var calls atomic.Int32
	auth := fileSourceAuthorityFunc(func(context.Context, client.IssuedDevelopFileAuthorization, client.DevelopFileSourceAccessRequest) (*client.DevelopFileSourceAccessObservation, error) {
		if calls.Add(1) > 1 {
			return nil, errors.New("revoked")
		}
		return &obs, nil
	})
	s, err := newFileSourceReader(t.Context(), auth, sourceWindowData(req, []byte("complete but private")), issued, req)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := io.ReadAll(s); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Receipt(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("EOF cached permission forever")
	}
	if receipt, err := s.Receipt(t.Context()); receipt != nil || err == nil {
		t.Fatal("revoked completed staging still eligible")
	}
	if !s.State().Stopped || s.State().CleanupPending {
		t.Fatal("read closure not recorded", s.State())
	}
}

func TestFileSourceWindowBusinessDeadlineStopsBlockedRead(t *testing.T) {
	issued, req, obs := fileSourceFixture(t, time.Second)
	obs.BusinessDeadline = time.Now().Add(220 * time.Millisecond)
	obs.ExpiresAt = obs.BusinessDeadline
	started := make(chan struct{})
	release := make(chan struct{})
	set, _ := plugin.NewQueryReadSet(req.Source)
	provider := &sourceWindowProvider{open: func(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
		return &sourceWindowHandle{set: set, Reader: sourceWindowReaderFunc(func(p []byte) (int, error) { close(started); <-release; return copy(p, []byte("late")), io.EOF })}, nil
	}}
	s, err := newFileSourceReader(t.Context(), fixedSourceAuthority(obs), provider, issued, req)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	done := make(chan error, 1)
	go func() { _, err := s.Read(make([]byte, 8)); done <- err }()
	<-started
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("business budget restarted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("original business deadline not applied")
	}
	if !s.State().CleanupPending {
		t.Fatal("blocked operation falsely stopped")
	}
	close(release)
	waitSourceStopped(t, s)
}
