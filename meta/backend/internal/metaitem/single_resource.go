package metaitem

import (
	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/dataitem"
)

// SingleResourceInput 是 single 组织方式 item 推断的输入。
type SingleResourceInput struct {
	Name        string
	Path        string
	Size        int64
	ContentType string
	Format      string
}

// InferSingleResourceItem 基于一个资源推断基础 Meta item 语义。
func InferSingleResourceItem(file plugin.FileEntry) *DetectedItem {
	return InferSingleResource(SingleResourceInput{
		Name:        file.Name,
		Path:        file.Path,
		Size:        file.Size,
		ContentType: file.ContentType,
	})
}

// InferSingleResource 基于单个资源信息推断基础 Meta item 语义。
func InferSingleResource(input SingleResourceInput) *DetectedItem {
	formatName := dataitem.InferFormat(input.Name, input.ContentType, input.Format)
	organization := dataitem.OrganizationSingle
	dataType := dataitem.InferDataType(formatName, input.ContentType)
	if rule, ok := dataitem.MatchBuiltinSingleResourceRule(formatName); ok {
		organization = rule.Organization
		dataType = rule.DataType
	}
	item := &DetectedItem{
		Organization:   organization,
		DataType:       dataType,
		Format:         formatName,
		PhysicalPath:   input.Path,
		EntryPath:      input.Path,
		ComponentFiles: []string{input.Path},
		SizeBytes:      input.Size,
		Attributes: map[string]interface{}{
			"storage": map[string]interface{}{
				"path":         input.Path,
				"size":         input.Size,
				"content_type": input.ContentType,
			},
		},
	}
	return item
}
