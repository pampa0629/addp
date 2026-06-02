package objectstore

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	miniogo "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type ClientConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

func ParseClientConfig(connInfo plugin.ConnectionInfo, defaultSSL bool, normalizeEndpoint bool) (ClientConfig, error) {
	cfg := ClientConfig{
		Endpoint:  plugin.GetString(connInfo, "endpoint"),
		AccessKey: plugin.GetString(connInfo, "access_key"),
		SecretKey: plugin.GetString(connInfo, "secret_key"),
		UseSSL:    defaultSSL,
	}
	cfg.Endpoint, cfg.UseSSL = ParseEndpoint(cfg.Endpoint, cfg.UseSSL)
	if _, ok := connInfo["use_ssl"]; ok {
		cfg.UseSSL = plugin.GetBool(connInfo, "use_ssl")
	}
	if normalizeEndpoint {
		cfg.Endpoint = NormalizeEndpoint(cfg.Endpoint)
	}
	if cfg.Endpoint == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return ClientConfig{}, fmt.Errorf("missing required fields: endpoint, access_key, secret_key")
	}
	return cfg, nil
}

func ParseEndpoint(endpoint string, defaultSSL bool) (string, bool) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", defaultSSL
	}

	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" {
		return strings.TrimPrefix(endpoint, "//"), defaultSSL
	}

	useSSL := defaultSSL
	switch parsed.Scheme {
	case "http":
		useSSL = false
	case "https":
		useSSL = true
	default:
		return strings.TrimPrefix(endpoint, "//"), defaultSSL
	}

	host := parsed.Host
	if host == "" {
		host = strings.TrimPrefix(parsed.Path, "//")
	}
	return strings.Trim(host, "/"), useSSL
}

func NormalizeEndpoint(endpoint string) string {
	if endpoint == "" {
		return ""
	}

	hostPart := endpoint
	portPart := ""
	for i := len(endpoint) - 1; i >= 0; i-- {
		if endpoint[i] == ':' {
			hostPart = endpoint[:i]
			portPart = endpoint[i:]
			break
		}
	}

	if hostPart == "localhost" || hostPart == "127.0.0.1" {
		return plugin.NormalizeHost(hostPart) + portPart
	}
	return endpoint
}

func NewClient(connInfo plugin.ConnectionInfo, defaultSSL bool, normalizeEndpoint bool) (*miniogo.Client, error) {
	cfg, err := ParseClientConfig(connInfo, defaultSSL, normalizeEndpoint)
	if err != nil {
		return nil, err
	}
	return miniogo.New(cfg.Endpoint, &miniogo.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
}

func TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo, defaultSSL bool, normalizeEndpoint bool) error {
	client, err := NewClient(connInfo, defaultSSL, normalizeEndpoint)
	if err != nil {
		return err
	}

	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	buckets, err := client.ListBuckets(testCtx)
	if err != nil {
		return fmt.Errorf("failed to list buckets: %w", err)
	}

	bucket := plugin.GetString(connInfo, "bucket")
	if bucket == "" {
		return nil
	}
	for _, b := range buckets {
		if b.Name == bucket {
			return nil
		}
	}
	return fmt.Errorf("bucket '%s' not found", bucket)
}

func InferContentType(objectKey string) string {
	ext := strings.ToLower(filepath.Ext(objectKey))
	if mimeType := mime.TypeByExtension(ext); mimeType != "" {
		return mimeType
	}

	switch ext {
	case ".geojson":
		return "application/geo+json"
	case ".shp", ".shx":
		return "application/x-shapefile"
	case ".dbf":
		return "application/x-dbf"
	case ".prj":
		return "application/x-shapefile-prj"
	case ".kml":
		return "application/vnd.google-earth.kml+xml"
	case ".kmz":
		return "application/vnd.google-earth.kmz"
	case ".gpx":
		return "application/gpx+xml"
	case ".gml":
		return "application/gml+xml"
	case ".tif", ".tiff":
		return "image/tiff"
	default:
		return "application/octet-stream"
	}
}

func SplitBucketPrefix(path string) (bucket, prefix string) {
	path = strings.TrimPrefix(path, "/")
	idx := strings.Index(path, "/")
	if idx < 0 {
		return path, ""
	}
	return path[:idx], path[idx+1:]
}

func SplitBucketDirectory(path string) (bucket, prefix string) {
	bucket, prefix = SplitBucketPrefix(path)
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return bucket, prefix
}

func CreateContent(ctx context.Context, client *miniogo.Client, path string, opts plugin.WriteOptions) (io.WriteCloser, error) {
	if client == nil {
		return nil, fmt.Errorf("object store client is required")
	}
	bucket, key := SplitBucketPrefix(path)
	if bucket == "" || key == "" {
		return nil, fmt.Errorf("invalid path: %s (expected bucket/key)", path)
	}
	if !opts.Overwrite {
		if _, err := client.StatObject(ctx, bucket, key, miniogo.StatObjectOptions{}); err == nil {
			return nil, fmt.Errorf("object already exists: %s", path)
		} else {
			resp := miniogo.ToErrorResponse(err)
			if resp.Code != "NoSuchKey" && resp.Code != "NoSuchBucket" && resp.StatusCode != 404 {
				return nil, fmt.Errorf("check object existence %s: %w", path, err)
			}
		}
	}
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket %q: %w", bucket, err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, miniogo.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("create bucket %q: %w", bucket, err)
		}
	}

	contentType := opts.ContentType
	if contentType == "" {
		contentType = InferContentType(key)
	}
	reader, writer := io.Pipe()
	uploadDone := make(chan error, 1)
	go func() {
		_, err := client.PutObject(ctx, bucket, key, reader, -1, miniogo.PutObjectOptions{
			ContentType:  contentType,
			UserMetadata: opts.UserMetadata,
		})
		if err != nil {
			uploadDone <- fmt.Errorf("write object %s: %w", path, err)
			return
		}
		uploadDone <- nil
	}()

	return &contentWriter{
		writer:     writer,
		uploadDone: uploadDone,
		path:       path,
	}, nil
}

func DeleteResource(ctx context.Context, client *miniogo.Client, path string) error {
	if client == nil {
		return fmt.Errorf("object store client is required")
	}
	bucket, key := SplitBucketPrefix(path)
	if bucket == "" || key == "" {
		return fmt.Errorf("invalid path: %s (expected bucket/key)", path)
	}
	if err := client.RemoveObject(ctx, bucket, key, miniogo.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete object %s: %w", path, err)
	}
	return nil
}

type contentWriter struct {
	writer     *io.PipeWriter
	uploadDone chan error
	path       string
	closed     bool
}

func (w *contentWriter) Write(p []byte) (int, error) {
	if w == nil || w.writer == nil {
		return 0, fmt.Errorf("object writer is not initialized")
	}
	if w.closed {
		return 0, fmt.Errorf("object writer is already closed")
	}
	return w.writer.Write(p)
}

func (w *contentWriter) Close() error {
	if w == nil || w.writer == nil {
		return nil
	}
	if w.closed {
		return nil
	}
	w.closed = true
	if err := w.writer.Close(); err != nil {
		_ = w.writer.CloseWithError(err)
		return fmt.Errorf("close object writer %s: %w", w.path, err)
	}
	if err := <-w.uploadDone; err != nil {
		return err
	}
	return nil
}
