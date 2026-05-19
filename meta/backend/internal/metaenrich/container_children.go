package metaenrich

import (
	"context"
	"io"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

const (
	containerChildLimit       = 100
	containerSampleChildLimit = 100
)

// EnrichContainerChildren 枚举容器内部对象，并写入 type_info.container.children。
func EnrichContainerChildren(ctx context.Context, attrs models.JSONMap, detected *metaitem.DetectedItem, reader io.Reader) error {
	if attrs == nil || detected == nil || reader == nil || detected.DataType != dataitem.DataTypeContainer {
		return nil
	}
	formatType := format.FormatType(detected.Format)
	if _, err := format.GetContainerInfoProvider(formatType); err != nil {
		return nil
	}
	return enrichContainerChildrenFromProvider(ctx, attrs, formatType, reader, format.ContainerParseOptions(containerChildLimit, 0))
}

func enrichContainerChildrenFromProvider(
	ctx context.Context,
	attrs models.JSONMap,
	formatType format.FormatType,
	reader io.Reader,
	options *format.ParseOptions,
) error {
	provider, err := format.GetContainerInfoProvider(formatType)
	if err != nil {
		return err
	}
	info, err := provider.DescribeContainer(ctx, reader, options)
	if err != nil {
		return err
	}
	if info == nil {
		return nil
	}
	info = resolveContainerChildren(info)

	children := make([]map[string]interface{}, 0, len(info.Children))
	for _, child := range info.Children {
		attrs := map[string]interface{}{
			"name":       child.Name,
			"child_kind": child.ChildKind,
			"data_type":  child.DataType,
		}
		if child.Format != "" {
			attrs["format"] = string(child.Format)
		}
		if child.Layout != "" {
			attrs["layout"] = child.Layout
		}
		if len(child.Refs) > 0 {
			attrs["refs"] = containerChildRefAttributes(child.Refs)
		}
		if child.RowCount != nil {
			attrs["row_count"] = *child.RowCount
		}
		if child.ColumnCount != nil {
			attrs["column_count"] = *child.ColumnCount
		}
		if child.HasHeader != nil {
			attrs["has_header"] = *child.HasHeader
		}
		for key, value := range child.Properties {
			if isContainerChildProtocolProperty(key) {
				continue
			}
			attrs[key] = value
		}
		children = append(children, attrs)
	}
	metaattr.UpsertNested(attrs, "type_info", "container", map[string]interface{}{
		"children":       children,
		"child_count":    info.ChildCount,
		"default_child":  info.DefaultChild,
		"resource_count": info.ResourceCount,
	})
	if len(info.FormatInfo) > 0 {
		metaattr.UpsertNested(attrs, "format_info", string(formatType), info.FormatInfo)
	}
	return nil
}

func containerChildRefAttributes(refs []format.ContainerChildRef) []map[string]interface{} {
	if len(refs) == 0 {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(refs))
	for _, ref := range refs {
		item := map[string]interface{}{
			"path":     ref.Path,
			"required": ref.Required,
			"primary":  ref.Primary,
		}
		if ref.Role != "" {
			item["role"] = ref.Role
		}
		if ref.Extension != "" {
			item["extension"] = ref.Extension
		}
		result = append(result, item)
	}
	return result
}

func isContainerChildProtocolProperty(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "name", "child_kind", "data_type", "format", "layout", "refs", "ref_paths", "components", "component_paths", "organization":
		return true
	default:
		return false
	}
}

func resolveContainerChildren(info *format.ContainerInfo) *format.ContainerInfo {
	if info == nil || len(info.Children) == 0 {
		return info
	}
	candidates := make([]dataitem.Candidate, 0, len(info.Children))
	for _, child := range info.Children {
		kind := strings.ToLower(strings.TrimSpace(child.ChildKind))
		if kind == "directory" {
			continue
		}
		if kind != "" && kind != "file" && kind != "object" && kind != "entry" && kind != "multi" {
			continue
		}
		pathValue := commonJSON.InterfaceString(child.Properties["path"])
		if pathValue == "" {
			pathValue = child.Name
		}
		var sizePtr *int64
		if size := commonJSON.InterfaceInt64(child.Properties["uncompressed_size"]); size > 0 {
			sizePtr = &size
		}
		props := map[string]interface{}{}
		for key, value := range child.Properties {
			props[key] = value
		}
		if child.Format != "" {
			props["format"] = string(child.Format)
		}
		candidates = append(candidates, dataitem.Candidate{
			Path:       pathValue,
			Name:       child.Name,
			SizeBytes:  sizePtr,
			Properties: props,
		})
	}
	if len(candidates) == 0 {
		return info
	}
	resolved, err := dataitem.ResolveItems(dataitem.ResolveInput{
		ScopeKind:  dataitem.ScopeKindContainer,
		Candidates: candidates,
		Options: dataitem.ResolveOptions{
			IncludeIgnored: true,
		},
	})
	if err != nil || resolved == nil || len(resolved.Items) == 0 {
		return info
	}
	next := *info
	next.Children = make([]format.ContainerChildInfo, 0, len(resolved.Items))
	for _, item := range resolved.Items {
		next.Children = append(next.Children, dataitem.ContainerChildInfoFromResolvedItem(item))
	}
	next.DefaultChild = next.Children[0].Name
	if next.FormatInfo == nil {
		next.FormatInfo = map[string]interface{}{}
	}
	next.FormatInfo["resolved_children"] = true
	next.FormatInfo["raw_child_count"] = info.ChildCount
	next.FormatInfo["visible_child_count"] = len(next.Children)
	next.FormatInfo["ignored_child_count"] = len(resolved.Ignored)
	return &next
}
