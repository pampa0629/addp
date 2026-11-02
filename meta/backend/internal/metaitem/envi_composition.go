package metaitem

import (
	"bytes"
	"context"
	"io"
	"math"
	"path"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/envi"
)

// ENVI identification is structural in both scan depths. It consumes only
// observed leaves, never guesses sibling paths or reads all pixel values.
func resolveENVIPairs(ctx context.Context, input DirectoryResolveInput, files []StorageFileRef) []*DetectedItem {
	if input.StructureReader == nil || (input.StructureReader.Type() != "nfs" && input.StructureReader.Type() != "minio") {
		return nil
	}
	observed := make([]envi.File, 0, len(files))
	headers := map[string]*envi.Header{}
	for _, file := range files {
		observed = append(observed, envi.File{Path: file.Path, Size: file.Size})
		if !strings.EqualFold(path.Ext(file.Path), ".hdr") || file.Size <= 0 {
			continue
		}
		reader, err := fileStructureReaderAt(ctx, input, file)
		if err != nil {
			continue
		}
		length := file.Size
		if length > envi.MaxHeaderBytes {
			length = 6
		}
		raw, err := io.ReadAll(io.NewSectionReader(reader, 0, length))
		if err != nil || int64(len(raw)) != length {
			continue
		}
		if !bytes.HasPrefix(raw, []byte("ENVI\n")) && !bytes.HasPrefix(raw, []byte("ENVI\r\n")) {
			continue
		}
		// A known but unsupported header still identifies an ENVI candidate. It
		// must not downgrade the primary to an unguarded single-file IMG read.
		headers[file.Path], _ = envi.ParseHeader(ctx, raw)
	}
	var result []*DetectedItem
	for _, main := range files {
		candidates := []string{}
		for name := range headers {
			if envi.MatchesHeader(main.Path, name) {
				candidates = append(candidates, name)
			}
		}
		if len(candidates) == 0 {
			continue
		}
		reader, err := fileStructureReaderAt(ctx, input, main)
		if err != nil {
			continue
		}
		prefixSize := min(main.Size, int64(64))
		if prefixSize < 0 {
			continue
		}
		prefix, err := io.ReadAll(io.NewSectionReader(reader, 0, prefixSize))
		if err != nil || int64(len(prefix)) != prefixSize {
			continue
		}
		identified := format.DetectFormat("", prefix)
		if identified != format.FormatUnknown && identified != format.FormatText && identified != format.FormatJSON && identified != format.FormatXML {
			continue
		}
		refs := []dataitem.ItemRef{{Path: main.Path, Role: "raster", Required: true, Primary: true, Extension: strings.ToLower(path.Ext(main.Path))}}
		claims := []string{main.Path}
		total := main.Size
		reason := "envi_unproven_pair"
		native := map[string]interface{}{"profile": envi.Profile, "composition_complete": false}
		if len(candidates) == 1 {
			name := candidates[0]
			header := headers[name]
			pair, err := envi.ResolvePair(name, header, observed)
			if err == nil && pair.Raster.Path == main.Path && pair.Raster.Size <= math.MaxInt64-pair.Header.Size {
				refs = append(refs, dataitem.ItemRef{Path: name, Role: "header", Required: true, Extension: strings.ToLower(path.Ext(name))})
				claims = append(claims, name)
				total += pair.Header.Size
				reason = "envi_header_pair"
				native = map[string]interface{}{"profile": envi.Profile, "composition_complete": true, "header_path": name, "header_sha256": header.SHA256, "header_size": pair.Header.Size, "raster_size": main.Size,
					"samples": header.Samples, "lines": header.Lines, "band_count": header.Bands, "data_type": header.DataType, "byte_order": header.ByteOrder, "header_offset": header.HeaderOffset, "interleave": header.Interleave}
			}
		}
		item := detectedItemFromResolvedItem(main.Path, dataitem.ResolvedItem{Name: main.Name, FullName: main.Path, Layout: format.LayoutMulti, DataType: datatype.Media, Format: string(format.FormatENVI), PrimaryContentPath: main.Path, RefList: refs, ClaimPaths: claims, SizeBytes: &total, DetectionReason: reason})
		item.Attributes = map[string]interface{}{"format_info": map[string]interface{}{"envi": native}}
		result = append(result, item)
	}
	return result
}
