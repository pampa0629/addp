package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
)

type directoryIOFixture struct {
	*storageModelIO
	lists, closes     int
	dirs              map[string]bool
	modified          map[string]time.Time
	afterOpen         func(string)
	listOverride      func([]plugin.EngineCatalogEntry) []plugin.EngineCatalogEntry
	readErr, closeErr error
	short             bool
}

func (p *directoryIOFixture) ResolvePath(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return nil, errors.New("unexpected ResolvePath")
}
func (p *directoryIOFixture) ListChildren(_ context.Context, info plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	p.lists++
	if info["identity"] != "same-version" || opts.Recursive {
		return nil, errors.New("lost request source or per-directory checks")
	}
	var entries []plugin.EngineCatalogEntry
	for name := range p.dirs {
		if path.Dir(name) == parent.StringPath() {
			entries = append(entries, plugin.FileDirectoryCatalogEntry(parent, path.Base(name), name))
		}
	}
	for name, body := range p.files {
		if path.Dir(name) != parent.StringPath() {
			continue
		}
		modified := p.modified[name]
		if modified.IsZero() {
			modified = time.Unix(1234567890, 0)
		}
		entries = append(entries, plugin.FileLeafCatalogEntry(parent, plugin.StorageObjectFacts{Name: path.Base(name), Path: name, Size: int64(len(body)), ModifiedAt: modified}))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name > entries[j].Name })
	if p.listOverride != nil {
		entries = p.listOverride(entries)
	}
	return entries, nil
}

type directoryFixtureCloser struct {
	io.Reader
	close func() error
}

func (r directoryFixtureCloser) Close() error { return r.close() }

type directoryFailedReader struct{ err error }

func (r directoryFailedReader) Read([]byte) (int, error) { return 0, r.err }
func (p *directoryIOFixture) OpenContent(ctx context.Context, info plugin.ConnectionInfo, target plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	r, err := p.storageModelIO.OpenContent(ctx, info, target, opts)
	if err != nil {
		return nil, err
	}
	var input io.Reader = r
	if p.readErr != nil {
		input = directoryFailedReader{p.readErr}
	}
	if p.short {
		input = io.LimitReader(r, 1)
	}
	if p.afterOpen != nil {
		p.afterOpen(target.StringPath())
	}
	return directoryFixtureCloser{Reader: input, close: func() error { p.closes++; return errors.Join(r.Close(), p.closeErr) }}, nil
}

func directoryFixture(t *testing.T) (*MetadataService, *StorageDataItem, *directoryIOFixture, *uint, *models.DownloadPlan) {
	t.Helper()
	svc, target, base, tenant := storageWholeFixture(t)
	root := target.item.FullName
	target.StorageRef = root
	p := &directoryIOFixture{storageModelIO: base, dirs: map[string]bool{root + "/空 目录": true, root + "/nested": true}, modified: map[string]time.Time{}}
	p.files[root+"/nested/ 原名 %.bin"] = "nested bytes"
	plugin.Register(p)
	plan, err := svc.resolveStorageDownloadPlan(t.Context(), target.EngineID, "nfs", root, target.item, tenant)
	if err != nil {
		t.Fatal(err)
	}
	return svc, target, p, tenant, plan
}

func TestDirectoryDownloadZIPPreservesWholeRootNamesBytesAndClosesUnnamedFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	svc, target, p, tenant, plan := directoryFixture(t)
	checks := 0
	svc.SetWholeSourceReadChecker(storageWholeCheck(func(_ context.Context, token string, targets []plugin.EngineCatalogPath) error {
		checks++
		if token != "addp_at_current_user" || !reflect.DeepEqual(targets, []plugin.EngineCatalogPath{*plan.DirectoryRoot}) {
			t.Fatal("lost whole authorization")
		}
		return nil
	}))
	r, err := svc.OpenStorageDownloadPlan(t.Context(), target, plan, tenant, "addp_at_current_user", true)
	if err != nil {
		t.Fatal(err)
	}
	prepared := r.(*preparedDirectoryArchive)
	if _, err := os.Stat(prepared.file.Name()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temporary ZIP still has a pathname")
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(raw)) != prepared.Size() {
		t.Fatal("archive size differs")
	}
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, entry := range z.File {
		seen[entry.Name] = true
		if entry.FileInfo().IsDir() {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		reader.Close()
		ref := "models/" + entry.Name
		if err != nil || string(body) != p.files[ref] {
			t.Fatalf("ZIP changed %q", entry.Name)
		}
	}
	if len(z.File) != len(p.files)+len(p.dirs)+1 || !seen["sample.gdb/"] || !seen["sample.gdb/空 目录/"] || !seen["sample.gdb/nested/ 原名 %.bin"] {
		t.Fatalf("missing directory or member: %v", seen)
	}
	if p.lists != 6 || p.opens != len(p.files) || p.closes != p.opens || checks != p.lists+2*p.opens+1 {
		t.Fatalf("checks=%d lists=%d opens=%d closes=%d", checks, p.lists, p.opens, p.closes)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("archive descriptor remained open")
	}
}

func TestDirectoryDownloadFailsClosedAndReclaimsAllTemporaryIO(t *testing.T) {
	for _, scenario := range []string{"no grant", "revoke before next read", "expiry before next read", "cancel", "read failure", "close failure", "member added", "member size changed", "member time changed", "short read", "outside path", "duplicate", "wrong root", "final denial"} {
		t.Run(scenario, func(t *testing.T) {
			temp := t.TempDir()
			t.Setenv("TMPDIR", temp)
			svc, target, p, tenant, plan := directoryFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			denied := scenario == "no grant"
			svc.SetWholeSourceReadChecker(storageWholeCheck(func(context.Context, string, []plugin.EngineCatalogPath) error {
				if denied {
					return commonClient.ErrManagerPreviewReadDenied
				}
				return nil
			}))
			p.afterOpen = func(name string) {
				switch scenario {
				case "revoke before next read", "expiry before next read":
					denied = true
				case "cancel":
					cancel()
				case "member added":
					p.files[target.StorageRef+"/added-during-download.gdbtable"] = "new member"
				case "member size changed":
					p.files[name] += "changed"
				case "member time changed":
					p.modified[name] = time.Now()
				}
			}
			switch scenario {
			case "read failure":
				p.readErr = io.ErrClosedPipe
			case "close failure":
				p.closeErr = io.ErrClosedPipe
			case "short read":
				p.short = true
			case "wrong root":
				plan.DirectoryRoot = pointerCatalogPath(plugin.FileDirectoryPath(9, "models/other.gdb"))
			case "outside path":
				p.listOverride = func(entries []plugin.EngineCatalogEntry) []plugin.EngineCatalogEntry {
					entries[0].Path = plugin.FileItemPath(9, "outside.bin")
					return entries
				}
			case "duplicate":
				p.listOverride = func(entries []plugin.EngineCatalogEntry) []plugin.EngineCatalogEntry {
					return append(entries, entries[0])
				}
			case "final denial":
				p.listOverride = func(entries []plugin.EngineCatalogEntry) []plugin.EngineCatalogEntry {
					if p.lists == 6 {
						denied = true
					}
					return entries
				}
			}
			r, err := svc.OpenStorageDownloadPlan(ctx, target, plan, tenant, "addp_at_current_user", true)
			if r != nil || err == nil {
				if r != nil {
					r.Close()
				}
				t.Fatalf("failed download delivered a package: %v", err)
			}
			if strings.HasPrefix(scenario, "member ") || scenario == "short read" {
				if !errors.Is(err, ErrDownloadSourceChanged) {
					t.Fatalf("source mutation was not reported: %v", err)
				}
			}
			if scenario == "no grant" && (p.lists != 0 || p.stats != 0 || p.opens != 0) {
				t.Fatal("ungranted source I/O")
			}
			if strings.Contains(scenario, "before next read") && p.opens != 1 {
				t.Fatal("source read continued after withdrawal")
			}
			if p.closes != p.opens {
				t.Fatalf("leaked source readers: %d/%d", p.closes, p.opens)
			}
			entries, readErr := os.ReadDir(temp)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("temporary package remained: %v %v", entries, readErr)
			}
		})
	}
}
