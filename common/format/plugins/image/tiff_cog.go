package image

// Bounded metadata layout evidence, not full COG conformance or pixel decoding.
// No CRS/overview inference, GDAL dependency, payload search or negative default.
import (
	"bytes"
	"encoding/binary"
	"sort"
	"strconv"
	"strings"
)

const tiffCOGTileLimit = tiffMetadataReadLimit / 8

type tiffCOGTile struct{ start, end, area uint64 }

func hasTIFFCOGLayout(data tiffMetadata) bool {
	order, ok := data.byteOrder()
	header := data.firstBytes(8)
	if !ok || len(header) != 8 || order.Uint16(header[2:4]) != 42 || !data.sizeKnown {
		return false
	}
	first := uint64(order.Uint32(header[4:]))
	ghost, valid := tiffCOGGhost(data, first)
	if !valid {
		return false
	}
	queue := []uint64{first}
	seen := map[uint64]bool{}
	var tiles []tiffCOGTile
	var metadataEnd, directoryEnd, mainWidth, mainHeight uint64
	images, masks := map[[2]uint32]bool{}, map[[2]uint32]bool{}
	var previousLevel [2][2]uint32 // imagery and masks each list largest IFD first
	for len(queue) > 0 {
		if len(seen) >= tiffPageIFDLimit {
			return false
		}
		sort.Slice(queue, func(i, j int) bool { return queue[i] < queue[j] })
		offset := queue[0]
		queue = queue[1:]
		if seen[offset] || offset < 8 || offset < directoryEnd {
			return false
		}
		seen[offset] = true
		ifd, ok := parseIFDAt(data, order, offset)
		if !ok {
			return false
		}
		// parseIFDAt serves other metadata consumers; prove ordered unique tags
		// here instead of accepting its map overwrite behavior as layout proof.
		countRaw, _ := data.slice(offset, 2)
		count := uint64(order.Uint16(countRaw))
		raw, _ := data.slice(offset+2, count*12+4)
		directoryEnd = offset + 2 + count*12 + 4
		metadataEnd = max(metadataEnd, directoryEnd)
		var previous uint16
		for i := uint64(0); i < count; i++ {
			tag := order.Uint16(raw[i*12:])
			if i > 0 && tag <= previous {
				return false
			}
			previous = tag
			entry := ifd.tags[tag]
			width := tiffTypeSize(entry.typ)
			if width == 0 {
				return false
			}
			size := uint64(width) * uint64(entry.count)
			if size > 4 {
				if entry.value < 8 {
					return false
				}
				if _, ok := data.slice(uint64(entry.value), size); !ok {
					return false
				}
				metadataEnd = max(metadataEnd, uint64(entry.value)+size)
			}
		}
		w, wok := tiffPageScalar(ifd, tagImageWidth, tiffTypeShort, tiffTypeLong)
		h, hok := tiffPageScalar(ifd, tagImageLength, tiffTypeShort, tiffTypeLong)
		tw, twok := tiffPageScalar(ifd, tagTileWidth, tiffTypeShort, tiffTypeLong)
		th, thok := tiffPageScalar(ifd, tagTileLength, tiffTypeShort, tiffTypeLong)
		if !wok || !hok || !twok || !thok || w == 0 || h == 0 || tw == 0 || th == 0 || tw%16 != 0 || th%16 != 0 {
			return false
		}
		kind := uint32(0)
		if ifd.hasTag(tagNewSubfileType) {
			kind, ok = tiffPageScalar(ifd, tagNewSubfileType, tiffTypeLong)
			if !ok || kind & ^uint32(5) != 0 {
				return false
			}
		}
		if offset == first {
			if kind != 0 {
				return false
			}
			mainWidth, mainHeight = uint64(w), uint64(h)
		} else if kind == 0 || uint64(w) > mainWidth || uint64(h) > mainHeight || kind&1 != 0 && uint64(w) == mainWidth && uint64(h) == mainHeight {
			return false
		}
		dims := [2]uint32{w, h}
		role := 0
		if kind&4 != 0 {
			role = 1
		}
		if prior := previousLevel[role]; prior[0] != 0 && (w > prior[0] || h > prior[1]) {
			return false
		}
		previousLevel[role] = dims
		if kind&4 == 0 {
			if images[dims] {
				return false
			}
			images[dims] = true
		} else {
			if masks[dims] {
				return false
			}
			masks[dims] = true
		}
		blocks := ((uint64(w)-1)/uint64(tw) + 1) * ((uint64(h)-1)/uint64(th) + 1)
		samples, planar := uint32(1), uint32(1)
		for _, field := range []struct {
			tag    uint16
			target *uint32
		}{{277, &samples}, {284, &planar}} {
			if ifd.hasTag(field.tag) {
				*field.target, ok = tiffPageScalar(ifd, field.tag, tiffTypeShort, tiffTypeLong)
				if !ok || *field.target == 0 {
					return false
				}
			}
		}
		if planar != 1 && planar != 2 || kind&4 != 0 && samples != 1 {
			return false
		}
		if planar == 2 {
			if uint64(samples) > uint64(tiffCOGTileLimit)/blocks {
				return false
			}
			blocks *= uint64(samples)
		}
		if blocks > uint64(tiffCOGTileLimit-len(tiles)) {
			return false
		}
		offsets, offsetsOK := tiffCOGArray(ifd, tagTileOffsets)
		sizes, sizesOK := tiffCOGArray(ifd, tagTileByteCounts)
		if !offsetsOK || !sizesOK || uint64(len(offsets)) != blocks || len(offsets) != len(sizes) {
			return false
		}
		var lastEnd uint64
		for i, start := range offsets {
			size := sizes[i]
			if start == 0 || size == 0 || i > 0 && start < lastEnd {
				return false
			}
			end := start + size
			if ghost {
				if start < 4 || size < 4 {
					return false
				}
				if leader, ok := data.slice(start-4, 4); ok && uint64(binary.LittleEndian.Uint32(leader)) != size {
					return false
				}
				trailer, available := data.slice(end, 4)
				last, lastAvailable := data.slice(end-4, 4)
				if available && lastAvailable && !bytes.Equal(trailer, last) {
					return false
				}
				start, end = start-4, end+4
			}
			if end > uint64(data.Len()) {
				return false
			}
			lastEnd = end
			tiles = append(tiles, tiffCOGTile{start, end, uint64(w) * uint64(h)})
		}
		links, ok := tiffDirectoryLinks(ifd)
		if !ok || len(links)+len(queue)+len(seen) > tiffPageIFDLimit {
			return false
		}
		for _, link := range links {
			if link <= offset {
				return false
			}
			queue = append(queue, link)
		}
	}
	for dims := range masks {
		if !images[dims] {
			return false
		}
	}
	if len(tiles) == 0 {
		return false
	}
	sort.Slice(tiles, func(i, j int) bool { return tiles[i].start < tiles[j].start })
	if tiles[0].start < metadataEnd {
		return false
	}
	for i := 1; i < len(tiles); i++ {
		if tiles[i].start < tiles[i-1].end || tiles[i].area < tiles[i-1].area {
			return false
		}
	}
	return true
}

func tiffCOGArray(ifd *tiffIFD, tag uint16) ([]uint64, bool) {
	entry, ok := ifd.tags[tag]
	validType := entry.typ == tiffTypeLong || entry.typ == tiffTypeShort
	if tag == tagSubIFDs {
		validType = entry.typ == tiffTypeLong || entry.typ == tiffTypeIFD
	}
	if !ok || entry.count == 0 || entry.count > tiffCOGTileLimit || !validType {
		return nil, false
	}
	raw, ok := ifd.tagBytes(entry)
	if !ok {
		return nil, false
	}
	result := make([]uint64, entry.count)
	for i := range result {
		if entry.typ == tiffTypeShort {
			result[i] = uint64(ifd.order.Uint16(raw[i*2:]))
		} else {
			result[i] = uint64(ifd.order.Uint32(raw[i*4:]))
		}
	}
	return result, true
}

func tiffCOGGhost(data tiffMetadata, first uint64) (present, valid bool) {
	const prefix = "GDAL_STRUCTURAL_METADATA_SIZE="
	if len(data.head) < 8+len(prefix) || !bytes.Equal(data.head[8:8+len(prefix)], []byte(prefix)) {
		return false, true
	}
	header, ok := data.slice(8, 43)
	if !ok || string(header[len(prefix)+6:]) != " bytes\n" {
		return true, false
	}
	for _, digit := range header[len(prefix) : len(prefix)+6] {
		if digit < '0' || digit > '9' {
			return true, false
		}
	}
	size, err := strconv.ParseUint(string(header[len(prefix):len(prefix)+6]), 10, 32)
	end := uint64(51) + size
	if err != nil || size == 0 || first != (end+1)&^uint64(1) {
		return true, false
	}
	body, ok := data.slice(51, size)
	if !ok {
		return true, false
	}
	if first > end {
		pad, ok := data.slice(end, 1)
		if !ok || pad[0] != 0 {
			return true, false
		}
	}
	values := map[string]string{}
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		// GDAL reserves one space after the NO line, before either the next
		// option or the end of the block. It is not general whitespace repair.
		if strings.HasPrefix(line, " ") {
			if i == 0 || lines[i-1] != "KNOWN_INCOMPATIBLE_EDITION=NO" {
				return true, false
			}
			line = line[1:]
		}
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		_, duplicate := values[key]
		if !ok || duplicate {
			return true, false
		}
		values[key] = value
	}
	for key, want := range map[string]string{"LAYOUT": "IFDS_BEFORE_DATA", "BLOCK_ORDER": "ROW_MAJOR", "BLOCK_LEADER": "SIZE_AS_UINT4", "BLOCK_TRAILER": "LAST_4_BYTES_REPEATED", "KNOWN_INCOMPATIBLE_EDITION": "NO"} {
		if values[key] != want {
			return true, false
		}
		delete(values, key)
	}
	for key, value := range values {
		if !(key == "MASK_INTERLEAVED_WITH_IMAGERY" && value == "YES") && !(key == "INTERLEAVE" && (value == "BAND" || value == "TILE")) {
			return true, false
		}
	}
	return true, true
}
