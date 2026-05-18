package format

import (
	"strings"

	"github.com/addp/common/contentio"
)

func DescribeRefs(formatType FormatType, refs []contentio.Ref) []RefDescriptor {
	plugin, err := GetFormatPlugin(formatType)
	if err == nil {
		if provider, ok := plugin.(RefDescriptorProvider); ok {
			return provider.DescribeRefs(refs)
		}
	}
	return defaultRefDescriptors(refs)
}

func defaultRefDescriptors(refs []contentio.Ref) []RefDescriptor {
	descriptors := make([]RefDescriptor, 0, len(refs))
	for _, ref := range refs {
		extension := strings.ToLower(strings.TrimSpace(contentio.NormalizeExtension(resourceExtension(ref.Path))))
		role := strings.TrimSpace(ref.Role)
		key := role
		if key == "" {
			key = extension
		}
		label := role
		if label == "" {
			label = ref.Name
		}
		descriptors = append(descriptors, RefDescriptor{
			Key:       key,
			Path:      ref.Path,
			Role:      role,
			Label:     label,
			Required:  ref.Required,
			Primary:   ref.Primary || ref.Role == contentio.RoleMain,
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
