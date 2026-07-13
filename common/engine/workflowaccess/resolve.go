package workflowaccess

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/objectstore"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

type ResourceSpec struct {
	Engine      *commonModels.Engine
	Locator     *resourcetree.ResourceLocator
	Kind        string
	Format      string
	Name        string
	Entrypoint  string
	WriteMode   string
	ContentType string
	Metadata    commonModels.JSONMap
}

func ResolveSource(spec ResourceSpec) (Source, error) {
	access, err := resolveAccess(spec.Engine, spec.Locator, spec.Kind, false, "")
	if err != nil {
		return Source{}, err
	}
	return Source{
		Kind:       spec.Kind,
		Format:     spec.Format,
		Entrypoint: spec.Entrypoint,
		Access:     access,
		Metadata:   spec.Metadata.Clone(),
	}, nil
}

func ResolveTarget(spec ResourceSpec) (Target, *resourcetree.ResourceLocator, error) {
	if strings.TrimSpace(spec.Name) == "" {
		return Target{}, nil, fmt.Errorf("target name is required")
	}
	access, err := resolveAccess(spec.Engine, spec.Locator, spec.Kind, true, spec.Name)
	if err != nil {
		return Target{}, nil, err
	}
	targetLocator := spec.Locator.Clone()
	targetLocator.Path = append(append([]string{}, spec.Locator.Path...), spec.Name)
	if spec.Kind == KindDirectory {
		targetLocator.Type = resourcetree.TypeDirectory
	} else if access.Method == MethodObjectStore {
		targetLocator.Type = resourcetree.TypeObject
	} else {
		targetLocator.Type = resourcetree.TypeFile
	}
	writeMode := strings.TrimSpace(spec.WriteMode)
	if writeMode == "" {
		writeMode = WriteModeCreate
	}
	return Target{
		Kind:        spec.Kind,
		Format:      spec.Format,
		Name:        spec.Name,
		WriteMode:   writeMode,
		ContentType: spec.ContentType,
		Access:      access,
		Metadata:    spec.Metadata.Clone(),
	}, targetLocator, nil
}

func ResolveObjectStoreTarget(connInfo plugin.ConnectionInfo, bucket, objectOrPrefix, kind string) (Access, error) {
	cfg, err := objectstore.ParseClientConfig(connInfo, false, true)
	if err != nil {
		return Access{}, err
	}
	access := Access{
		Method:    MethodObjectStore,
		Endpoint:  cfg.Endpoint,
		AccessKey: cfg.AccessKey,
		SecretKey: cfg.SecretKey,
		UseSSL:    cfg.UseSSL,
		Bucket:    strings.Trim(bucket, "/ "),
	}
	if kind == KindDirectory {
		access.Prefix = strings.Trim(objectOrPrefix, "/ ")
	} else {
		access.Object = strings.Trim(objectOrPrefix, "/ ")
	}
	return access, nil
}

func resolveAccess(engine *commonModels.Engine, locator *resourcetree.ResourceLocator, kind string, target bool, name string) (Access, error) {
	if engine == nil || locator == nil {
		return Access{}, fmt.Errorf("engine and locator are required")
	}
	if engine.ID != locator.EngineID {
		return Access{}, fmt.Errorf("engine %d does not match locator engine %d", engine.ID, locator.EngineID)
	}
	fullName := strings.Trim(strings.TrimSpace(locator.FullName()), "/")
	if target {
		fullName = joinPath(fullName, name)
	}
	connInfo := plugin.ConnectionInfo(engine.ConnectionInfo)
	switch strings.ToLower(strings.TrimSpace(engine.EngineType)) {
	case "nfs", "nas", "localfs", "filesystem":
		basePath := firstNonEmpty(
			plugin.GetString(connInfo, "mount_path"),
			plugin.GetString(connInfo, "export_path"),
			plugin.GetString(connInfo, "base_path"),
		)
		if basePath == "" {
			return Access{}, fmt.Errorf("file engine requires mount_path, export_path or base_path")
		}
		access := Access{Method: MethodMountedPath, Path: filepath.Join(basePath, filepath.FromSlash(fullName))}
		if strings.EqualFold(engine.EngineType, "nfs") || strings.EqualFold(engine.EngineType, "nas") {
			access.Server = plugin.GetString(connInfo, "server")
			access.ExportPath = plugin.GetString(connInfo, "export_path")
			access.NFSVersion = firstNonEmpty(plugin.GetString(connInfo, "nfs_version"), plugin.GetString(connInfo, "version"))
		}
		return access, nil
	case "minio", "s3":
		bucket, objectOrPrefix := objectstore.SplitBucketPrefix(fullName)
		if bucket == "" || objectOrPrefix == "" {
			return Access{}, fmt.Errorf("object store resource requires bucket and object or prefix")
		}
		return ResolveObjectStoreTarget(connInfo, bucket, objectOrPrefix, kind)
	default:
		return Access{}, fmt.Errorf("engine %s cannot produce mounted_path or object_store workflow access", engine.EngineType)
	}
}

func joinPath(parent, name string) string {
	parent = strings.Trim(parent, "/ ")
	name = strings.Trim(name, "/ ")
	if parent == "" {
		return name
	}
	if name == "" {
		return parent
	}
	return parent + "/" + name
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
