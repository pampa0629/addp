package dataitem

import "github.com/addp/common/resource"

type Organization string

const (
	OrganizationSingle Organization = "single"
	OrganizationMulti  Organization = "multi"
	OrganizationWhole  Organization = "whole"
)

type DataType string

const (
	DataTypeTable     DataType = "table"
	DataTypeDocument  DataType = "document"
	DataTypeMedia     DataType = "media"
	DataTypeContainer DataType = "container"
	DataTypeGraph     DataType = "graph"
	DataTypeFile      DataType = "file"
	DataTypeUnknown   DataType = "unknown"
)

type ComponentMatchScope string

const (
	ComponentMatchScopeSameDirectory ComponentMatchScope = "same_directory"
	ComponentMatchScopeSamePrefix    ComponentMatchScope = "same_prefix"
	ComponentMatchScopeRecursive     ComponentMatchScope = "recursive"
)

type ComponentMatchKey string

const (
	ComponentMatchKeyBaseName ComponentMatchKey = "base_name"
	ComponentMatchKeyManifest ComponentMatchKey = "manifest"
)

type ScopeKind string

const (
	ScopeKindDirectory    ScopeKind = "directory"
	ScopeKindObjectPrefix ScopeKind = "object_prefix"
	ScopeKindContainer    ScopeKind = "container"
	ScopeKindSchema       ScopeKind = "schema"
)

type Candidate struct {
	Path        string
	Name        string
	BaseName    string
	Extension   string
	ContentType string
	SizeBytes   *int64
	IsDirectory bool
	Properties  map[string]interface{}
}

type ResolveInput struct {
	ScopeKind  ScopeKind
	ScopePath  string
	Candidates []Candidate
	Options    ResolveOptions
}

type ResolveOptions struct {
	MaxItems        int
	IncludeIgnored  bool
	AllowWholeScope bool
	IgnorePolicy    IgnorePolicy
}

type ComponentRef struct {
	Role      string
	Path      string
	Required  bool
	Primary   bool
	Extension string
}

type EntryRule struct {
	Extensions []string
	MIMETypes  []string
}

type ComponentRule struct {
	MatchScope         ComponentMatchScope
	MatchKey           ComponentMatchKey
	RequiredExtensions []string
	OptionalExtensions []string
	EntryExtension     string
	AllowRecursive     bool
}

type ContainerRule struct {
	ExpandInternalItems bool
}

type WholeScopeRule struct {
	AllowRecursive       bool
	IgnoredFileNames     []string
	RequiresStrongMatch  bool
	ExclusiveOnStrongHit bool
}

type FormatRule struct {
	Format       string
	DataType     DataType
	Organization Organization
	Priority     int

	Entry          EntryRule
	Components     *ComponentRule
	Container      *ContainerRule
	WholeScope     *WholeScopeRule
	ComponentSpecs []resource.ComponentSpec
}

type ResolvedItem struct {
	Name         string
	FullName     string
	ItemType     string
	Organization Organization
	DataType     DataType
	Format       string

	EntryPath      string
	ComponentPaths map[string]string
	ComponentList  []ComponentRef

	SizeBytes *int64

	DetectionReason string
	Properties      map[string]interface{}
}

type ResolveResult struct {
	Items     []ResolvedItem
	Claims    map[string]bool
	Ignored   []IgnoredCandidate
	Exclusive bool
}

type IgnoredCandidate struct {
	Candidate Candidate
	Reason    string
}

func (item ResolvedItem) ResourceComponents() []resource.ComponentRef {
	components := make([]resource.ComponentRef, 0, len(item.ComponentList))
	for _, component := range item.ComponentList {
		role := component.Role
		if role == "" {
			role = component.Extension
		}
		components = append(components, resource.ComponentRef{
			ResourceRef:   resource.NewResourceRef(component.Path, resource.ResourceRoleComponent),
			ComponentRole: role,
			Required:      component.Required,
		})
	}
	return components
}
