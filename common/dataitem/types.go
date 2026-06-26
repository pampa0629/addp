package dataitem

import (
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
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
}

type RefRule struct {
	RequiredExtensions []string
	OptionalExtensions []string
	EntryExtension     string
}

type WholeScopeRule struct {
	IgnoredFileNames     []string
	RequiredFileNames    []string
	RequiresStrongMatch  bool
	ExclusiveOnStrongHit bool
	ClaimAllOnStrongHit  bool
}

type FormatRule struct {
	Format   string
	DataType datatype.DataType
	Layout   format.Layout
	Priority int

	Entry           EntryRule
	Refs            *RefRule
	WholeScope      *WholeScopeRule
	RelatedRefSpecs []format.RelatedRefSpec
}

type ResolvedItem struct {
	Name     string
	FullName string
	ItemType string
	Layout   format.Layout
	DataType datatype.DataType
	Format   string

	PrimaryContentPath string
	ScopePath          string
	RefPaths           map[string]string
	RefList            []ItemRef

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

type ItemDescriptor struct {
	Layout             format.Layout
	DataType           datatype.DataType
	Format             string
	PrimaryContentPath string
	ScopePath          string
	PhysicalPath       string
	StoragePath        string
	StorageName        string
	StorageBucket      string
	Refs               []ItemRef
	SizeBytes          *int64
}

func (descriptor ItemDescriptor) RelatedRefs() []format.RelatedRef {
	if len(descriptor.Refs) == 0 {
		return nil
	}
	refs := make([]format.RelatedRef, 0, len(descriptor.Refs))
	for _, itemRef := range descriptor.Refs {
		role := strings.TrimSpace(itemRef.Role)
		if role == "" {
			role = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(itemRef.Extension)), ".")
		}
		refs = append(refs, format.NewRelatedRef(contentio.NewRef(itemRef.Path, role), itemRef.Required, itemRef.Primary))
	}
	return refs
}
