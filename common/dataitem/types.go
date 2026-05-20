package dataitem

import (
	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
)

// Layout is the resolved data item layout.
//
// It reuses format.Layout values deliberately: format capability declares which
// layouts a format can support, while dataitem resolves one concrete layout for
// an item and stores it as layout.
type Layout = format.Layout

const (
	LayoutSingle Layout = format.FormatLayoutSingle
	LayoutMulti  Layout = format.FormatLayoutMulti
	LayoutWhole  Layout = format.FormatLayoutWhole
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

type RefMatchScope string

const (
	RefMatchScopeSameDirectory RefMatchScope = "same_directory"
	RefMatchScopeSamePrefix    RefMatchScope = "same_prefix"
	RefMatchScopeRecursive     RefMatchScope = "recursive"
)

type RefMatchKey string

const (
	RefMatchKeyBaseName RefMatchKey = "base_name"
	RefMatchKeyManifest RefMatchKey = "manifest"
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

type ItemRef struct {
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

type RefRule struct {
	MatchScope         RefMatchScope
	MatchKey           RefMatchKey
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
	Format   string
	DataType DataType
	Layout   Layout
	Priority int

	Entry           EntryRule
	Refs            *RefRule
	Container       *ContainerRule
	WholeScope      *WholeScopeRule
	RelatedRefSpecs []format.RelatedRefSpec
}

type ResolvedItem struct {
	Name     string
	FullName string
	ItemType string
	Layout   Layout
	DataType DataType
	Format   string

	EntryPath string
	RefPaths  map[string]string
	RefList   []ItemRef

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

func (item ResolvedItem) RelatedRefs() []format.RelatedRef {
	refs := make([]format.RelatedRef, 0, len(item.RefList))
	for _, itemRef := range item.RefList {
		role := itemRef.Role
		if role == "" {
			role = itemRef.Extension
		}
		contentRef := contentio.NewRef(itemRef.Path, role)
		refs = append(refs, format.NewRelatedRef(contentRef, itemRef.Required, itemRef.Primary))
	}
	return refs
}

type ScanTarget struct {
	Path string
}
