package workflowaccess

import (
	"fmt"
	"path"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	commonModels "github.com/addp/common/models"
)

const (
	SchemaVersion     = "addp.workflow.access-plan/v1"
	MethodMountedPath = "mounted_path"
	MethodObjectStore = "object_store"
	KindFile          = "file"
	KindDirectory     = "directory"
	WriteModeCreate   = "create"
	WriteModeReplace  = "replace"
)

type Access struct {
	Method     string `json:"method"`
	Path       string `json:"path,omitempty"`
	Server     string `json:"server,omitempty"`
	ExportPath string `json:"export_path,omitempty"`
	NFSVersion string `json:"nfs_version,omitempty"`
	Endpoint   string `json:"endpoint,omitempty"`
	AccessKey  string `json:"access_key,omitempty"`
	SecretKey  string `json:"secret_key,omitempty"`
	UseSSL     bool   `json:"use_ssl,omitempty"`
	Bucket     string `json:"bucket,omitempty"`
	Object     string `json:"object,omitempty"`
	Prefix     string `json:"prefix,omitempty"`
}

type Source struct {
	Kind         string               `json:"kind"`
	Format       string               `json:"format"`
	Entrypoint   string               `json:"entrypoint,omitempty"`
	Access       Access               `json:"access"`
	Metadata     commonModels.JSONMap `json:"metadata,omitempty"`
	RelatedFiles []RelatedFile        `json:"related_files,omitempty"`
}

// RelatedFile is an exact execution-time companion, never a directory grant.
type RelatedFile struct {
	Role   string `json:"role"`
	Access Access `json:"access"`
}

type Target struct {
	Kind        string               `json:"kind"`
	Format      string               `json:"format"`
	Name        string               `json:"name"`
	WriteMode   string               `json:"write_mode"`
	ContentType string               `json:"content_type,omitempty"`
	Access      Access               `json:"access"`
	Metadata    commonModels.JSONMap `json:"metadata,omitempty"`
}

type Plan struct {
	SchemaVersion string `json:"schema_version"`
	Source        Source `json:"source"`
	Target        Target `json:"target"`
}

// SourcePlan is the source-only access contract used by read-only direct
// operators such as metadata inspection.
type SourcePlan struct {
	SchemaVersion string `json:"schema_version"`
	Source        Source `json:"source"`
}

// TargetPlan is the target-only access contract used by incremental direct
// writers that receive table batches across multiple calls.
type TargetPlan struct {
	SchemaVersion string `json:"schema_version"`
	Target        Target `json:"target"`
}

func NewSourcePlan(source Source) (SourcePlan, error) {
	plan := SourcePlan{SchemaVersion: SchemaVersion, Source: source}
	if err := plan.Validate(); err != nil {
		return SourcePlan{}, err
	}
	return plan, nil
}

func (p SourcePlan) Validate() error {
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported workflow access plan schema_version %q", p.SchemaVersion)
	}
	return validateSource(p.Source)
}

func (p SourcePlan) JSONMap() commonModels.JSONMap {
	return commonModels.JSONMap{
		"schema_version": p.SchemaVersion,
		"source":         sourceJSONMap(p.Source, false),
	}
}

func NewTargetPlan(target Target) (TargetPlan, error) {
	plan := TargetPlan{SchemaVersion: SchemaVersion, Target: target}
	if err := plan.Validate(); err != nil {
		return TargetPlan{}, err
	}
	return plan, nil
}

func (p TargetPlan) Validate() error {
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported workflow access plan schema_version %q", p.SchemaVersion)
	}
	if err := validateResource("target", p.Target.Kind, p.Target.Format, p.Target.Access); err != nil {
		return err
	}
	if strings.TrimSpace(p.Target.Name) == "" {
		return fmt.Errorf("target name is required")
	}
	switch p.Target.WriteMode {
	case WriteModeCreate, WriteModeReplace:
	default:
		return fmt.Errorf("target write_mode must be create or replace")
	}
	return nil
}

func (p TargetPlan) JSONMap() commonModels.JSONMap {
	return commonModels.JSONMap{
		"schema_version": p.SchemaVersion,
		"target":         targetJSONMap(p.Target, false),
	}
}

func (p TargetPlan) AuditJSONMap() commonModels.JSONMap {
	return commonModels.JSONMap{
		"schema_version": p.SchemaVersion,
		"target":         targetJSONMap(p.Target, true),
	}
}

func New(source Source, target Target) (Plan, error) {
	plan := Plan{SchemaVersion: SchemaVersion, Source: source, Target: target}
	if err := plan.Validate(); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (p Plan) Validate() error {
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported workflow access plan schema_version %q", p.SchemaVersion)
	}
	if err := validateSource(p.Source); err != nil {
		return err
	}
	if err := validateResource("target", p.Target.Kind, p.Target.Format, p.Target.Access); err != nil {
		return err
	}
	if strings.TrimSpace(p.Target.Name) == "" {
		return fmt.Errorf("target name is required")
	}
	switch p.Target.WriteMode {
	case WriteModeCreate, WriteModeReplace:
	default:
		return fmt.Errorf("target write_mode must be create or replace")
	}
	return nil
}

func (p Plan) JSONMap() commonModels.JSONMap {
	return commonModels.JSONMap{
		"schema_version": p.SchemaVersion,
		"source":         sourceJSONMap(p.Source, false),
		"target":         targetJSONMap(p.Target, false),
	}
}

func (p Plan) AuditJSONMap() commonModels.JSONMap {
	return commonModels.JSONMap{
		"schema_version": p.SchemaVersion,
		"source":         sourceJSONMap(p.Source, true),
		"target":         targetJSONMap(p.Target, true),
	}
}

func validateResource(label, kind, format string, access Access) error {
	if kind != KindFile && kind != KindDirectory {
		return fmt.Errorf("%s kind must be file or directory", label)
	}
	if strings.TrimSpace(format) == "" {
		return fmt.Errorf("%s format is required", label)
	}
	switch access.Method {
	case MethodMountedPath:
		if strings.TrimSpace(access.Path) == "" {
			return fmt.Errorf("%s mounted_path requires path", label)
		}
	case MethodObjectStore:
		if strings.TrimSpace(access.Endpoint) == "" || strings.TrimSpace(access.AccessKey) == "" || strings.TrimSpace(access.SecretKey) == "" || strings.TrimSpace(access.Bucket) == "" {
			return fmt.Errorf("%s object_store requires endpoint, access_key, secret_key and bucket", label)
		}
		if kind == KindFile && strings.TrimSpace(access.Object) == "" {
			return fmt.Errorf("%s object_store file requires object", label)
		}
		if kind == KindDirectory && strings.TrimSpace(access.Prefix) == "" {
			return fmt.Errorf("%s object_store directory requires prefix", label)
		}
	default:
		return fmt.Errorf("%s access method must be mounted_path or object_store", label)
	}
	return nil
}

func validateSource(source Source) error {
	if err := validateResource("source", source.Kind, source.Format, source.Access); err != nil {
		return err
	}
	if source.Format != "envi" {
		if source.RelatedFiles != nil {
			return fmt.Errorf("related files are not admitted for this source format")
		}
		return nil
	}
	if source.Kind != KindFile || len(source.RelatedFiles) != 1 || source.RelatedFiles[0].Role != "header" || source.Entrypoint != "" {
		return fmt.Errorf("ENVI requires a file source and one exact header")
	}
	companion := source.RelatedFiles[0].Access
	if err := validateResource("related header", KindFile, "envi", companion); err != nil {
		return err
	}
	main := source.Access
	var mainName, headerName string
	switch main.Method {
	case MethodMountedPath:
		mainName, headerName = main.Path, companion.Path
		if !path.IsAbs(mainName) || !path.IsAbs(headerName) {
			return fmt.Errorf("ENVI mounted paths must be absolute")
		}
		main.Path, companion.Path = "", ""
	case MethodObjectStore:
		mainName, headerName = main.Object, companion.Object
		if strings.HasPrefix(mainName, "/") || strings.HasPrefix(headerName, "/") {
			return fmt.Errorf("ENVI object names must be exact relative names")
		}
		main.Object, companion.Object = "", ""
	}
	validName := func(name string) bool {
		return name != "" && utf8.ValidString(name) && len(name) <= 4096 && name != ".." && !strings.HasPrefix(name, "../") && path.Clean(name) == name && !strings.ContainsAny(name, "\\:") && !strings.ContainsFunc(name, unicode.IsControl)
	}
	if !reflect.DeepEqual(main, companion) || main.Prefix != "" || !validName(mainName) || !validName(headerName) || path.Dir(mainName) != path.Dir(headerName) ||
		strings.EqualFold(path.Base(mainName), path.Base(headerName)) || strings.EqualFold(path.Ext(mainName), ".hdr") || !strings.EqualFold(path.Ext(headerName), ".hdr") {
		return fmt.Errorf("ENVI companion must be an exact header beside the primary on the same connection")
	}
	return nil
}

func sourceJSONMap(source Source, audit bool) commonModels.JSONMap {
	result := commonModels.JSONMap{
		"kind":   source.Kind,
		"format": source.Format,
		"access": accessJSONMap(source.Access, audit),
	}
	if source.Entrypoint != "" {
		result["entrypoint"] = source.Entrypoint
	}
	if source.Metadata != nil {
		result["metadata"] = source.Metadata.Clone()
	}
	if source.RelatedFiles != nil {
		members := make([]interface{}, len(source.RelatedFiles))
		for index, file := range source.RelatedFiles {
			members[index] = commonModels.JSONMap{"role": file.Role, "access": accessJSONMap(file.Access, audit)}
		}
		result["related_files"] = members
	}
	return result
}

func targetJSONMap(target Target, audit bool) commonModels.JSONMap {
	result := commonModels.JSONMap{
		"kind":       target.Kind,
		"format":     target.Format,
		"name":       target.Name,
		"write_mode": target.WriteMode,
		"access":     accessJSONMap(target.Access, audit),
	}
	if target.ContentType != "" {
		result["content_type"] = target.ContentType
	}
	if target.Metadata != nil {
		result["metadata"] = target.Metadata.Clone()
	}
	return result
}

func accessJSONMap(access Access, audit bool) commonModels.JSONMap {
	result := commonModels.JSONMap{"method": access.Method}
	if access.Path != "" {
		result["path"] = access.Path
	}
	if access.Server != "" {
		result["server"] = access.Server
	}
	if access.ExportPath != "" {
		result["export_path"] = access.ExportPath
	}
	if access.NFSVersion != "" {
		result["nfs_version"] = access.NFSVersion
	}
	if access.Endpoint != "" {
		result["endpoint"] = access.Endpoint
	}
	if !audit && access.AccessKey != "" {
		result["access_key"] = access.AccessKey
	}
	if !audit && access.SecretKey != "" {
		result["secret_key"] = access.SecretKey
	}
	if access.UseSSL {
		result["use_ssl"] = true
	}
	if access.Bucket != "" {
		result["bucket"] = access.Bucket
	}
	if access.Object != "" {
		result["object"] = access.Object
	}
	if access.Prefix != "" {
		result["prefix"] = access.Prefix
	}
	return result
}
