package image

import "math"

const tiffPageIFDLimit = 256
const tagSubfileType = 255

// describeTIFFPages uses the same bounded metadata windows as the TIFF summary.
// Only complete top-level directories establish a page count; masks, overviews
// and SubIFDs are not independent image pages.
func describeTIFFPages(data tiffMetadata) map[string]interface{} {
	result := map[string]interface{}{"page_summary_status": "invalid"}
	if isBigTIFF(data) {
		result["page_summary_status"] = "unsupported"
		return result
	}
	header := data.firstBytes(8)
	order, ok := data.byteOrder()
	if !ok || len(header) < 8 || order.Uint16(header[2:4]) != 42 {
		return result
	}
	offset := uint64(order.Uint32(header[4:8]))
	seen := make(map[uint64]bool)
	pages := make([]map[string]interface{}, 0)
	for index := 0; offset != 0; index++ {
		if seen[offset] || offset < 8 {
			return result
		}
		if index >= tiffPageIFDLimit {
			result["page_summary_status"] = "budget_exceeded"
			return result
		}
		seen[offset] = true
		countBytes, available := data.slice(offset, 2)
		if !available {
			result["page_summary_status"] = tiffMissingDirectoryStatus(data, offset, 2)
			return result
		}
		count := uint64(order.Uint16(countBytes))
		entries, available := data.slice(offset+2, count*12+4)
		if !available {
			result["page_summary_status"] = tiffMissingDirectoryStatus(data, offset+2, count*12+4)
			return result
		}
		tags := make(map[uint16]bool)
		for i := uint64(0); i < count; i++ {
			tag := order.Uint16(entries[i*12 : i*12+2])
			if tags[tag] {
				return result
			}
			tags[tag] = true
		}
		ifd, valid := parseIFDAt(data, order, offset)
		if !valid {
			return result
		}
		width, widthOK := tiffPageScalar(ifd, tagImageWidth, tiffTypeShort, tiffTypeLong)
		height, heightOK := tiffPageScalar(ifd, tagImageLength, tiffTypeShort, tiffTypeLong)
		if !widthOK || !heightOK || width == 0 || height == 0 ||
			uint64(width) > uint64(math.MaxInt) || uint64(height) > uint64(math.MaxInt) {
			return result
		}
		excluded := false
		if ifd.hasTag(tagNewSubfileType) {
			kind, valid := tiffPageScalar(ifd, tagNewSubfileType, tiffTypeLong)
			if !valid {
				return result
			}
			excluded = kind&5 != 0
		} else if ifd.hasTag(tagSubfileType) {
			kind, valid := tiffPageScalar(ifd, tagSubfileType, tiffTypeShort)
			if !valid || kind < 1 || kind > 3 {
				return result
			}
			excluded = kind == 2
		}
		if !excluded {
			pages = append(pages, map[string]interface{}{
				"ifd_index": index, "width": int(width), "height": int(height),
			})
		}
		offset = uint64(ifd.nextOffset)
	}
	if len(pages) == 0 {
		return result
	}
	result["page_summary_status"] = "parsed"
	result["page_count"] = len(pages)
	result["pages"] = pages
	return result
}

func tiffPageScalar(ifd *tiffIFD, tag uint16, types ...uint16) (uint32, bool) {
	entry, ok := ifd.tags[tag]
	if !ok || entry.count != 1 {
		return 0, false
	}
	for _, typ := range types {
		if entry.typ == typ {
			return ifd.firstLong(tag)
		}
	}
	return 0, false
}

func tiffMissingDirectoryStatus(data tiffMetadata, offset, size uint64) string {
	// A seekable tail establishes EOF; a short sequential prefix establishes
	// EOF as well. A full prefix or an uncovered middle window is inconclusive.
	if data.tail != nil {
		if offset+size > uint64(data.Len()) {
			return "invalid"
		}
	} else if len(data.head) < tiffMetadataReadLimit {
		return "invalid"
	}
	return "budget_exceeded"
}
