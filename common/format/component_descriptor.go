package format

import (
	"strings"

	"github.com/addp/common/resource"
)

func DescribeComponents(formatType FormatType, components []resource.ComponentRef) []ComponentDescriptor {
	plugin, err := GetFormatPlugin(formatType)
	if err == nil {
		if provider, ok := plugin.(ComponentDescriptorProvider); ok {
			return provider.DescribeComponents(components)
		}
	}
	return defaultComponentDescriptors(components)
}

func defaultComponentDescriptors(components []resource.ComponentRef) []ComponentDescriptor {
	descriptors := make([]ComponentDescriptor, 0, len(components))
	for _, component := range components {
		extension := strings.ToLower(strings.TrimSpace(resource.NormalizeExtension(resourceExtension(component.Path))))
		role := strings.TrimSpace(component.ComponentRole)
		key := role
		if key == "" {
			key = extension
		}
		label := role
		if label == "" {
			label = component.Name
		}
		descriptors = append(descriptors, ComponentDescriptor{
			Key:       key,
			Path:      component.Path,
			Role:      role,
			Label:     label,
			Required:  component.Required,
			Primary:   component.Role == resource.ResourceRoleMain,
			DataType:  FormatDataTypeFile,
			Format:    FormatUnknown,
			Extension: extension,
		})
	}
	return descriptors
}

func resourceExtension(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		switch path[i] {
		case '.':
			return path[i:]
		case '/', '\\':
			return ""
		}
	}
	return ""
}
