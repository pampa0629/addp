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

	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
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

func (p *Plugin) ResolveContainerChild(ctx context.Context, parent contentio.Reader, parentRef contentio.Ref, child format.ContainerChildInfo, options *format.ParseOptions) (*format.ContainerChildResource, error) {
	if parent == nil {
		return nil, fmt.Errorf("zip child resolver requires parent contentio.Reader")
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
		return nil, contentio.ErrContentNotFound
	}

	entryRef := contentio.NewRef(path.Join(parentRef.Path, entryPath), contentio.RoleMain)
	size := int64(0)
	if rawSize, ok := child.Properties["uncompressed_size"]; ok {
		size = interfaceInt64(rawSize)
	}
	childFormat := format.FormatType(strings.TrimSpace(formatInterfaceString(child.Properties["format"])))
	if childFormat == "" {
		childFormat = format.FormatUnknown
	}
	refs := zipChildRefs(child, parentRef.Path)
	if len(refs) > 0 {
		entryPath = zipPrimaryRefPath(refs, entryPath)
	}
	metadata := &contentio.Metadata{
		Ref:          entryRef,
		Size:         size,
		Exists:       true,
		FormatHint:   string(childFormat),
		DataTypeHint: child.DataType,
	}
	reader := format.NewSingleContentReader(entryRef, openEntry, metadata)
	if len(refs) > 0 {
		reader = &zipChildContentReader{
			parent:    parent,
			parentRef: parentRef,
			basePath:  parentRef.Path,
			metadata:  metadata,
		}
	}
	resolved := format.StreamContainerChildResource(reader, entryRef, child)
	resolved.Refs = refs
	return resolved, nil
}

type zipChildContentReader struct {
	parent    contentio.Reader
	parentRef contentio.Ref
	basePath  string
	metadata  *contentio.Metadata
}

func (r *zipChildContentReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	entryPath := strings.Trim(strings.TrimPrefix(strings.Trim(ref.Path, "/"), strings.Trim(r.basePath, "/")), "/")
	if entryPath == "" {
		return nil, contentio.ErrContentNotFound
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
	return nil, contentio.ErrContentNotFound
}

func (r *zipChildContentReader) Stat(context.Context, contentio.Ref) (*contentio.Metadata, error) {
	if r.metadata == nil {
		return nil, contentio.ErrContentNotFound
	}
	return r.metadata, nil
}

func (r *zipChildContentReader) List(context.Context, contentio.Ref) ([]contentio.Ref, error) {
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

func zipChildRefs(child format.ContainerChildInfo, basePath string) []contentio.Ref {
	if len(child.Refs) == 0 {
		return nil
	}
	refs := make([]contentio.Ref, 0, len(child.Refs))
	for _, itemRef := range child.Refs {
		role := contentio.RoleAuxiliary
		if itemRef.Primary {
			role = contentio.RoleMain
		}
		refPath := strings.TrimSpace(itemRef.Path)
		if basePath != "" && refPath != "" {
			refPath = path.Join(basePath, refPath)
		}
		contentRef := contentio.NewRef(refPath, role)
		if itemRef.Role != "" {
			contentRef.Role = itemRef.Role
		}
		contentRef.Required = itemRef.Required
		contentRef.Primary = itemRef.Primary
		refs = append(refs, contentRef)
	}
	return refs
}

func zipPrimaryRefPath(refs []contentio.Ref, fallback string) string {
	for _, ref := range refs {
		if ref.Role == contentio.RoleMain || ref.Primary {
			return ref.Path
		}
	}
	for _, ref := range refs {
		if strings.TrimSpace(ref.Path) != "" {
			return ref.Path
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
