package service

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

const infraLocatorScheme = "addp-infra"

type InspectService struct {
	cfg *config.Config
}

type InspectRequest struct {
	Locator   string                `json:"locator"`
	RefGroups []models.ScanRefGroup `json:"ref_groups,omitempty"`
	ScanDepth string                `json:"scan_depth,omitempty"`
}

type InspectResult struct {
	Attributes models.JSONMap `json:"attributes"`
	FullName   string         `json:"full_name,omitempty"`
	Name       string         `json:"name,omitempty"`
	DataType   string         `json:"data_type,omitempty"`
	Format     string         `json:"format,omitempty"`
	Layout     string         `json:"layout,omitempty"`
}

type infraLocator struct {
	Kind      string
	Namespace string
	Path      []string
	Type      resourcetree.ResourceType
}

func NewInspectService(cfg *config.Config) *InspectService {
	return &InspectService{cfg: cfg}
}

func (s *InspectService) Inspect(ctx context.Context, tenantID uint, req InspectRequest) (*InspectResult, error) {
	loc, err := parseInfraLocator(req.Locator)
	if err != nil {
		return nil, err
	}
	if loc.Kind != "minio" {
		return nil, fmt.Errorf("unsupported infra locator kind %q", loc.Kind)
	}
	return s.inspectBuiltinMinIO(ctx, tenantID, loc, req)
}

func (s *InspectService) inspectBuiltinMinIO(ctx context.Context, tenantID uint, loc *infraLocator, req InspectRequest) (*InspectResult, error) {
	if s == nil || s.cfg == nil {
		return nil, fmt.Errorf("inspect service config is required")
	}
	if strings.TrimSpace(s.cfg.BuiltinMinioEndpoint) == "" {
		return nil, fmt.Errorf("builtin minio endpoint is required")
	}
	enginePlugin, err := plugin.Get("minio")
	if err != nil {
		return nil, fmt.Errorf("minio plugin is not registered: %w", err)
	}
	contentReader, ok := enginePlugin.(plugin.ContentReadableProvider)
	if !ok {
		return nil, fmt.Errorf("minio plugin does not implement ContentReadableProvider")
	}
	catalogProvider, ok := enginePlugin.(plugin.EngineCatalogProvider)
	if !ok {
		return nil, fmt.Errorf("minio plugin does not implement EngineCatalogProvider")
	}

	connInfo := plugin.ConnectionInfo{
		"endpoint":   s.cfg.BuiltinMinioEndpoint,
		"access_key": s.cfg.BuiltinMinioAccessKey,
		"secret_key": s.cfg.BuiltinMinioSecretKey,
		"use_ssl":    s.cfg.BuiltinMinioUseSSL,
	}
	resource := &commonModels.Engine{
		ID:             0,
		Name:           "builtin-minio",
		EngineType:     "minio",
		ConnectionInfo: map[string]interface{}(connInfo),
		TenantID:       &tenantID,
	}

	groups := req.RefGroups
	if len(groups) == 0 {
		group, err := s.refGroupFromInfraObject(ctx, catalogProvider, connInfo, loc)
		if err != nil {
			return nil, err
		}
		groups = []models.ScanRefGroup{group}
	}
	return inspectFirstObjectRefGroup(ctx, resource, contentReader, connInfo, groups)
}

func (s *InspectService) refGroupFromInfraObject(ctx context.Context, catalogProvider plugin.EngineCatalogProvider, connInfo plugin.ConnectionInfo, loc *infraLocator) (models.ScanRefGroup, error) {
	if loc == nil || loc.Type != resourcetree.TypeObject {
		return models.ScanRefGroup{}, fmt.Errorf("inspect locator must be an infra minio object")
	}
	objectPath := strings.Join(loc.Path, "/")
	if strings.TrimSpace(objectPath) == "" {
		return models.ScanRefGroup{}, fmt.Errorf("infra minio object path is required")
	}
	dir := strings.Trim(path.Dir(objectPath), ".")
	if dir == "/" {
		dir = ""
	}
	parent := plugin.ObjectDirectoryPath(0, loc.Namespace, dir)
	children, err := catalogProvider.ListChildren(ctx, connInfo, parent, plugin.ListOptions{})
	if err != nil {
		return models.ScanRefGroup{}, fmt.Errorf("list inspect object siblings: %w", err)
	}
	base := strings.TrimSuffix(path.Base(objectPath), path.Ext(objectPath))
	primary := strings.Trim(loc.Namespace+"/"+objectPath, "/")
	refs := make([]models.ScanRef, 0, len(children))
	for _, child := range children {
		if child.Role != plugin.EngineCatalogRoleLeaf {
			continue
		}
		storagePath := strings.TrimSpace("")
		if child.Storage != nil {
			storagePath = strings.Trim(child.Storage.Path, "/")
		}
		if storagePath == "" {
			storagePath = strings.Trim(child.Path.StringPath(), "/")
		}
		_, childObjectPath := splitBucketObject(storagePath)
		if childObjectPath == "" {
			childObjectPath = strings.TrimPrefix(storagePath, strings.Trim(loc.Namespace, "/")+"/")
		}
		if strings.TrimSuffix(path.Base(childObjectPath), path.Ext(childObjectPath)) != base {
			continue
		}
		isPrimary := isPrimaryShapefileExtension(path.Ext(childObjectPath))
		refs = append(refs, models.ScanRef{
			Path:     strings.Trim(loc.Namespace+"/"+childObjectPath, "/"),
			Role:     roleFromExtension(path.Ext(childObjectPath)),
			Required: requiredShapefileExtension(path.Ext(childObjectPath)),
			Primary:  isPrimary,
		})
	}
	if len(refs) == 0 {
		refs = append(refs, models.ScanRef{Path: primary, Role: "main", Required: true, Primary: true})
	}
	return models.ScanRefGroup{Primary: primary, Refs: refs}, nil
}

func inspectFirstObjectRefGroup(ctx context.Context, resource *commonModels.Engine, contentReader plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, groups []models.ScanRefGroup) (*InspectResult, error) {
	for _, group := range groups {
		primary := scanflow.ScanRefGroupPrimaryPath(group)
		if primary == "" {
			continue
		}
		bucket, objectPath, err := plugin.SplitObjectRefPath(primary)
		if err != nil {
			return nil, err
		}
		resources, err := scanflow.ObjectResourcesFromScanRefGroup(resource.ID, bucket, group)
		if err != nil {
			return nil, err
		}
		candidates := scanflow.ObjectRefGroupCandidateSet(resource.ID, bucket, objectPath, resources)
		detection, err := scanflow.ResolveContentCandidates(ctx, contentReader, connInfo, resource.ID, candidates)
		if err != nil {
			return nil, err
		}
		for _, detected := range detection.Items {
			if detected == nil {
				continue
			}
			attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(detected)))
			metaattr.MergeStandardAttributes(attrs, detected.Attributes)
			metaattr.SetStorage(attrs, "physical_path", detectedItemPhysicalPath(detected, primary))
			metaattr.SetStorage(attrs, "bucket", bucket)
			metaattr.SetStorage(attrs, "path", strings.Trim(path.Dir(objectPath), "."))
			metaattr.SetStorage(attrs, "name", path.Base(objectPath))
			return &InspectResult{
				Attributes: metaattr.Normalize(attrs),
				FullName:   strings.Trim(bucket+"/"+objectPath, "/"),
				Name:       path.Base(objectPath),
				DataType:   string(detected.DataType),
				Format:     detected.Format,
				Layout:     string(detected.Layout),
			}, nil
		}
	}
	return nil, fmt.Errorf("no inspectable item detected")
}

func parseInfraLocator(value string) (*infraLocator, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("invalid infra locator: %w", err)
	}
	if parsed.Scheme != infraLocatorScheme {
		return nil, fmt.Errorf("invalid infra locator scheme %q", parsed.Scheme)
	}
	kind := strings.ToLower(strings.TrimSpace(parsed.Host))
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
	locType := resourcetree.ResourceType(parsed.Query().Get("type"))
	if locType == "" {
		return nil, fmt.Errorf("infra locator type is required")
	}
	return &infraLocator{
		Kind:      kind,
		Namespace: parts[0],
		Path:      parts[1:],
		Type:      locType,
	}, nil
}

func detectedItemPhysicalPath(item *metaitem.DetectedItem, fallback string) string {
	if item == nil {
		return strings.Trim(fallback, "/")
	}
	if item.PrimaryContentPath != "" {
		return strings.Trim(item.PrimaryContentPath, "/")
	}
	if item.PhysicalPath != "" {
		return strings.Trim(item.PhysicalPath, "/")
	}
	return strings.Trim(fallback, "/")
}

func splitBucketObject(value string) (bucket, objectPath string) {
	trimmed := strings.Trim(value, "/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 {
		return trimmed, ""
	}
	return parts[0], parts[1]
}

func roleFromExtension(ext string) string {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".shp":
		return "main"
	case ".shx":
		return "index"
	case ".dbf":
		return "attributes"
	case ".prj", ".qpj":
		return "projection"
	case ".cpg":
		return "encoding"
	default:
		return strings.TrimPrefix(strings.ToLower(ext), ".")
	}
}

func requiredShapefileExtension(ext string) bool {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".shp", ".shx", ".dbf":
		return true
	default:
		return false
	}
}

func isPrimaryShapefileExtension(ext string) bool {
	return strings.EqualFold(strings.TrimSpace(ext), ".shp")
}
