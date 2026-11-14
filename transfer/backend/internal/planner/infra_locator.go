package planner

import (
	"fmt"
	"strings"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/infra/objectstore"
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
	if l == nil || l.Kind != "object" {
		return ""
	}
	value, err := (objectstore.Ref{Area: l.Namespace, Key: strings.Join(l.Path, "/"), Type: string(l.Type)}).URI()
	if err != nil {
		return ""
	}
	return value
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

func IsFileGDBArchiveTarget(formatName, locator, parentLocator string) bool {
	return strings.EqualFold(strings.TrimSpace(formatName), "filegdb") &&
		(IsInfraLocatorURI(locator) || IsInfraLocatorURI(parentLocator))
}

func IsInfraLocatorURI(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), infraLocatorScheme+"://")
}

func ParseInfraLocatorURI(value string) (*InfraLocator, error) {
	ref, err := objectstore.Parse(value)
	if err != nil {
		return nil, err
	}
	return &InfraLocator{Kind: "object", Namespace: ref.Area, Path: strings.Split(ref.Key, "/"), Type: resourcetree.ResourceType(ref.Type)}, nil
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
	case "object":
		return l.objectCatalogPath()
	default:
		return engineplugin.EngineCatalogPath{}, fmt.Errorf("unsupported infra kind %q", l.Kind)
	}
}

func (l *InfraLocator) objectCatalogPath() (engineplugin.EngineCatalogPath, error) {
	area := strings.TrimSpace(l.Namespace)
	if area == "" {
		return engineplugin.EngineCatalogPath{}, fmt.Errorf("infra object locator area is required")
	}
	objectPath := strings.Join(l.Path, "/")
	switch l.Type {
	case resourcetree.TypeObject:
		if strings.TrimSpace(objectPath) == "" {
			return engineplugin.EngineCatalogPath{}, fmt.Errorf("infra object locator path is required")
		}
		return engineplugin.ObjectItemPath(0, area, objectPath), nil
	case resourcetree.TypePrefix:
		return engineplugin.ObjectDirectoryPath(0, area, objectPath), nil
	default:
		return engineplugin.EngineCatalogPath{}, fmt.Errorf("unsupported infra object locator type %q", l.Type)
	}
}

func infraEngineRefType(kind string) string {
	return "infra:" + strings.ToLower(strings.TrimSpace(kind))
}
