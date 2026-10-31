package skp

// This private reader establishes container integrity only. It is deliberately
// not a Model3DCompositionProvider: model, XML and nested-resource semantics
// still have to be proved before this plugin can advertise composition facts.

import (
	"bufio"
	"bytes"
	"compress/flate"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type vffReadLimits struct {
	sourceBytes, readBytes, expandedBytes, memberBytes int64
	members                                            int
}

type vffContainer struct {
	version string
	schema  uint16
	members map[string][]byte
}

type vffSource struct {
	ctx             context.Context
	input           io.ReaderAt
	size, remaining int64
}

func (s *vffSource) ReadAt(p []byte, offset int64) (int, error) {
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	if offset < 0 {
		return 0, fmt.Errorf("negative VFF read offset")
	}
	if len(p) == 0 {
		return 0, nil
	}
	if offset >= s.size {
		return 0, io.EOF
	}
	length := int64(len(p))
	if length > s.size-offset {
		length = s.size - offset
	}
	if length > s.remaining {
		return 0, fmt.Errorf("VFF cumulative source read budget exceeded")
	}
	s.remaining -= length
	n, err := s.input.ReadAt(p[:int(length)], offset)
	if check := s.ctx.Err(); check != nil {
		return n, check
	}
	if n < 0 || int64(n) > length {
		return 0, fmt.Errorf("invalid VFF source read count")
	}
	if int64(n) != length && err == nil {
		err = io.ErrUnexpectedEOF
	}
	if n == int(length) && int64(len(p)) > length && err == nil {
		err = io.EOF
	}
	return n, err
}

func (s *vffSource) at(offset, length int64) ([]byte, error) {
	if offset < 0 || length < 0 || offset > s.size || length > s.size-offset || length > s.remaining {
		return nil, fmt.Errorf("invalid or over-budget VFF byte range")
	}
	data := make([]byte, int(length))
	n, err := s.ReadAt(data, offset)
	if err != nil || n != len(data) {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("read complete VFF range: %w", err)
	}
	return data, nil
}

func readVFFContainer(ctx context.Context, input io.ReaderAt, size int64, limits vffReadLimits) (*vffContainer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input == nil || size < 61+22 || limits.sourceBytes <= 0 || size > limits.sourceBytes ||
		limits.readBytes <= 0 || limits.expandedBytes <= 0 || limits.memberBytes <= 0 || limits.members <= 0 {
		return nil, fmt.Errorf("VFF requires an owned primary and explicit positive read budgets")
	}
	source := &vffSource{ctx: ctx, input: input, size: size, remaining: limits.readBytes}
	version, schema, checksum, base, err := readVFFHeader(source)
	if err != nil {
		return nil, err
	}
	records, directory, err := readVFFDirectory(source, base, limits, vffModelZIP)
	if err != nil {
		return nil, err
	}
	if foldedVFFCRC(records) != checksum {
		return nil, fmt.Errorf("VFF directory checksum mismatch")
	}
	members, err := readVFFZIPMembers(source, base, records, directory, vffModelZIP)
	if err != nil {
		return nil, err
	}
	result := &vffContainer{version: version, schema: schema, members: members}
	if len(result.members["model.dat"]) == 0 {
		return nil, fmt.Errorf("VFF requires one nonempty model.dat member")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// One physical framing/decompression path for the model envelope and its SKC
// members. Profile-specific differences never relax the outer VFF contract.
func readVFFZIPMembers(source *vffSource, base int64, records []vffZIPRecord, directory int64, profile vffZIPProfile) (map[string][]byte, error) {
	physical := append([]vffZIPRecord(nil), records...)
	sort.Slice(physical, func(i, j int) bool { return physical[i].offset < physical[j].offset })
	next := base
	for i := range physical {
		record := &physical[i]
		if record.offset != next {
			return nil, fmt.Errorf("VFF ZIP has overlapping, hidden or unclaimed bytes")
		}
		var err error
		next, err = validateVFFLocal(source, record, profile)
		if err != nil {
			return nil, err
		}
	}
	if next != directory {
		return nil, fmt.Errorf("VFF ZIP local entries do not cover the exact archive range")
	}
	members := make(map[string][]byte, len(records))
	for _, record := range physical {
		data, err := readVFFMember(source, record)
		if err != nil {
			return nil, err
		}
		members[record.name] = data
	}
	if err := source.ctx.Err(); err != nil {
		return nil, err
	}
	return members, nil
}

func readVFFHeader(source *vffSource) (string, uint16, uint32, int64, error) {
	prefix, err := source.at(0, 36)
	if err != nil {
		return "", 0, 0, 0, err
	}
	label := []byte{0xff, 0xfe, 0xff, 14}
	for _, r := range "SketchUp Model" {
		label = append(label, byte(r), 0)
	}
	if !bytes.Equal(prefix[:32], label) || !bytes.Equal(prefix[32:35], []byte{0xff, 0xfe, 0xff}) {
		return "", 0, 0, 0, fmt.Errorf("unsupported VFF string header; legacy MFC is outside this reader")
	}
	units := int(prefix[35])
	if units < 7 || units > 64 {
		return "", 0, 0, 0, fmt.Errorf("unsupported VFF version string length")
	}
	tail, err := source.at(36, int64(units*2+13))
	if err != nil {
		return "", 0, 0, 0, err
	}
	chars := make([]uint16, units)
	for i := range chars {
		chars[i] = binary.LittleEndian.Uint16(tail[i*2:])
		if chars[i] > 127 {
			return "", 0, 0, 0, fmt.Errorf("invalid VFF version string")
		}
	}
	version := string(utf16.Decode(chars))
	if !strings.HasPrefix(version, "{") || !strings.HasSuffix(version, "}") {
		return "", 0, 0, 0, fmt.Errorf("invalid VFF version delimiters")
	}
	parts := strings.Split(version[1:len(version)-1], ".")
	if len(parts) != 3 {
		return "", 0, 0, 0, fmt.Errorf("invalid VFF version components")
	}
	for _, part := range parts {
		if part == "" || strings.IndexFunc(part, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return "", 0, 0, 0, fmt.Errorf("invalid VFF version component")
		}
		if _, err := strconv.ParseUint(part, 10, 32); err != nil {
			return "", 0, 0, 0, fmt.Errorf("overlong VFF version component")
		}
	}
	block := tail[units*2:]
	if !bytes.Equal(block[:3], []byte("VFF")) || binary.LittleEndian.Uint16(block[3:]) != 8 || binary.LittleEndian.Uint16(block[5:]) != 1 {
		return "", 0, 0, 0, fmt.Errorf("unsupported VFF header block")
	}
	schema := binary.LittleEndian.Uint16(block[7:])
	if schema == 0 {
		return "", 0, 0, 0, fmt.Errorf("invalid VFF schema identifier")
	}
	return version, schema, binary.LittleEndian.Uint32(block[9:]), int64(36 + len(tail)), nil
}

type vffZIPRecord struct {
	name                         string
	flags, method, needed        uint16
	crc                          uint32
	compressed, expanded, offset int64
	dataOffset                   int64
	directory                    bool
}

type vffZIPProfile uint8

const (
	vffModelZIP vffZIPProfile = iota + 1
	vffClassificationZIP
)

// Read-only, hidden, system, directory, archive, normal. Other DOS/Windows
// attributes (including reparse points) are outside the SKC container profile.
const skcDOSAttributes = uint32(0x01 | 0x02 | 0x04 | 0x10 | 0x20 | 0x80)

func vffMemberName(name []byte, flags uint16) error {
	if !utf8.Valid(name) || len(name) == 0 || name[0] == '/' || bytes.ContainsAny(name, "\\:\x00") {
		return fmt.Errorf("invalid VFF member name")
	}
	// The two readers must agree on filename decoding. Python's ZIP reader
	// decodes non-UTF8 names as CP437; do not silently interpret them as UTF8.
	if flags&0x800 == 0 {
		for _, b := range name {
			if b >= 128 {
				return fmt.Errorf("ambiguous VFF ZIP filename encoding")
			}
		}
	}
	for _, part := range strings.Split(string(name), "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("noncanonical VFF member path")
		}
	}
	return nil
}

func readVFFDirectory(source *vffSource, base int64, limits vffReadLimits, profile vffZIPProfile) ([]vffZIPRecord, int64, error) {
	if profile != vffModelZIP && profile != vffClassificationZIP {
		return nil, 0, fmt.Errorf("unknown VFF ZIP profile")
	}
	end, err := source.at(source.size-22, 22)
	if err != nil {
		return nil, 0, err
	}
	if binary.LittleEndian.Uint32(end) != 0x06054b50 || binary.LittleEndian.Uint16(end[4:]) != 0 || binary.LittleEndian.Uint16(end[6:]) != 0 || binary.LittleEndian.Uint16(end[20:]) != 0 {
		return nil, 0, fmt.Errorf("VFF requires one complete ZIP directory without archive comments")
	}
	count := binary.LittleEndian.Uint16(end[10:])
	if count == 0 || count == 0xffff || int(count) > limits.members || count != binary.LittleEndian.Uint16(end[8:]) {
		return nil, 0, fmt.Errorf("VFF member count exceeds budget or ZIP profile")
	}
	length := int64(binary.LittleEndian.Uint32(end[12:]))
	offset := int64(binary.LittleEndian.Uint32(end[16:]))
	if length == 0xffffffff || offset == 0xffffffff || base+offset > source.size-22 || length != source.size-22-base-offset {
		return nil, 0, fmt.Errorf("invalid VFF central directory bounds")
	}
	directory := base + offset
	data, err := source.at(directory, length)
	if err != nil {
		return nil, 0, err
	}
	records := make([]vffZIPRecord, 0, int(count))
	names := make(map[string]bool)
	var expanded int64
	for position := 0; position < len(data); {
		if err := source.ctx.Err(); err != nil {
			return nil, 0, err
		}
		if len(records) >= int(count) || len(data)-position < 46 {
			return nil, 0, fmt.Errorf("unclaimed or truncated VFF central directory bytes")
		}
		header := data[position : position+46]
		if binary.LittleEndian.Uint32(header) != 0x02014b50 {
			return nil, 0, fmt.Errorf("invalid VFF central directory member")
		}
		made := binary.LittleEndian.Uint16(header[4:])
		needed := binary.LittleEndian.Uint16(header[6:])
		flags := binary.LittleEndian.Uint16(header[8:])
		method := binary.LittleEndian.Uint16(header[10:])
		n := int(binary.LittleEndian.Uint16(header[28:]))
		extra := int(binary.LittleEndian.Uint16(header[30:]))
		comment := int(binary.LittleEndian.Uint16(header[32:]))
		if n == 0 || n+extra > len(data)-position-46 || comment != 0 || binary.LittleEndian.Uint16(header[34:]) != 0 ||
			(made>>8 != 0 && made>>8 != 3) || (needed != 10 && needed != 20) || flags & ^uint16(0x806) != 0 || (method != 0 && method != 8) || method == 0 && flags&6 != 0 {
			return nil, 0, fmt.Errorf("unsupported VFF ZIP metadata, flags or compression")
		}
		attrs := binary.LittleEndian.Uint32(header[38:])
		if profile == vffClassificationZIP {
			dos := attrs & 0xffff
			if dos & ^skcDOSAttributes != 0 || dos&0x80 != 0 && dos != 0x80 || made>>8 == 0 && attrs>>16 != 0 {
				return nil, 0, fmt.Errorf("unsupported SKC DOS file attributes")
			}
		}
		kind := (attrs >> 16) & 0170000
		isDirectory := attrs&0x10 != 0 || made>>8 == 3 && kind == 0040000
		if isDirectory {
			if profile != vffClassificationZIP || made>>8 == 3 && kind != 0 && kind != 0040000 {
				return nil, 0, fmt.Errorf("unsupported VFF ZIP directory attributes")
			}
		} else if made>>8 == 3 && kind != 0 && kind != 0100000 {
			return nil, 0, fmt.Errorf("VFF ZIP members must be regular files")
		}
		nameBytes := data[position+46 : position+46+n]
		checkedName := nameBytes
		if isDirectory {
			if !bytes.HasSuffix(nameBytes, []byte("/")) {
				return nil, 0, fmt.Errorf("SKC directory name lacks its explicit separator")
			}
			checkedName = nameBytes[:len(nameBytes)-1]
		}
		if err := vffMemberName(checkedName, flags); err != nil {
			return nil, 0, err
		}
		if !validVFFZIPCentralExtra(data[position+46+n:position+46+n+extra], profile) {
			return nil, 0, fmt.Errorf("unsupported VFF ZIP central extension")
		}
		name := string(nameBytes)
		if names[name] {
			return nil, 0, fmt.Errorf("duplicate VFF member name")
		}
		names[name] = true
		compressed := int64(binary.LittleEndian.Uint32(header[20:]))
		size := int64(binary.LittleEndian.Uint32(header[24:]))
		local := int64(binary.LittleEndian.Uint32(header[42:]))
		if isDirectory && (method != 0 || compressed != 0 || size != 0 || binary.LittleEndian.Uint32(header[16:]) != 0) {
			return nil, 0, fmt.Errorf("SKC directory carries file data")
		}
		if compressed == 0xffffffff || size == 0xffffffff || local == 0xffffffff || size > limits.memberBytes || size > limits.expandedBytes-expanded ||
			local > directory-base || compressed > directory-base-local || method == 0 && compressed != size {
			return nil, 0, fmt.Errorf("VFF member exceeds declared bounds or expansion budget")
		}
		expanded += size
		records = append(records, vffZIPRecord{name: name, flags: flags, method: method, needed: needed, crc: binary.LittleEndian.Uint32(header[16:]), compressed: compressed, expanded: size, offset: base + local, directory: isDirectory})
		position += 46 + n + extra
	}
	if len(records) != int(count) {
		return nil, 0, fmt.Errorf("VFF central directory count mismatch")
	}
	if profile == vffClassificationZIP {
		paths := map[string]bool{}
		for _, record := range records {
			name := strings.TrimSuffix(record.name, "/")
			if _, exists := paths[name]; exists {
				return nil, 0, fmt.Errorf("SKC file and directory path conflict")
			}
			paths[name] = record.directory
		}
		for name := range paths {
			for parent := name; strings.Contains(parent, "/"); {
				parent = parent[:strings.LastIndexByte(parent, '/')]
				if directory, exists := paths[parent]; exists && !directory {
					return nil, 0, fmt.Errorf("SKC file used as parent directory")
				}
			}
		}
	}
	return records, directory, nil
}

func validVFFZIPCentralExtra(raw []byte, profile vffZIPProfile) bool {
	if len(raw) == 0 {
		return true
	}
	// APPNOTE 4.5.5: one NTFS time tuple, without unknown subattributes.
	return profile == vffClassificationZIP && len(raw) == 36 &&
		binary.LittleEndian.Uint16(raw) == 0x000a && binary.LittleEndian.Uint16(raw[2:]) == 32 &&
		binary.LittleEndian.Uint32(raw[4:]) == 0 && binary.LittleEndian.Uint16(raw[8:]) == 1 &&
		binary.LittleEndian.Uint16(raw[10:]) == 24
}

// The published VFF fold uses the CRC sequence in central-directory order.
// It is separate from the CRC of each actual uncompressed member body.
func foldedVFFCRC(records []vffZIPRecord) uint32 {
	var result uint32
	for parity := 0; parity < 2; parity++ {
		for i := parity; i < len(records); i += 2 {
			value := records[i].crc
			if parity == 0 {
				value = ^value
			} else if value > 42 {
				value -= 42
			}
			var word [4]byte
			binary.LittleEndian.PutUint32(word[:], value)
			result = crc32.Update(result, crc32.IEEETable, word[:])
		}
	}
	return result
}

func validateVFFLocal(source *vffSource, record *vffZIPRecord, profile vffZIPProfile) (int64, error) {
	header, err := source.at(record.offset, 30)
	if err != nil {
		return 0, err
	}
	n := int64(binary.LittleEndian.Uint16(header[26:]))
	extra := int64(binary.LittleEndian.Uint16(header[28:]))
	needed := binary.LittleEndian.Uint16(header[4:])
	if binary.LittleEndian.Uint32(header) != 0x04034b50 ||
		binary.LittleEndian.Uint16(header[6:]) != record.flags || binary.LittleEndian.Uint16(header[8:]) != record.method ||
		binary.LittleEndian.Uint32(header[14:]) != record.crc || int64(binary.LittleEndian.Uint32(header[18:])) != record.compressed ||
		int64(binary.LittleEndian.Uint32(header[22:])) != record.expanded || n != int64(len(record.name)) {
		return 0, fmt.Errorf("VFF local header and central directory disagree")
	}
	if !(extra == 0 && needed == record.needed) && !(profile == vffModelZIP && extra == 20 && needed == 45 && record.needed == 20) {
		return 0, fmt.Errorf("unsupported VFF local extension or version")
	}
	fields, err := source.at(record.offset+30, n+extra)
	if err != nil {
		return 0, err
	}
	if string(fields[:n]) != record.name {
		return 0, fmt.Errorf("VFF local and directory member names disagree")
	}
	if extra != 0 {
		// Observed SketchUp VFF packaging reserves two zero ZIP64 slots even
		// though neither size has the sentinel required by APPNOTE 4.5.3.
		// Recognize only these exact inactive bytes; never use them as sizes,
		// skip arbitrary extras, or enable actual ZIP64 through this rule.
		var reservation [20]byte
		binary.LittleEndian.PutUint16(reservation[:], 1)
		binary.LittleEndian.PutUint16(reservation[2:], 16)
		if !bytes.Equal(fields[n:], reservation[:]) {
			return 0, fmt.Errorf("unsupported VFF local ZIP64 reservation")
		}
	}
	record.dataOffset = record.offset + 30 + n + extra
	next := record.dataOffset + record.compressed
	if next > source.size-22 {
		return 0, fmt.Errorf("VFF local member exceeds archive")
	}
	return next, nil
}

func readVFFMember(source *vffSource, record vffZIPRecord) ([]byte, error) {
	section := io.NewSectionReader(source, record.dataOffset, record.compressed)
	buffer := bufio.NewReaderSize(section, 32<<10)
	var input io.Reader = buffer
	var inflater io.ReadCloser
	if record.method == 8 {
		inflater = flate.NewReader(buffer)
		defer inflater.Close()
		input = inflater
	}
	data := make([]byte, 0, int(record.expanded))
	var chunk [32 << 10]byte
	for {
		if err := source.ctx.Err(); err != nil {
			return nil, err
		}
		n, err := input.Read(chunk[:])
		if int64(n) > record.expanded-int64(len(data)) {
			return nil, fmt.Errorf("VFF actual member expansion exceeds its declaration")
		}
		data = append(data, chunk[:n]...)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decompress VFF member: %w", err)
		}
		if n == 0 {
			return nil, io.ErrNoProgress
		}
	}
	position, err := section.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	if position-int64(buffer.Buffered()) != record.compressed {
		return nil, fmt.Errorf("unconsumed bytes in VFF compressed member")
	}
	if int64(len(data)) != record.expanded || crc32.ChecksumIEEE(data) != record.crc {
		return nil, fmt.Errorf("VFF member content length or CRC mismatch")
	}
	return data, nil
}
