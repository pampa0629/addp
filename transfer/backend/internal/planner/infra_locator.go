package planner

import (
	"fmt"
	"net/url"
	"strings"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/resourcetree"
)

const infraLocatorScheme = "addp-infra"

type InfraLocator struct {
	Kind      string
	Namespace string
	Path      []string
	Type      resourcetree.ResourceType
}

func (l *InfraLocator) ToURI() string {
	if l == nil || strings.TrimSpace(l.Kind) == "" || strings.TrimSpace(l.Namespace) == "" || l.Type == "" {
		return ""
	}
	parts := make([]string, 0, len(l.Path)+1)
	parts = append(parts, strings.TrimSpace(l.Namespace))
	parts = append(parts, l.Path...)
	locator := &url.URL{
		Scheme: infraLocatorScheme,
		Host:   strings.TrimSpace(l.Kind),
		Path:   "/" + strings.Join(parts, "/"),
	}
	query := locator.Query()
	query.Set("type", string(l.Type))
	locator.RawQuery = query.Encode()
	return locator.String()
}

func (l *InfraLocator) Child(name string, resourceType resourcetree.ResourceType) (*InfraLocator, error) {
	if l == nil {
		return nil, fmt.Errorf("infra parent locator is required")
	}
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "/") {
		return nil, fmt.Errorf("infra child name must be one path segment")
	}
	if resourceType == "" {
		return nil, fmt.Errorf("infra child resource type is required")
	}
	return &InfraLocator{
		Kind:      l.Kind,
		Namespace: l.Namespace,
		Path:      append(append([]string(nil), l.Path...), name),
		Type:      resourceType,
	}, nil
}

func IsInfraLocatorURI(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), infraLocatorScheme+"://")
}

func ParseInfraLocatorURI(value string) (*InfraLocator, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("invalid infra locator: %w", err)
	}
	if parsed.Scheme != infraLocatorScheme {
		return nil, fmt.Errorf("invalid infra locator scheme: expected %q, got %q", infraLocatorScheme, parsed.Scheme)
	}

	kind := strings.TrimSpace(parsed.Host)
	if kind == "" {
		return nil, fmt.Errorf("infra locator kind is required")
	}

	rawParts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		decoded, err := url.PathUnescape(part)
		if err != nil {
			return nil, fmt.Errorf("decode infra locator path segment %q: %w", part, err)
		}
		parts = append(parts, decoded)
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("infra locator namespace is required")
	}

	resourceType := resourcetree.ResourceType(parsed.Query().Get("type"))
	if resourceType == "" {
		return nil, fmt.Errorf("infra locator type is required")
	}

	return &InfraLocator{
		Kind:      kind,
		Namespace: parts[0],
		Path:      parts[1:],
		Type:      resourceType,
	}, nil
}

func (l *InfraLocator) EngineRef() EngineRef {
	if l == nil {
		return EngineRef{}
	}
	return EngineRef{Type: infraEngineRefType(l.Kind)}
}

func (l *InfraLocator) EngineCatalogPath() (engineplugin.EngineCatalogPath, error) {
	if l == nil {
		return engineplugin.EngineCatalogPath{}, fmt.Errorf("infra locator is required")
	}
	switch strings.ToLower(strings.TrimSpace(l.Kind)) {
	case "minio":
		return l.minioCatalogPath()
	default:
		return engineplugin.EngineCatalogPath{}, fmt.Errorf("unsupported infra kind %q", l.Kind)
	}
}

func (l *InfraLocator) minioCatalogPath() (engineplugin.EngineCatalogPath, error) {
	bucket := strings.TrimSpace(l.Namespace)
	if bucket == "" {
		return engineplugin.EngineCatalogPath{}, fmt.Errorf("infra minio locator bucket is required")
	}
	objectPath := strings.Join(l.Path, "/")
	switch l.Type {
	case resourcetree.TypeObject:
		if strings.TrimSpace(objectPath) == "" {
			return engineplugin.EngineCatalogPath{}, fmt.Errorf("infra minio object locator path is required")
		}
		return engineplugin.ObjectItemPath(0, bucket, objectPath), nil
	case resourcetree.TypePrefix, resourcetree.TypeDirectory:
		return engineplugin.ObjectDirectoryPath(0, bucket, objectPath), nil
	default:
		return engineplugin.EngineCatalogPath{}, fmt.Errorf("unsupported infra minio locator type %q", l.Type)
	}
}

func infraEngineRefType(kind string) string {
	return "infra:" + strings.ToLower(strings.TrimSpace(kind))
}
