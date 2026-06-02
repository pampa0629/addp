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
	"github.com/addp/common/datatype"
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
		ID:       "builtin-zip",
		Format:   p.Format(),
		I18nKey:  "format.zip",
		DataType: datatype.DataTypeContainer,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions: []string{".zip"},
			MimeTypes:  []string{"application/zip", "application/x-zip-compressed"},
		},
	}
}

func (p *Plugin) DescribeFormat(ctx context.Context, input io.Reader, options *format.ParseOptions) (map[string]interface{}, error) {
	info, err := p.describeContainer(ctx, input, options)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"entry_count":        info.entryCount,
		"file_count":         info.fileCount,
		"directory_count":    info.dirCount,
		"sampled_children":   info.sampledChildren,
		"children_truncated": info.childrenTruncated,
	}, nil
}

func (p *Plugin) DescribeContainer(ctx context.Context, input io.Reader, options *format.ParseOptions) (*datatype.ContainerInfo, error) {
	info, err := p.describeContainer(ctx, input, options)
	if err != nil {
		return nil, err
	}
	return info.container, nil
}

type containerDescribeResult struct {
	container         *datatype.ContainerInfo
	entryCount        int
	fileCount         int
	dirCount          int
	sampledChildren   int
	childrenTruncated bool
}

func (p *Plugin) describeContainer(ctx context.Context, input io.Reader, options *format.ParseOptions) (*containerDescribeResult, error) {
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
	children := make([]datatype.ContainerChildInfo, 0, childCapacity)
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
	return &containerDescribeResult{
		container: &datatype.ContainerInfo{
			ChildCount:    entryCount,
			DefaultChild:  defaultChild,
			ResourceCount: 1,
			Children:      children,
		},
		entryCount:        entryCount,
		fileCount:         fileCount,
		dirCount:          dirCount,
		sampledChildren:   len(children),
		childrenTruncated: limited && entryCount > len(children),
	}, nil
}

func (p *Plugin) ResolveContainerChild(ctx context.Context, parent contentio.Reader, parentRef contentio.Ref, child datatype.ContainerChildInfo, options *format.ParseOptions) (*format.ContainerChildResource, error) {
	if parent == nil {
		return nil, fmt.Errorf("zip child resolver requires parent contentio.Reader")
	}
	entryPath := zipChildPath(child, options)
	if entryPath == "" {
		return nil, fmt.Errorf("zip child resolver requires child path")
	}
	if child.ChildKind == "directory" {
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
	if rawSize, ok := child.Native["uncompressed_size"]; ok {
		size = interfaceInt64(rawSize)
	}
	refs := zipChildRefs(child, parentRef.Path)
	if len(refs) > 0 {
		entryPath = zipPrimaryRefPath(refs, entryPath)
	}
	stat := &contentio.Stat{
		Ref:    entryRef,
		Size:   size,
		Exists: true,
	}
	reader := format.NewSingleContentReader(entryRef, openEntry, stat)
	if len(refs) > 0 {
		reader = &zipChildContentReader{
			parent:    parent,
			parentRef: parentRef,
			basePath:  parentRef.Path,
			stat:      stat,
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
	stat      *contentio.Stat
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

func (r *zipChildContentReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	if r.stat == nil {
		return nil, contentio.ErrContentNotFound
	}
	return r.stat, nil
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

func zipEntryToContainerChild(entry *zip.File, isDir bool) datatype.ContainerChildInfo {
	name := strings.Trim(strings.TrimSpace(entry.Name), "/")
	kind := "file"
	dataType := datatype.DataTypeUnknown
	childFormat := format.FormatUnknown
	if isDir {
		kind = "directory"
		dataType = datatype.DataTypeContainer
	}
	native := map[string]interface{}{
		"path":              name,
		"compressed_size":   entry.CompressedSize64,
		"uncompressed_size": entry.UncompressedSize64,
		"method":            entry.Method,
	}
	if !isDir {
		childFormat = format.DetectFormat(filepath.Base(name), nil)
		if childFormat != format.FormatUnknown {
			if descriptor, ok := format.GetFormatDescriptor(childFormat); ok && descriptor.DataType != "" {
				dataType = descriptor.DataType
			}
		}
	}
	formatName := ""
	if childFormat != format.FormatUnknown {
		formatName = string(childFormat)
	}
	return datatype.ContainerChildInfo{
		Name:      name,
		ChildKind: kind,
		DataType:  dataType,
		Format:    formatName,
		Native:    native,
	}
}

func zipChildPath(child datatype.ContainerChildInfo, options *format.ParseOptions) string {
	if options != nil && options.ExtraParams != nil {
		if value := strings.Trim(strings.TrimSpace(formatInterfaceString(options.ExtraParams[format.ChildNameParam])), "/"); value != "" {
			return value
		}
	}
	if value := strings.Trim(strings.TrimSpace(formatInterfaceString(child.Native["path"])), "/"); value != "" {
		return value
	}
	return strings.Trim(strings.TrimSpace(child.Name), "/")
}

func zipChildRefs(child datatype.ContainerChildInfo, basePath string) []format.RelatedRef {
	if len(child.Refs) == 0 {
		return nil
	}
	refs := make([]format.RelatedRef, 0, len(child.Refs))
	for _, itemRef := range child.Refs {
		role := itemRef.Role
		if role == "" {
			role = contentio.RoleAuxiliary
		}
		refPath := strings.TrimSpace(itemRef.Path)
		if basePath != "" && refPath != "" {
			refPath = path.Join(basePath, refPath)
		}
		contentRef := contentio.NewRef(refPath, role)
		refs = append(refs, format.NewRelatedRef(contentRef, itemRef.Required, itemRef.Primary))
	}
	return refs
}

func zipPrimaryRefPath(refs []format.RelatedRef, fallback string) string {
	primaryRef, err := format.PrimaryRelatedRef(refs)
	if err == nil {
		return primaryRef.Ref.Path
	}
	for _, ref := range refs {
		if strings.TrimSpace(ref.Ref.Path) != "" {
			return ref.Ref.Path
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
