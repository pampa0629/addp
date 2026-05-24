package format

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
)

const (
	ContainerChildResourceStream = "stream"
	ContainerChildResourceNative = "native"
)

// ContainerChildResource is a resolved nested resource inside a container.
//
// Stream children expose an independent contentio.Reader/contentio.Ref, for example
// a ZIP entry. Native children reuse the parent resource with format-specific
// options, for example an Excel sheet or SQLite table.
type ContainerChildResource struct {
	Name          string
	Kind          string
	DataType      datatype.DataType
	Format        FormatType
	Layout        string
	Refs          []RelatedRef
	ResourceKind  string
	Reader        contentio.Reader
	Ref           contentio.Ref
	ParentReader  contentio.Reader
	ParentRef     contentio.Ref
	ParentFormat  FormatType
	ParentOptions *ParseOptions
	Properties    map[string]interface{}
}

func (r *ContainerChildResource) Open(ctx context.Context) (io.ReadCloser, error) {
	if r == nil {
		return nil, fmt.Errorf("container child resource is nil")
	}
	reader := r.Reader
	ref := r.Ref
	if r.ResourceKind == ContainerChildResourceNative {
		reader = r.ParentReader
		ref = r.ParentRef
	}
	if reader == nil {
		return nil, fmt.Errorf("container child resource %s has no reader", r.Name)
	}
	if strings.TrimSpace(ref.Path) == "" {
		return nil, fmt.Errorf("container child resource %s has no resource ref", r.Name)
	}
	return reader.Open(ctx, ref)
}

// ContainerChildResolver resolves a child locator into a nested contentio.
//
// The resolver does not connect to engines. The caller supplies the parent
// contentio.Reader and contentio.Ref that already include permissions, credentials,
// auditing and storage-specific read behavior.
type ContainerChildResolver interface {
	ContentReader
	ResolveContainerChild(ctx context.Context, parent contentio.Reader, parentRef contentio.Ref, child ContainerChildInfo, options *ParseOptions) (*ContainerChildResource, error)
}

func NativeContainerChildResource(parent contentio.Reader, parentRef contentio.Ref, parentFormat FormatType, child ContainerChildInfo, options *ParseOptions) *ContainerChildResource {
	childFormat := childFormatOrParent(child, parentFormat)
	return &ContainerChildResource{
		Name:          child.Name,
		Kind:          child.ChildKind,
		DataType:      child.DataType,
		Format:        childFormat,
		ResourceKind:  ContainerChildResourceNative,
		ParentReader:  parent,
		ParentRef:     parentRef,
		ParentFormat:  parentFormat,
		ParentOptions: options,
		Properties:    cloneStringInterfaceMap(child.Properties),
	}
}

func StreamContainerChildResource(reader contentio.Reader, ref contentio.Ref, child ContainerChildInfo) *ContainerChildResource {
	return &ContainerChildResource{
		Name:         child.Name,
		Kind:         child.ChildKind,
		DataType:     child.DataType,
		Format:       childFormatOrParent(child, FormatUnknown),
		Layout:       child.Layout,
		Refs:         childRelatedRefs(child),
		ResourceKind: ContainerChildResourceStream,
		Reader:       reader,
		Ref:          ref,
		Properties:   cloneStringInterfaceMap(child.Properties),
	}
}

func childFormatOrParent(child ContainerChildInfo, parentFormat FormatType) FormatType {
	if child.Format != "" {
		return child.Format
	}
	if child.Properties != nil {
		if formatName := strings.TrimSpace(interfaceString(child.Properties["format"])); formatName != "" {
			return FormatType(formatName)
		}
	}
	if parentFormat != "" {
		return parentFormat
	}
	return FormatUnknown
}

func childRelatedRefs(child ContainerChildInfo) []RelatedRef {
	if len(child.Refs) == 0 {
		return nil
	}
	refs := make([]RelatedRef, 0, len(child.Refs))
	for _, itemRef := range child.Refs {
		role := itemRef.Role
		if role == "" {
			role = contentio.RoleAuxiliary
		}
		contentRef := contentio.NewRef(itemRef.Path, role)
		refs = append(refs, NewRelatedRef(contentRef, itemRef.Required, itemRef.Primary))
	}
	return refs
}

func cloneStringInterfaceMap(input map[string]interface{}) map[string]interface{} {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]interface{}, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
