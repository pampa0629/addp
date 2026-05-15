package zip

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/addp/common/format"
	"github.com/addp/common/resource"
)

const defaultEntryLimit = 100

type Plugin struct {
	options *format.ParseOptions
}

func NewPlugin(opts *format.ParseOptions) *Plugin {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Plugin{options: opts}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatZIP
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:            "builtin-zip",
		Format:        p.Format(),
		I18nKey:       "format.zip",
		DataType:      format.FormatDataTypeContainer,
		Layouts:       []string{format.FormatLayoutSingle},
		ProviderHints: []string{format.FormatProviderContainer},
		Identification: format.FormatIdentification{
			Extensions: []string{".zip"},
			MimeTypes:  []string{"application/zip", "application/x-zip-compressed"},
		},
		Providers: format.FormatProviderDescriptor{ContainerInfo: true},
		ContentReaders: []string{
			string(format.ContentReaderRawContent),
			string(format.ContentReaderContainerEntry),
		},
		EngineFamilies: []string{format.EngineFamilyObject, format.EngineFamilyFile},
	}
}

func (p *Plugin) Capabilities() format.FormatCapability {
	capability, ok := format.GetFormatCapability(p.Format())
	if ok {
		return capability
	}
	return format.FormatCapability{
		Format:        p.Format(),
		DataType:      format.FormatDataTypeContainer,
		Layouts:       []string{format.FormatLayoutSingle},
		ProviderHints: []string{format.FormatProviderContainer},
		ContentReaders: []string{
			string(format.ContentReaderRawContent),
			string(format.ContentReaderContainerEntry),
		},
		Parse: true,
	}
}

func (p *Plugin) DescribeContainer(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.ContainerInfo, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("read zip input: %w", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	entryLimit := p.entryLimit(options)
	limited := entryLimit > 0
	childCapacity := len(reader.File)
	if limited && entryLimit < childCapacity {
		childCapacity = entryLimit
	}
	children := make([]format.ContainerChildInfo, 0, childCapacity)
	fileCount := 0
	dirCount := 0
	defaultChild := ""
	for _, entry := range sortedEntries(reader.File) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			continue
		}
		isDir := entry.FileInfo().IsDir()
		if isDir {
			dirCount++
		} else {
			fileCount++
		}
		if limited && len(children) >= entryLimit {
			continue
		}
		child := zipEntryToContainerChild(entry, isDir)
		if defaultChild == "" && !isDir {
			defaultChild = child.Name
		}
		children = append(children, child)
	}
	if defaultChild == "" && len(children) > 0 {
		defaultChild = children[0].Name
	}

	entryCount := fileCount + dirCount
	return &format.ContainerInfo{
		Format:        p.Format(),
		ChildCount:    entryCount,
		DefaultChild:  defaultChild,
		ResourceCount: 1,
		Children:      children,
		FormatInfo: map[string]interface{}{
			"entry_count":        entryCount,
			"file_count":         fileCount,
			"directory_count":    dirCount,
			"sampled_children":   len(children),
			"children_truncated": limited && entryCount > len(children),
		},
	}, nil
}

func (p *Plugin) ResolveContainerChild(ctx context.Context, parent resource.ResourceReader, parentRef resource.ResourceRef, child format.ContainerChildInfo, options *format.ParseOptions) (*format.ContainerChildResource, error) {
	if parent == nil {
		return nil, fmt.Errorf("zip child resolver requires parent resource reader")
	}
	entryPath := zipChildPath(child, options)
	if entryPath == "" {
		return nil, fmt.Errorf("zip child resolver requires child path")
	}
	if child.Kind == "directory" {
		return nil, fmt.Errorf("zip child %s is a directory", entryPath)
	}

	openEntry := func(openCtx context.Context) (io.ReadCloser, error) {
		parentReader, err := parent.Open(openCtx, parentRef)
		if err != nil {
			return nil, err
		}
		defer parentReader.Close()

		data, err := io.ReadAll(parentReader)
		if err != nil {
			return nil, fmt.Errorf("read zip parent: %w", err)
		}
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, fmt.Errorf("open zip parent: %w", err)
		}
		for _, entry := range reader.File {
			name := strings.Trim(strings.TrimSpace(entry.Name), "/")
			if name != entryPath || entry.FileInfo().IsDir() {
				continue
			}
			entryReader, err := entry.Open()
			if err != nil {
				return nil, err
			}
			return entryReader, nil
		}
		return nil, resource.ErrResourceNotFound
	}

	entryRef := resource.NewResourceRef(path.Join(parentRef.Path, entryPath), resource.ResourceRoleMain)
	size := int64(0)
	if rawSize, ok := child.Properties["uncompressed_size"]; ok {
		size = interfaceInt64(rawSize)
	}
	childFormat := format.FormatType(strings.TrimSpace(formatInterfaceString(child.Properties["format"])))
	if childFormat == "" {
		childFormat = format.FormatUnknown
	}
	components := zipChildComponents(child, parentRef.Path)
	if len(components) > 0 {
		entryPath = zipPrimaryComponentPath(components, entryPath)
	}
	metadata := &resource.ResourceMetadata{
		Ref:          entryRef,
		Size:         size,
		Exists:       true,
		FormatHint:   string(childFormat),
		DataTypeHint: child.DataType,
	}
	reader := format.NewSingleResourceReader(entryRef, openEntry, metadata)
	if len(components) > 0 {
		reader = &zipChildResourceReader{
			parent:    parent,
			parentRef: parentRef,
			basePath:  parentRef.Path,
			metadata:  metadata,
		}
	}
	resolved := format.StreamContainerChildResource(reader, entryRef, child)
	resolved.Components = components
	return resolved, nil
}

type zipChildResourceReader struct {
	parent    resource.ResourceReader
	parentRef resource.ResourceRef
	basePath  string
	metadata  *resource.ResourceMetadata
}

func (r *zipChildResourceReader) Open(ctx context.Context, ref resource.ResourceRef) (io.ReadCloser, error) {
	entryPath := strings.Trim(strings.TrimPrefix(strings.Trim(ref.Path, "/"), strings.Trim(r.basePath, "/")), "/")
	if entryPath == "" {
		return nil, resource.ErrResourceNotFound
	}
	parentReader, err := r.parent.Open(ctx, r.parentRef)
	if err != nil {
		return nil, err
	}
	defer parentReader.Close()
	data, err := io.ReadAll(parentReader)
	if err != nil {
		return nil, fmt.Errorf("read zip parent: %w", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip parent: %w", err)
	}
	for _, entry := range reader.File {
		name := strings.Trim(strings.TrimSpace(entry.Name), "/")
		if name != entryPath || entry.FileInfo().IsDir() {
			continue
		}
		return entry.Open()
	}
	return nil, resource.ErrResourceNotFound
}

func (r *zipChildResourceReader) Stat(context.Context, resource.ResourceRef) (*resource.ResourceMetadata, error) {
	if r.metadata == nil {
		return nil, resource.ErrResourceNotFound
	}
	return r.metadata, nil
}

func (r *zipChildResourceReader) List(context.Context, resource.ResourceRef) ([]resource.ResourceRef, error) {
	return nil, nil
}

// entryLimit returns the default sampling limit when unspecified.
// An explicit zero means "unlimited", matching other container providers.
func (p *Plugin) entryLimit(options *format.ParseOptions) int {
	if options == nil {
		options = p.options
	}
	limit := defaultEntryLimit
	if options != nil && options.ExtraParams != nil {
		if value, ok := options.ExtraParams[format.ContainerChildLimitParam].(int); ok && value >= 0 {
			limit = value
		}
	}
	if limit < 0 {
		return defaultEntryLimit
	}
	return limit
}

func sortedEntries(entries []*zip.File) []*zip.File {
	result := append([]*zip.File(nil), entries...)
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func zipEntryToContainerChild(entry *zip.File, isDir bool) format.ContainerChildInfo {
	name := strings.Trim(strings.TrimSpace(entry.Name), "/")
	kind := "file"
	dataType := format.FormatDataTypeFile
	if isDir {
		kind = "directory"
		dataType = format.FormatDataTypeContainer
	}
	properties := map[string]interface{}{
		"path":              name,
		"compressed_size":   entry.CompressedSize64,
		"uncompressed_size": entry.UncompressedSize64,
		"method":            entry.Method,
	}
	if !isDir {
		childFormat := format.DetectFormat(filepath.Base(name), nil)
		if childFormat != format.FormatUnknown {
			properties["format"] = string(childFormat)
			if descriptor, ok := format.GetFormatDescriptor(childFormat); ok && descriptor.DataType != "" {
				dataType = descriptor.DataType
			}
		}
	}
	return format.ContainerChildInfo{
		Name:       name,
		Kind:       kind,
		DataType:   dataType,
		Properties: properties,
	}
}

func zipChildPath(child format.ContainerChildInfo, options *format.ParseOptions) string {
	if options != nil && options.ExtraParams != nil {
		if value := strings.Trim(strings.TrimSpace(formatInterfaceString(options.ExtraParams[format.ChildNameParam])), "/"); value != "" {
			return value
		}
	}
	if value := strings.Trim(strings.TrimSpace(formatInterfaceString(child.Properties["path"])), "/"); value != "" {
		return value
	}
	return strings.Trim(strings.TrimSpace(child.Name), "/")
}

func zipChildComponents(child format.ContainerChildInfo, basePath string) []resource.ComponentRef {
	if len(child.Components) == 0 {
		return nil
	}
	components := make([]resource.ComponentRef, 0, len(child.Components))
	for _, component := range child.Components {
		role := resource.ResourceRoleComponent
		if component.Primary {
			role = resource.ResourceRoleMain
		}
		componentPath := strings.TrimSpace(component.Path)
		if basePath != "" && componentPath != "" {
			componentPath = path.Join(basePath, componentPath)
		}
		components = append(components, resource.ComponentRef{
			ResourceRef:   resource.NewResourceRef(componentPath, role),
			ComponentRole: component.Role,
			Required:      component.Required,
		})
	}
	return components
}

func zipPrimaryComponentPath(components []resource.ComponentRef, fallback string) string {
	for _, component := range components {
		if component.ResourceRef.Role == resource.ResourceRoleMain {
			return component.Path
		}
	}
	for _, component := range components {
		if strings.TrimSpace(component.Path) != "" {
			return component.Path
		}
	}
	return fallback
}

func formatInterfaceString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}

func interfaceInt64(value interface{}) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case uint64:
		if typed > uint64(^uint64(0)>>1) {
			return 0
		}
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

func init() {
	_ = format.RegisterFormatPlugin(NewPlugin(nil))
}
