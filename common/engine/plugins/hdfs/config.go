package hdfs

import (
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/addp/common/engine/plugin"
)

// Config contains connection facts; its management root is never a catalog segment.
type Config struct {
	Endpoint *url.URL
	RPC      *url.URL
	Root     string
	User     string
}

func ParseConnectionInfo(info plugin.ConnectionInfo) (*Config, error) {
	if plugin.GetString(info, "authentication") != "simple" {
		return nil, fmt.Errorf("HDFS requires explicit Simple authentication")
	}
	c := &Config{Root: plugin.GetString(info, "root_path"), User: plugin.GetString(info, "user")}
	var err error
	c.Endpoint, err = url.Parse(plugin.GetString(info, "webhdfs_endpoint"))
	if err != nil || !validURL(c.Endpoint) || (c.Endpoint.Scheme != "http" && c.Endpoint.Scheme != "https") || c.Endpoint.Path != "" && c.Endpoint.Path != "/" {
		return nil, fmt.Errorf("invalid HDFS WebHDFS endpoint")
	}
	c.RPC, err = url.Parse(plugin.GetString(info, "rpc_uri"))
	if err != nil || !validURL(c.RPC) || c.RPC.Scheme != "hdfs" || c.RPC.Port() == "" || c.RPC.Path != "" && c.RPC.Path != "/" {
		return nil, fmt.Errorf("invalid HDFS RPC URI; an explicit host and port are required")
	}
	if !strings.HasPrefix(c.Root, "/") || path.Clean(c.Root) != c.Root || strings.ContainsAny(c.Root, "\x00\\") {
		return nil, fmt.Errorf("HDFS management root must be an absolute canonical path")
	}
	if c.User == "" || strings.ContainsAny(c.User, "\x00/\\ \t\r\n") {
		return nil, fmt.Errorf("invalid HDFS Simple user")
	}
	return c, nil
}

func validURL(u *url.URL) bool {
	if u == nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		return err == nil && port > 0 && port <= 65535
	}
	return !strings.HasSuffix(u.Host, ":")
}

func validateName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") {
		return plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("invalid HDFS catalog name"))
	}
	return nil
}

func validateRelative(relative string) error {
	if relative == "" {
		return nil
	}
	for _, name := range strings.Split(relative, "/") {
		if err := validateName(name); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) PhysicalPath(relative string) (string, error) {
	if err := validateRelative(relative); err != nil {
		return "", err
	}
	if relative == "" {
		return c.Root, nil
	}
	return strings.TrimSuffix(c.Root, "/") + "/" + relative, nil
}

// NativeURI derives an execution-only address from the same validated connection facts.
func (c *Config) NativeURI(relative string) (string, error) {
	physical, err := c.PhysicalPath(relative)
	if err != nil {
		return "", err
	}
	u := *c.RPC
	u.Path, u.RawPath = physical, ""
	return u.String(), nil
}

func validateCatalog(p plugin.EngineCatalogPath) error {
	if p.Version != plugin.EngineCatalogPathVersion || len(p.Segments) == 0 || p.Segments[0].Term != plugin.EngineCatalogTermRoot || p.Segments[0].Kind != plugin.EngineCatalogKindRoot {
		return plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("HDFS requires an explicit file catalog root"))
	}
	for i, s := range p.Segments[1:] {
		if err := validateName(s.Name); err != nil {
			return err
		}
		isFile := s.Term == plugin.EngineCatalogTermFile && s.Kind == plugin.EngineCatalogKindFile
		isDir := s.Term == plugin.EngineCatalogTermDirectory && s.Kind == plugin.EngineCatalogKindDirectory
		if !isDir && (!isFile || i != len(p.Segments)-2) {
			return plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("invalid HDFS catalog hierarchy"))
		}
	}
	return nil
}
