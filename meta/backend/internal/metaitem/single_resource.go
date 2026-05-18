package metaitem

import (
	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
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
	properties := map[string]interface{}{}
	if input.Format != "" {
		properties["format"] = input.Format
	}
	resolved, _ := dataitem.ResolveItems(dataitem.ResolveInput{
		ScopeKind: dataitem.ScopeKindDirectory,
		ScopePath: input.Path,
		Candidates: []dataitem.Candidate{{
			Name:        input.Name,
			Path:        input.Path,
			ContentType: input.ContentType,
			SizeBytes:   &input.Size,
			Properties:  properties,
		}},
		Options: dataitem.ResolveOptions{MaxItems: 1},
	})
	formatName := dataitem.InferFormat(input.Name, input.ContentType, input.Format)
	item := dataitem.ResolvedItem{
		Organization:  dataitem.OrganizationSingle,
		DataType:      dataitem.InferDataType(formatName, input.ContentType),
		Format:        formatName,
		EntryPath:     input.Path,
		RefList: ItemRefsFromPaths([]string{input.Path}),
		SizeBytes:     &input.Size,
	}
	if resolved != nil && len(resolved.Items) > 0 {
		item = resolved.Items[0]
	}
	detected := &DetectedItem{
		ResolvedItem: item,
		PhysicalPath: input.Path,
		Attributes: map[string]interface{}{
			"storage": map[string]interface{}{
				"path":         input.Path,
				"size":         input.Size,
				"content_type": input.ContentType,
			},
		},
	}
	return detected
}
