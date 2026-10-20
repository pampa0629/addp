package ftp

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func ownedInfo(t *testing.T, security string) plugin.ConnectionInfo {
	t.Helper()
	if os.Getenv("ADDP_FTP_INTEGRATION") != "1" {
		t.Skip("requires make test-common-ftp")
	}
	b, err := os.ReadFile(filepath.Join(os.Getenv("ADDP_FTP_ADMISSION_DIR"), "connection.json"))
	if err != nil {
		t.Fatal("private connection unavailable")
	}
	var c struct {
		Host, User, Password, CA string
		Port                     int
	}
	if json.Unmarshal(b, &c) != nil {
		t.Fatal("invalid private connection")
	}
	info := plugin.ConnectionInfo{"host": c.Host, "port": c.Port, "user": c.User, "password": c.Password, "root_path": "/examples", "security": security}
	if security == "explicit_tls" {
		ca, err := os.ReadFile(c.CA)
		if err != nil {
			t.Fatal(err)
		}
		info["tls_ca_cert"] = string(ca)
	}
	return info
}
func TestIntegrationProviderCatalogFactsAndContent(t *testing.T) {
	for _, mode := range []string{"plain", "explicit_tls"} {
		t.Run(mode, func(t *testing.T) {
			info := ownedInfo(t, mode)
			p := &FTPPlugin{}
			root := plugin.FileRootPath(99)
			if err := p.TestConnection(t.Context(), info); err != nil {
				t.Fatal(err)
			}
			entries, err := p.ListChildren(t.Context(), info, root, plugin.ListOptions{})
			if err != nil || len(entries) != 7 {
				t.Fatal("incomplete provider directory", err)
			}
			for _, entry := range entries {
				if entry.Path.StringPath() != entry.Name || entry.Storage.Path != entry.Name {
					t.Fatal("physical management root leaked or name normalized")
				}
				facts, err := p.DescribeEngineCatalogFacts(t.Context(), info, entry.Path, plugin.EngineCatalogFactsOptions{})
				if err != nil || facts.Storage.SizeBytes == nil || facts.Storage.ETag != "" {
					t.Fatal("invalid native file facts", err)
				}
				if facts.Storage.Name != entry.Name || facts.UpdatedAt == nil {
					t.Fatal("file identity/time lost")
				}
				if entry.Name == "range.bin" {
					r, err := p.OpenRange(t.Context(), info, entry.Path, plugin.ReadOptions{Offset: 193, Length: 513})
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(r)
					r.Close()
					want := make([]byte, 513)
					for i := range want {
						want[i] = byte((193 + i) % 256)
					}
					if err != nil || !bytes.Equal(data, want) {
						t.Fatal("provider REST range failed", err)
					}
				} else {
					r, err := p.OpenContent(t.Context(), info, entry.Path, plugin.ReadOptions{})
					if err != nil {
						t.Fatal(err)
					}
					body, err := io.ReadAll(r)
					r.Close()
					if err != nil || int64(len(body)) != *facts.Storage.SizeBytes {
						t.Fatal("full download differs from native facts", err)
					}
				}
			}
			page, err := p.ListChildren(t.Context(), info, root, plugin.ListOptions{Offset: 1, Limit: 2})
			if err != nil || len(page) != 2 || page[0].Name != entries[1].Name {
				t.Fatal("provider listing window failed", err)
			}
			if _, err = p.OpenContent(t.Context(), info, root, plugin.ReadOptions{}); err == nil {
				t.Fatal("directory opened as file")
			}
		})
	}
}
