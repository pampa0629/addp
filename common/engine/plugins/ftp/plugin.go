// Package ftp implements the admitted read-only FTP file-engine providers.
package ftp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path"
	"strconv"
	"time"

	"github.com/addp/common/engine/internal/ftpclient"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/mimetype"
)

type FTPPlugin struct{}

func init() { plugin.Register(&FTPPlugin{}) }

func (*FTPPlugin) Type() string                         { return "ftp" }
func (*FTPPlugin) DisplayName() string                  { return "FTP / FTPS" }
func (*FTPPlugin) EngineOrigin() string                 { return "general" }
func (p *FTPPlugin) DefaultPort() int                   { return p.ConnectionSpec().DefaultPortValue() }
func (p *FTPPlugin) RequiredFields() []string           { return p.ConnectionSpec().RequiredFields() }
func (p *FTPPlugin) SensitiveFields() []string          { return p.ConnectionSpec().SensitiveFields() }
func (p *FTPPlugin) ConnectionIdentityFields() []string { return p.ConnectionSpec().IdentityFields() }
func (*FTPPlugin) ConnectionSpec() plugin.ConnectionSpec {
	return plugin.NewConnectionSpec(
		plugin.ConnectionFieldSpec{Key: "host", LabelKey: "storageEngine.host", Input: plugin.ConnectionFieldText, Required: true, Identity: true},
		plugin.ConnectionFieldSpec{Key: "port", LabelKey: "storageEngine.port", Input: plugin.ConnectionFieldNumber, Required: true, Identity: true, Default: 21},
		plugin.ConnectionFieldSpec{Key: "user", LabelKey: "storageEngine.username", Input: plugin.ConnectionFieldText, Required: true, Identity: true},
		plugin.ConnectionFieldSpec{Key: "password", LabelKey: "storageEngine.password", Input: plugin.ConnectionFieldPassword, Required: true, Sensitive: true},
		plugin.ConnectionFieldSpec{Key: "security", LabelKey: "storageEngine.securityProtocol", Input: plugin.ConnectionFieldSelect, Required: true, Default: "explicit_tls", Options: []plugin.ConnectionFieldOption{{Value: "explicit_tls", LabelKey: "storageEngine.ftpExplicitTLS"}, {Value: "plain", LabelKey: "storageEngine.ftpPlain"}}},
		plugin.ConnectionFieldSpec{Key: "tls_ca_cert", LabelKey: "storageEngine.tlsCaCertOptional", Input: plugin.ConnectionFieldTextarea, PlaceholderKey: "storageEngine.pemPlaceholder", VisibleWhen: &plugin.ConnectionFieldCondition{Field: "security", Values: []string{"explicit_tls"}}},
	)
}
func (*FTPPlugin) Capabilities() plugin.EngineCapabilities {
	c := plugin.NewFileCapabilities("ftp")
	c.Storage.Store.StreamWrite = false
	c.Storage.Store.Delete = false
	c.Storage.Semantics = []string{"root", "directory", "file", "stream_read", "range_read"}
	c.Storage.NotSupported = []string{"stream_write", "delete"}
	return c
}
func (p *FTPPlugin) StoreSemantics() plugin.StoreSemantics {
	c := p.Capabilities().Storage
	return plugin.StoreSemantics{Semantics: c.Semantics, NotSupported: c.NotSupported}
}
func (*FTPPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.FileCatalogModel()
}
func parseConnectionInfo(info plugin.ConnectionInfo) (ftpclient.Config, error) {
	// Only descriptor fields are accepted; credentials and paths are never inferred.
	values := map[string]string{}
	for key, value := range info {
		field, exists := (&FTPPlugin{}).ConnectionSpec().Field(key)
		if !exists {
			return ftpclient.Config{}, fmt.Errorf("unknown FTP connection field")
		}
		if field.Key == "port" {
			continue
		}
		text, ok := value.(string)
		if !ok {
			return ftpclient.Config{}, fmt.Errorf("FTP connection field requires text")
		}
		values[key] = text
	}
	port := 0
	switch value := info["port"].(type) {
	case int:
		port = value
	case float64:
		if value >= 1 && value <= 65535 && math.Trunc(value) == value {
			port = int(value)
		}
	case json.Number:
		n, err := strconv.ParseInt(string(value), 10, 32)
		if err == nil {
			port = int(n)
		}
	}
	c := ftpclient.Config{Host: values["host"], Port: port, User: values["user"], Password: values["password"], Security: values["security"], CAPEM: values["tls_ca_cert"], Timeout: 60 * time.Second}
	return c, c.Validate()
}
func (*FTPPlugin) ValidateConnectionInfo(info plugin.ConnectionInfo) error {
	_, err := parseConnectionInfo(info)
	return err
}
func (*FTPPlugin) TestConnection(ctx context.Context, info plugin.ConnectionInfo) error {
	c, err := parseConnectionInfo(info)
	if err != nil {
		return err
	}
	s, err := ftpclient.Dial(ctx, c)
	if err != nil {
		return err
	}
	defer s.Close()
	if _, err = s.Resolve(""); err != nil {
		return err
	}
	_, err = s.List("/")
	return err
}
func validateCatalog(p plugin.EngineCatalogPath) error {
	invalid := func() error {
		return plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("FTP requires an exact root/directory/file path"))
	}
	if p.Version != plugin.EngineCatalogPathVersion || len(p.Segments) == 0 || p.Segments[0].Term != plugin.EngineCatalogTermRoot || p.Segments[0].Kind != plugin.EngineCatalogKindRoot {
		return invalid()
	}
	for i, s := range p.Segments[1:] {
		if ftpclient.Name(s.Name) != nil {
			return invalid()
		}
		directory := s.Term == plugin.EngineCatalogTermDirectory && s.Kind == plugin.EngineCatalogKindDirectory
		file := s.Term == plugin.EngineCatalogTermFile && s.Kind == plugin.EngineCatalogKindFile
		if !directory && (!file || i != len(p.Segments)-2) {
			return invalid()
		}
	}
	return nil
}
func fileFacts(relative string, e *ftpclient.Entry) plugin.StorageObjectFacts {
	contentType := mimetype.ByExtension(path.Ext(relative))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return plugin.StorageObjectFacts{Name: path.Base(relative), Path: relative, Size: e.Size, ModifiedAt: e.Time, ContentType: contentType}
}
func catalogEntry(parent plugin.EngineCatalogPath, relative string, e *ftpclient.Entry) plugin.EngineCatalogEntry {
	var entry plugin.EngineCatalogEntry
	if e.Type == "dir" {
		entry = plugin.FileDirectoryCatalogEntry(parent, path.Base(relative), relative)
	} else {
		entry = plugin.FileLeafCatalogEntry(parent, fileFacts(relative, e))
	}
	// Generic filesystem normalization is not a spelling rule for remote names.
	entry.Storage.Path = relative
	return entry
}
func (*FTPPlugin) ListChildren(ctx context.Context, info plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	if err := validateCatalog(parent); err != nil {
		return nil, err
	}
	if parent.Segments[len(parent.Segments)-1].Term == plugin.EngineCatalogTermFile || opts.Offset < 0 || opts.Limit < 0 {
		return nil, fmt.Errorf("invalid FTP listing window or target")
	}
	c, err := parseConnectionInfo(info)
	if err != nil {
		return nil, err
	}
	s, err := ftpclient.Dial(ctx, c)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	queue := []plugin.EngineCatalogPath{parent}
	result := []plugin.EngineCatalogEntry{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if _, err = s.Resolve(current.StringPath()); err != nil {
			return nil, err
		}
		physical, _ := ftpclient.Physical(current.StringPath())
		entries, err := s.List(physical)
		if err != nil {
			return nil, err
		}
		if len(result)+len(entries) > ftpclient.MaxEntries {
			return nil, fmt.Errorf("FTP recursive catalog exceeds budget")
		}
		for _, e := range entries {
			relative := e.Name
			if current.StringPath() != "" {
				relative = current.StringPath() + "/" + e.Name
			}
			entry := catalogEntry(current, relative, e)
			result = append(result, entry)
			if opts.Recursive && entry.Role == plugin.EngineCatalogRoleBranch {
				queue = append(queue, entry.Path)
			}
		}
	}
	if opts.Offset >= len(result) {
		return []plugin.EngineCatalogEntry{}, nil
	}
	result = result[opts.Offset:]
	if opts.Limit > 0 && len(result) > opts.Limit {
		result = result[:opts.Limit]
	}
	return result, nil
}
func (*FTPPlugin) ResolvePath(ctx context.Context, info plugin.ConnectionInfo, target plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	if err := validateCatalog(target); err != nil {
		return nil, err
	}
	c, err := parseConnectionInfo(info)
	if err != nil {
		return nil, err
	}
	s, err := ftpclient.Dial(ctx, c)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	e, err := s.Resolve(target.StringPath())
	if err != nil {
		return nil, err
	}
	last := target.Segments[len(target.Segments)-1]
	if len(target.Segments) == 1 {
		if e.Type != "dir" {
			return nil, fmt.Errorf("FTP root must be a directory")
		}
		entry := plugin.EngineCatalogRootEntry(plugin.FileCatalogModel(), target.EngineID, last.Name)
		return &entry, nil
	}
	if (last.Term == plugin.EngineCatalogTermFile) != (e.Type == "file") {
		return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("FTP live kind differs from catalog path"))
	}
	parent := target
	parent.Segments = target.Segments[:len(target.Segments)-1]
	entry := catalogEntry(parent, target.StringPath(), e)
	return &entry, nil
}
func (p *FTPPlugin) DescribeEngineCatalogFacts(ctx context.Context, info plugin.ConnectionInfo, target plugin.EngineCatalogPath, _ plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	if err := validateCatalog(target); err != nil {
		return nil, err
	}
	if target.Segments[len(target.Segments)-1].Term != plugin.EngineCatalogTermFile {
		return nil, fmt.Errorf("FTP facts require file")
	}
	entry, err := p.ResolvePath(ctx, info, target)
	if err != nil {
		return nil, err
	}
	entry.Storage.Name = entry.Name
	entry.Storage.Extension = path.Ext(entry.Name)
	return &plugin.EngineCatalogFacts{Path: entry.Path, Kind: entry.Kind, Storage: entry.Storage, UpdatedAt: entry.UpdatedAt}, nil
}
func (*FTPPlugin) OpenContent(ctx context.Context, info plugin.ConnectionInfo, target plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if err := validateCatalog(target); err != nil {
		return nil, err
	}
	if target.Segments[len(target.Segments)-1].Term != plugin.EngineCatalogTermFile {
		return nil, fmt.Errorf("FTP content requires file")
	}
	c, err := parseConnectionInfo(info)
	if err != nil {
		return nil, err
	}
	return ftpclient.Open(ctx, c, target.StringPath(), opts.Offset, opts.Length)
}
func (p *FTPPlugin) OpenRange(ctx context.Context, info plugin.ConnectionInfo, target plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if opts.Length <= 0 {
		return nil, fmt.Errorf("FTP range requires positive length")
	}
	return p.OpenContent(ctx, info, target, opts)
}

var _ plugin.EnginePlugin = (*FTPPlugin)(nil)
var _ plugin.EngineCatalogProvider = (*FTPPlugin)(nil)
var _ plugin.EngineCatalogFactsProvider = (*FTPPlugin)(nil)
var _ plugin.ContentReadableProvider = (*FTPPlugin)(nil)
var _ plugin.RangeReadableProvider = (*FTPPlugin)(nil)
