package format

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/addp/common/resource"
)

const (
	ContainerChildResourceStream = "stream"
	ContainerChildResourceNative = "native"
)

// ContainerChildResource is a resolved nested resource inside a container.
//
// Stream children expose an independent ResourceReader/ResourceRef, for example
// a ZIP entry. Native children reuse the parent resource with format-specific
// options, for example an Excel sheet or SQLite table.
type ContainerChildResource struct {
	Name          string
	Kind          string
	DataType      string
	Format        FormatType
	Organization  string
	Components    []resource.ComponentRef
	ResourceKind  string
	Reader        resource.ResourceReader
	Ref           resource.ResourceRef
	ParentReader  resource.ResourceReader
	ParentRef     resource.ResourceRef
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

// ContainerChildResolver resolves a child locator into a nested resource.
//
// The resolver does not connect to engines. The caller supplies the parent
// ResourceReader and ResourceRef that already include permissions, credentials,
// auditing and storage-specific read behavior.
type ContainerChildResolver interface {
	ContentReader
	ResolveContainerChild(ctx context.Context, parent resource.ResourceReader, parentRef resource.ResourceRef, child ContainerChildInfo, options *ParseOptions) (*ContainerChildResource, error)
}

func NativeContainerChildResource(parent resource.ResourceReader, parentRef resource.ResourceRef, parentFormat FormatType, child ContainerChildInfo, options *ParseOptions) *ContainerChildResource {
	childFormat := childFormatOrParent(child, parentFormat)
	return &ContainerChildResource{
		Name:          child.Name,
		Kind:          child.Kind,
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

func StreamContainerChildResource(reader resource.ResourceReader, ref resource.ResourceRef, child ContainerChildInfo) *ContainerChildResource {
	return &ContainerChildResource{
		Name:         child.Name,
		Kind:         child.Kind,
		DataType:     child.DataType,
		Format:       childFormatOrParent(child, FormatUnknown),
		Organization: child.Organization,
		Components:   childResourceComponents(child),
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

func childResourceComponents(child ContainerChildInfo) []resource.ComponentRef {
	if len(child.Components) == 0 {
		return nil
	}
	components := make([]resource.ComponentRef, 0, len(child.Components))
	for _, component := range child.Components {
		role := resource.ResourceRoleComponent
		if component.Primary {
			role = resource.ResourceRoleMain
		}
		components = append(components, resource.ComponentRef{
			ResourceRef:   resource.NewResourceRef(component.Path, role),
			ComponentRole: component.Role,
			Required:      component.Required,
		})
	}
	return components
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
