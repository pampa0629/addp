package nfs

import (
	"context"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"sync"

	"github.com/addp/common/engine/plugin"
)

// NFSPlugin NFS 文件系统存储引擎插件
type NFSPlugin struct{}

type limitedReadCloser struct {
	io.Reader
	closer io.Closer
}

func (r *limitedReadCloser) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

type nfsFileReadCloser struct {
	io.ReadCloser
	mu   *sync.Mutex
	size int64
}

func (r *nfsFileReadCloser) Read(p []byte) (int, error) {
	if r == nil || r.ReadCloser == nil {
		return 0, io.ErrClosedPipe
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ReadCloser.Read(p)
}

func (r *nfsFileReadCloser) Close() error {
	if r == nil || r.ReadCloser == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ReadCloser.Close()
}

func (r *nfsFileReadCloser) Seek(offset int64, whence int) (int64, error) {
	seeker, ok := r.ReadCloser.(io.Seeker)
	if !ok {
		return 0, fmt.Errorf("NFS file reader does not support seek")
	}
	target := offset
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		r.mu.Lock()
		current, err := seeker.Seek(0, io.SeekCurrent)
		r.mu.Unlock()
		if err != nil {
			return 0, err
		}
		target = current + offset
	case io.SeekEnd:
		if r.size < 0 {
			return 0, fmt.Errorf("NFS file size is unavailable")
		}
		target = r.size + offset
	default:
		return 0, fmt.Errorf("invalid seek whence: %d", whence)
	}
	if target < 0 {
		return 0, fmt.Errorf("negative seek offset: %d", target)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return seeker.Seek(target, io.SeekStart)
}

type lockedWriteCloser struct {
	io.WriteCloser
	mu *sync.Mutex
}

func (w *lockedWriteCloser) Write(p []byte) (int, error) {
	if w == nil || w.WriteCloser == nil {
		return 0, io.ErrClosedPipe
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.WriteCloser.Write(p)
}

func (w *lockedWriteCloser) Close() error {
	if w == nil || w.WriteCloser == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.WriteCloser.Close()
}

func init() {
	plugin.Register(&NFSPlugin{})
}

func (p *NFSPlugin) Type() string         { return "nfs" }
func (p *NFSPlugin) DisplayName() string  { return "NFS 文件系统" }
func (p *NFSPlugin) EngineOrigin() string { return "general" }
func (p *NFSPlugin) DefaultPort() int     { return 2049 }

func (p *NFSPlugin) RequiredFields() []string {
	return []string{"server", "export_path"}
}

func (p *NFSPlugin) SensitiveFields() []string {
	return []string{} // NFS 基于 IP 访问控制，无密钥
}

func (p *NFSPlugin) ConnectionIdentityFields() []string {
	return []string{"server", "export_path"}
}

func (p *NFSPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewFileCapabilities(p.Type())
}

func (p *NFSPlugin) StoreSemantics() plugin.StoreSemantics {
	capabilities := p.Capabilities()
	return plugin.StoreSemantics{
		Semantics:    capabilities.Storage.Semantics,
		NotSupported: capabilities.Storage.NotSupported,
	}
}

func (p *NFSPlugin) CatalogModel() plugin.CatalogModelSpec {
	return plugin.FileCatalogModel()
}

func (p *NFSPlugin) fileCatalogCallbacks() plugin.FileCatalogCallbacks {
	return plugin.FileCatalogCallbacks{
		ListDirectoryFunc:       p.listDirectory,
		GetFileStorageFactsFunc: p.getStorageObjectFacts,
	}
}

func (p *NFSPlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	return plugin.ListFileCatalogChildren(ctx, p.fileCatalogCallbacks(), connInfo, parent.EngineID, parent, opts)
}

func (p *NFSPlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogEntry, error) {
	return plugin.ResolveFileCatalogPath(ctx, p.fileCatalogCallbacks(), connInfo, path.EngineID, path)
}

func (p *NFSPlugin) DescribeCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	return plugin.DescribeFileCatalogFacts(ctx, p.fileCatalogCallbacks(), connInfo, path.EngineID, path)
}

func (p *NFSPlugin) OpenContent(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	filePath, err := plugin.RequireFileLeafPath(path)
	if err != nil {
		return nil, err
	}
	return p.readFile(ctx, connInfo, filePath)
}

func (p *NFSPlugin) OpenRange(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if opts.Offset < 0 {
		return nil, fmt.Errorf("range read offset cannot be negative")
	}
	if opts.Length <= 0 {
		return nil, fmt.Errorf("range read requires positive length")
	}
	filePath, err := plugin.RequireFileLeafPath(path)
	if err != nil {
		return nil, err
	}
	return p.readFileRange(ctx, connInfo, filePath, opts)
}

func (p *NFSPlugin) CreateContent(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.WriteOptions) (io.WriteCloser, error) {
	filePath, err := plugin.RequireFileLeafPath(path)
	if err != nil {
		return nil, err
	}
	return p.openFileForWrite(ctx, connInfo, filePath, opts.Overwrite)
}

func (p *NFSPlugin) DeleteResource(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) error {
	filePath, err := plugin.RequireFileLeafPath(path)
	if err != nil {
		return err
	}
	return p.deleteFile(ctx, connInfo, filePath)
}

func (p *NFSPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

// TestConnection 通过 listRoots 验证连通性
func (p *NFSPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return err
	}
	_, err = getOrCreateMount(server, exportPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return fmt.Errorf("NFS connection failed: %w", err)
	}
	return err
}

// listDirectory 列出目录内容（非递归）
func (p *NFSPlugin) listDirectory(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath) ([]plugin.CatalogEntry, error) {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return nil, err
	}

	entry, err := getOrCreateMount(server, exportPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("NFS connection failed: %w", err)
	}

	// 规范化路径
	dirPath := normalizePath(parent.StringPath())

	entry.mu.Lock()
	entries, err := entry.target.ReadDirPlus(dirPath)
	entry.mu.Unlock()
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("failed to list directory %s: %w", dirPath, err)
	}

	var nodes []plugin.CatalogEntry

	for _, entry := range entries {
		name := entry.FileName
		if name == "." || name == ".." {
			continue
		}
		fullPath := joinPath(dirPath, name)

		if entry.IsDir() {
			nodes = append(nodes, plugin.FileDirectoryCatalogEntry(parent, name, fullPath+"/"))
		} else {
			nodes = append(nodes, plugin.FileLeafCatalogEntry(parent, plugin.StorageObjectFacts{
				Name:        name,
				Path:        fullPath,
				Size:        entry.Size(),
				ModifiedAt:  entry.ModTime(),
				ContentType: inferContentType(name),
			}))
		}
	}

	return nodes, nil
}

// readFile 流式读取文件内容
func (p *NFSPlugin) readFile(ctx context.Context, connInfo plugin.ConnectionInfo, path string) (io.ReadCloser, error) {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return nil, err
	}

	entry, err := getOrCreateMount(server, exportPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("NFS connection failed: %w", err)
	}

	filePath := normalizePath(path)
	entry.mu.Lock()
	info, _, statErr := entry.target.Lookup(filePath)
	rc, err := entry.target.Open(filePath)
	entry.mu.Unlock()
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("failed to open NFS file %s: %w", filePath, err)
	}
	size := int64(-1)
	if statErr == nil {
		size = info.Size()
	}
	return &nfsFileReadCloser{ReadCloser: rc, mu: &entry.mu, size: size}, nil
}

func (p *NFSPlugin) readFileRange(ctx context.Context, connInfo plugin.ConnectionInfo, path string, opts plugin.ReadOptions) (io.ReadCloser, error) {
	rc, err := p.readFile(ctx, connInfo, path)
	if err != nil {
		return nil, err
	}
	seeker, ok := rc.(io.Seeker)
	if !ok {
		_ = rc.Close()
		return nil, fmt.Errorf("NFS file reader does not support seek")
	}
	if _, err := seeker.Seek(opts.Offset, io.SeekStart); err != nil {
		_ = rc.Close()
		return nil, fmt.Errorf("failed to seek NFS file %s to offset %d: %w", normalizePath(path), opts.Offset, err)
	}
	return &limitedReadCloser{
		Reader: io.LimitReader(rc, opts.Length),
		closer: rc,
	}, nil
}

// getStorageObjectFacts 获取存储对象事实
func (p *NFSPlugin) getStorageObjectFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path string) (*plugin.StorageObjectFacts, error) {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return nil, err
	}

	entry, err := getOrCreateMount(server, exportPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("NFS connection failed: %w", err)
	}

	filePath := normalizePath(path)
	entry.mu.Lock()
	info, _, err := entry.target.Lookup(filePath)
	entry.mu.Unlock()
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("failed to stat NFS file %s: %w", filePath, err)
	}

	return &plugin.StorageObjectFacts{
		Name:        filepath.Base(filePath),
		Path:        path,
		Size:        info.Size(),
		ModifiedAt:  info.ModTime(),
		ContentType: inferContentType(filePath),
	}, nil
}

// === 写入方法（NFS 插件专有）===

// OpenFileForWrite 打开 NFS 文件用于写入（文件不存在则创建，存在则覆盖）
func (p *NFSPlugin) OpenFileForWrite(ctx context.Context, connInfo plugin.ConnectionInfo, path string) (io.WriteCloser, error) {
	return p.openFileForWrite(ctx, connInfo, path, true)
}

func (p *NFSPlugin) openFileForWrite(ctx context.Context, connInfo plugin.ConnectionInfo, path string, overwrite bool) (io.WriteCloser, error) {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return nil, err
	}

	entry, err := getOrCreateMount(server, exportPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("NFS connection failed: %w", err)
	}

	filePath := normalizePath(path)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	parentDir := filepath.Dir(filePath)
	if parentDir != "." && parentDir != "/" {
		if err := mkdirAllLocked(entry, parentDir); err != nil {
			invalidatePool(server, exportPath)
			return nil, err
		}
	}
	if overwrite {
		// go-nfs-client 的 OpenFile 不会自动截断，覆盖写必须先删除旧文件。
		_ = entry.target.Remove(filePath)
	} else if _, _, err := entry.target.Lookup(filePath); err == nil {
		return nil, fmt.Errorf("NFS file %s already exists", filePath)
	}

	f, err := entry.target.OpenFile(filePath, 0644)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("failed to open NFS file for write %s: %w", filePath, err)
	}
	return &lockedWriteCloser{WriteCloser: f, mu: &entry.mu}, nil
}

func (p *NFSPlugin) deleteFile(ctx context.Context, connInfo plugin.ConnectionInfo, path string) error {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return err
	}
	entry, err := getOrCreateMount(server, exportPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return fmt.Errorf("NFS connection failed: %w", err)
	}
	filePath := normalizePath(path)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := entry.target.Remove(filePath); err != nil {
		if _, _, lookupErr := entry.target.Lookup(filePath); lookupErr != nil {
			return nil
		}
		invalidatePool(server, exportPath)
		return fmt.Errorf("failed to delete NFS file %s: %w", filePath, err)
	}
	return nil
}

func mkdirAllLocked(entry *poolEntry, path string) error {
	dirPath := normalizePath(path)
	parts := strings.Split(strings.TrimPrefix(dirPath, "/"), "/")
	current := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		current = current + "/" + part
		if _, err := entry.target.Mkdir(current, 0755); err != nil {
			if _, _, lookupErr := entry.target.Lookup(current); lookupErr == nil {
				continue
			}
			return fmt.Errorf("failed to create NFS directory %s: %w", current, err)
		}
	}
	return nil
}

// MkdirAll 在 NFS 上递归创建目录
func (p *NFSPlugin) MkdirAll(ctx context.Context, connInfo plugin.ConnectionInfo, path string) error {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return err
	}

	entry, err := getOrCreateMount(server, exportPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return fmt.Errorf("NFS connection failed: %w", err)
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	return mkdirAllLocked(entry, path)
}

// WriteFile 将 reader 内容写入 NFS 文件
func (p *NFSPlugin) WriteFile(ctx context.Context, connInfo plugin.ConnectionInfo, path string, r io.Reader) error {
	f, err := p.OpenFileForWrite(ctx, connInfo, path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

// === 辅助方法 ===

func (p *NFSPlugin) parseConnInfo(connInfo plugin.ConnectionInfo) (server, exportPath string, err error) {
	server = plugin.GetString(connInfo, "server")
	exportPath = plugin.GetString(connInfo, "export_path")
	if server == "" || exportPath == "" {
		return "", "", fmt.Errorf("missing required fields: server, export_path")
	}
	// 规范化 localhost
	server = plugin.NormalizeHost(server)
	return server, exportPath, nil
}

// normalizePath 确保路径以 / 开头
func normalizePath(path string) string {
	path = plugin.NormalizeFileCatalogPath(path)
	if path == "" {
		return "/"
	}
	return "/" + path
}

// joinPath 拼接路径
func joinPath(dir, name string) string {
	if dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}

// inferContentType 根据文件名推断 MIME 类型
func inferContentType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if mimeType := mime.TypeByExtension(ext); mimeType != "" {
		return mimeType
	}
	customTypes := map[string]string{
		".geojson": "application/geo+json",
		".shp":     "application/x-shapefile",
		".shx":     "application/x-shapefile",
		".dbf":     "application/x-dbf",
		".prj":     "application/x-shapefile-prj",
		".parquet": "application/x-parquet",
		".kml":     "application/vnd.google-earth.kml+xml",
		".gpx":     "application/gpx+xml",
		".tif":     "image/tiff",
		".tiff":    "image/tiff",
	}
	if mimeType, ok := customTypes[ext]; ok {
		return mimeType
	}
	return "application/octet-stream"
}
