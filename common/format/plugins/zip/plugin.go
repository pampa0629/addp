package zip

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

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
	descriptor, ok := format.GetFormatDescriptor(p.Format())
	if ok {
		return descriptor
	}
	return format.FormatDescriptor{
		ID:            "builtin-zip",
		Format:        p.Format(),
		DataType:      format.FormatDataTypeContainer,
		Layouts:       []string{format.FormatLayoutSingle},
		ProviderHints: []string{format.FormatProviderContainer},
		ContentReaders: []string{
			string(format.ContentReaderRawContent),
		},
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
		Parse:         true,
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

func init() {
	_ = format.RegisterFormatPlugin(NewPlugin(nil))
}
