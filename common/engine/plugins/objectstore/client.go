package objectstore

import (
	"context"
	"fmt"
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
