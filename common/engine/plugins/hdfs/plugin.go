package hdfs

import (
	"context"
	"fmt"
	"io"
	"mime"
	"path"
	"time"

	"github.com/addp/common/engine/plugin"
)

type HDFSPlugin struct{}

func init()                                              { plugin.Register(&HDFSPlugin{}) }
func (*HDFSPlugin) Type() string                         { return "hdfs" }
func (*HDFSPlugin) DisplayName() string                  { return "HDFS" }
func (*HDFSPlugin) EngineOrigin() string                 { return "general" }
func (p *HDFSPlugin) DefaultPort() int                   { return p.ConnectionSpec().DefaultPortValue() }
func (p *HDFSPlugin) RequiredFields() []string           { return p.ConnectionSpec().RequiredFields() }
func (p *HDFSPlugin) SensitiveFields() []string          { return p.ConnectionSpec().SensitiveFields() }
func (p *HDFSPlugin) ConnectionIdentityFields() []string { return p.ConnectionSpec().IdentityFields() }
func (*HDFSPlugin) ConnectionSpec() plugin.ConnectionSpec {
	return plugin.NewConnectionSpec(
		plugin.ConnectionFieldSpec{Key: "webhdfs_endpoint", LabelKey: "storageEngine.hdfsWebEndpoint", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Placeholder: "http://namenode:9870"},
		plugin.ConnectionFieldSpec{Key: "rpc_uri", LabelKey: "storageEngine.hdfsRpcUri", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Placeholder: "hdfs://namenode:8020"},
		plugin.ConnectionFieldSpec{Key: "root_path", LabelKey: "storageEngine.hdfsRootPath", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Default: "/addp", Placeholder: "/addp"},
		plugin.ConnectionFieldSpec{Key: "authentication", LabelKey: "storageEngine.hdfsAuthentication", Input: plugin.ConnectionFieldSelect, Required: true, Default: "simple", HintKey: "storageEngine.hints.hdfsSimple", Options: []plugin.ConnectionFieldOption{{Value: "simple", Label: "Simple"}}},
		plugin.ConnectionFieldSpec{Key: "user", LabelKey: "storageEngine.username", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Default: "addp_business_reader"},
	)
}
func (p *HDFSPlugin) Capabilities() plugin.EngineCapabilities {
	c := plugin.NewFileCapabilities(p.Type())
	c.Storage.Store.StreamWrite, c.Storage.Store.Delete = false, false
	c.Storage.Semantics = []string{"root", "directory", "file", "stream_read", "range_read"}
	c.Storage.NotSupported = []string{"stream_write", "delete"}
	return c
}
func (p *HDFSPlugin) StoreSemantics() plugin.StoreSemantics {
	c := p.Capabilities().Storage
	return plugin.StoreSemantics{Semantics: c.Semantics, NotSupported: c.NotSupported}
}
func (*HDFSPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.FileCatalogModel()
}
func (*HDFSPlugin) ValidateConnectionInfo(info plugin.ConnectionInfo) error {
	_, err := ParseConnectionInfo(info)
	return err
}
func (*HDFSPlugin) TestConnection(ctx context.Context, info plugin.ConnectionInfo) error {
	c, err := newClient(info)
	if err != nil {
		return err
	}
	_, err = c.list(ctx, "")
	return err
}

func facts(relative string, s fileStatus) plugin.StorageObjectFacts {
	contentType := mime.TypeByExtension(path.Ext(relative))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return plugin.StorageObjectFacts{Name: path.Base(relative), Path: relative, Size: s.Length, ModifiedAt: time.UnixMilli(s.Modified), ContentType: contentType}
}

func entry(parent plugin.EngineCatalogPath, relative string, s fileStatus) plugin.EngineCatalogEntry {
	var e plugin.EngineCatalogEntry
	if s.Type == "DIRECTORY" {
		e = plugin.FileDirectoryCatalogEntry(parent, s.Name, relative)
	} else {
		e = plugin.FileLeafCatalogEntry(parent, facts(relative, s))
	}
	// Preserve original names; filesystem protocol names are not normalized spellings.
	e.Storage.Path = relative
	return e
}

func (*HDFSPlugin) ListChildren(ctx context.Context, info plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	if err := validateCatalog(parent); err != nil {
		return nil, err
	}
	if parent.Segments[len(parent.Segments)-1].Term == plugin.EngineCatalogTermFile {
		return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("HDFS listing requires a branch"))
	}
	if opts.Offset < 0 || opts.Limit < 0 {
		return nil, fmt.Errorf("invalid HDFS listing window")
	}
	c, err := newClient(info)
	if err != nil {
		return nil, err
	}
	queue := []plugin.EngineCatalogPath{parent}
	var result []plugin.EngineCatalogEntry
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		statuses, err := c.list(ctx, current.StringPath())
		if err != nil {
			return nil, err
		}
		if len(result)+len(statuses) > maxCatalogEntries {
			return nil, fmt.Errorf("HDFS recursive catalog exceeds entry budget")
		}
		for _, s := range statuses {
			relative := s.Name
			if current.StringPath() != "" {
				relative = current.StringPath() + "/" + s.Name
			}
			e := entry(current, relative, s)
			result = append(result, e)
			if opts.Recursive && e.Role == plugin.EngineCatalogRoleBranch {
				queue = append(queue, e.Path)
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

func (*HDFSPlugin) ResolvePath(ctx context.Context, info plugin.ConnectionInfo, p plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	if err := validateCatalog(p); err != nil {
		return nil, err
	}
	c, err := newClient(info)
	if err != nil {
		return nil, err
	}
	s, err := c.stat(ctx, p.StringPath())
	if err != nil {
		return nil, err
	}
	last := p.Segments[len(p.Segments)-1]
	if len(p.Segments) == 1 {
		if s.Type != "DIRECTORY" {
			return nil, fmt.Errorf("HDFS management root must be a directory")
		}
		e := plugin.EngineCatalogRootEntry(plugin.FileCatalogModel(), p.EngineID, last.Name)
		return &e, nil
	}
	if (last.Term == plugin.EngineCatalogTermFile) != (s.Type == "FILE") {
		return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("HDFS catalog kind differs from live resource"))
	}
	parent := p
	parent.Segments = p.Segments[:len(p.Segments)-1]
	s.Name = last.Name
	e := entry(parent, p.StringPath(), *s)
	return &e, nil
}

func (p *HDFSPlugin) DescribeEngineCatalogFacts(ctx context.Context, info plugin.ConnectionInfo, target plugin.EngineCatalogPath, _ plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	if len(target.Segments) < 2 || target.Segments[len(target.Segments)-1].Term != plugin.EngineCatalogTermFile {
		return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("HDFS facts require a file"))
	}
	e, err := p.ResolvePath(ctx, info, target)
	if err != nil {
		return nil, err
	}
	e.Storage.Name, e.Storage.Extension = e.Name, path.Ext(e.Name)
	return &plugin.EngineCatalogFacts{Path: e.Path, Kind: e.Kind, Storage: e.Storage, UpdatedAt: e.UpdatedAt}, nil
}

func (p *HDFSPlugin) OpenContent(ctx context.Context, info plugin.ConnectionInfo, target plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if opts.Offset < 0 || opts.Length < 0 {
		return nil, fmt.Errorf("invalid HDFS read range")
	}
	f, err := p.DescribeEngineCatalogFacts(ctx, info, target, plugin.EngineCatalogFactsOptions{})
	if err != nil {
		return nil, err
	}
	c, err := newClient(info)
	if err != nil {
		return nil, err
	}
	remaining := *f.Storage.SizeBytes - opts.Offset
	if remaining < 0 {
		remaining = 0
	}
	if opts.Length > 0 && opts.Length < remaining {
		remaining = opts.Length
	}
	return c.open(ctx, target.StringPath(), opts.Offset, remaining)
}
func (p *HDFSPlugin) OpenRange(ctx context.Context, info plugin.ConnectionInfo, target plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if opts.Length <= 0 {
		return nil, fmt.Errorf("HDFS range read requires positive length")
	}
	return p.OpenContent(ctx, info, target, opts)
}

var _ plugin.EngineCatalogProvider = (*HDFSPlugin)(nil)
var _ plugin.EngineCatalogFactsProvider = (*HDFSPlugin)(nil)
var _ plugin.ContentReadableProvider = (*HDFSPlugin)(nil)
var _ plugin.RangeReadableProvider = (*HDFSPlugin)(nil)
