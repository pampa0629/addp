package format

import (
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
)

func DescribeRefs(formatType FormatType, refs []RelatedRef) []RefDescriptor {
	plugin, err := GetFormatPlugin(formatType)
	if err == nil {
		if provider, ok := plugin.(RefDescriptorProvider); ok {
			return provider.DescribeRefs(refs)
		}
	}
	return defaultRefDescriptors(refs)
}

func defaultRefDescriptors(refs []RelatedRef) []RefDescriptor {
	descriptors := make([]RefDescriptor, 0, len(refs))
	for _, ref := range refs {
		contentRef := ref.Ref
		extension := strings.ToLower(strings.TrimSpace(NormalizeExtension(resourceExtension(contentRef.Path))))
		role := strings.TrimSpace(contentRef.Role)
		key := role
		if key == "" {
			key = extension
		}
		label := role
		if label == "" {
			label = contentio.BaseName(ref.Ref)
		}
		descriptors = append(descriptors, RefDescriptor{
			Key:       key,
			Path:      contentRef.Path,
			Role:      role,
			Label:     label,
			Required:  ref.Required,
			Primary:   ref.Primary,
			DataType:  datatype.DataTypeFile,
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
