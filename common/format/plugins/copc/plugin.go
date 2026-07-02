package copc

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/internal/lasfamily"
)

const (
	copcInfoVLROffset        = 375
	copcVLRHeaderSize        = 54
	copcInfoVLRDataSize      = 160
	copcInfoReadSize         = copcInfoVLROffset + copcVLRHeaderSize + copcInfoVLRDataSize
	copcInfoVLRRecordID      = 1
	copcHierarchyVLRRecordID = 1000
	copcHierarchyEntrySize   = 32
	copcHierarchyMaxPages    = 64
	copcHierarchyMaxBytes    = 4 * 1024 * 1024
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register COPC format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatCOPC
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-copc",
		Format:   format.FormatCOPC,
		I18nKey:  "format.copc",
		DataType: datatype.PointCloud,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions:        []string{".copc.laz", ".copc"},
			MimeTypes:         []string{"application/vnd.copc", "application/vnd.laszip", "application/octet-stream"},
			ContentSignatures: []string{"hex:4c415346"},
		},
	}
}

func (p *Plugin) DescribePointCloud(ctx context.Context, input format.PointCloudDescribeInput, options *format.ParseOptions) (*format.PointCloudDescribeResult, error) {
	header, info, err := readCOPCInfo(input.Reader)
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	formatInfo := lasfamily.BuildHeaderFormatInfo(header)
	formatInfo["profile"] = "copc"
	formatInfo["compression"] = "laszip"
	mergeCOPCInfo(formatInfo, info)
	if input.RangeReader != nil {
		hierarchy, err := readRootHierarchy(ctx, input.RangeReader, input.Ref, info)
		if err != nil {
			return nil, err
		}
		mergeHierarchyInfo(formatInfo, hierarchy)
	}
	return &format.PointCloudDescribeResult{
		PointCloud: lasfamily.BuildPointCloudInfo(header, datatype.PointCloudKindTiledPointCloud),
		Spatial:    lasfamily.BuildSpatialInfo(header),
		FormatInfo: formatInfo,
	}, nil
}

type copcInfo struct {
	CenterX             float64
	CenterY             float64
	CenterZ             float64
	HalfSize            float64
	Spacing             float64
	RootHierarchyOffset uint64
	RootHierarchySize   uint64
	GPSTimeMinimum      float64
	GPSTimeMaximum      float64
}

type hierarchySummary struct {
	Root             hierarchyAggregate
	Total            hierarchyAggregate
	PageReadCount    int
	PageReadByteSize int64
	ReadComplete     bool
	BudgetMaxPages   int
	BudgetMaxBytes   int64
}

type hierarchyAggregate struct {
	EntryCount         int
	LeafEntryCount     int
	InternalEntryCount int
	PageEntryCount     int
	MinimumLevel       int32
	MaximumLevel       int32
	PointCount         int64
	ByteSize           int64
}

type hierarchyEntry struct {
	Level      int32
	X          int32
	Y          int32
	Z          int32
	Offset     uint64
	ByteSize   int32
	PointCount int32
}

type hierarchyPageRef struct {
	Offset uint64
	Size   uint64
}

func readCOPCInfo(input io.Reader) (*lasfamily.Header, *copcInfo, error) {
	buf := make([]byte, copcInfoReadSize)
	n, err := io.ReadFull(input, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, nil, fmt.Errorf("read COPC header: %w", err)
	}
	if n < copcInfoReadSize {
		return nil, nil, fmt.Errorf("COPC header too short: %d", n)
	}
	header, err := lasfamily.ReadHeader(bytes.NewReader(buf[:lasfamily.MaxHeaderRead]))
	if err != nil {
		return nil, nil, err
	}
	if header.VersionMajor != 1 || header.VersionMinor < 4 {
		return nil, nil, fmt.Errorf("invalid COPC header: LAS version %d.%d", header.VersionMajor, header.VersionMinor)
	}
	if header.PointDataFormat < 6 || header.PointDataFormat > 8 {
		return nil, nil, fmt.Errorf("invalid COPC header: point data record format %d", header.PointDataFormat)
	}
	vlrHeader := buf[copcInfoVLROffset : copcInfoVLROffset+copcVLRHeaderSize]
	userID := strings.TrimRight(strings.TrimSpace(string(vlrHeader[2:18])), "\x00")
	recordID := binary.LittleEndian.Uint16(vlrHeader[18:20])
	recordLength := binary.LittleEndian.Uint16(vlrHeader[20:22])
	if userID != "copc" || recordID != copcInfoVLRRecordID || recordLength != copcInfoVLRDataSize {
		return nil, nil, fmt.Errorf("invalid COPC info VLR")
	}
	infoData := buf[copcInfoVLROffset+copcVLRHeaderSize : copcInfoReadSize]
	info := &copcInfo{
		CenterX:             float64At(infoData[0:8]),
		CenterY:             float64At(infoData[8:16]),
		CenterZ:             float64At(infoData[16:24]),
		HalfSize:            float64At(infoData[24:32]),
		Spacing:             float64At(infoData[32:40]),
		RootHierarchyOffset: binary.LittleEndian.Uint64(infoData[40:48]),
		RootHierarchySize:   binary.LittleEndian.Uint64(infoData[48:56]),
		GPSTimeMinimum:      float64At(infoData[56:64]),
		GPSTimeMaximum:      float64At(infoData[64:72]),
	}
	if info.RootHierarchyOffset == 0 || info.RootHierarchySize == 0 || info.RootHierarchySize%copcHierarchyEntrySize != 0 {
		return nil, nil, fmt.Errorf("invalid COPC root hierarchy reference")
	}
	if !allZero(infoData[72:160]) {
		return nil, nil, fmt.Errorf("invalid COPC info VLR reserved bytes")
	}
	return header, info, nil
}

func readRootHierarchy(ctx context.Context, reader formatRangeReader, ref contentio.Ref, info *copcInfo) (*hierarchySummary, error) {
	if reader == nil || info == nil || info.RootHierarchySize == 0 {
		return nil, nil
	}
	summary := &hierarchySummary{
		Root: hierarchyAggregate{
			MinimumLevel: -1,
			MaximumLevel: -1,
		},
		Total: hierarchyAggregate{
			MinimumLevel: -1,
			MaximumLevel: -1,
		},
		ReadComplete:   true,
		BudgetMaxPages: copcHierarchyMaxPages,
		BudgetMaxBytes: copcHierarchyMaxBytes,
	}
	queue := []hierarchyPageRef{{Offset: info.RootHierarchyOffset, Size: info.RootHierarchySize}}
	for len(queue) > 0 {
		page := queue[0]
		if summary.PageReadCount >= copcHierarchyMaxPages || summary.PageReadByteSize+int64(page.Size) > copcHierarchyMaxBytes {
			summary.ReadComplete = false
			break
		}
		queue = queue[1:]
		entries, err := readHierarchyPage(ctx, reader, ref, page, "COPC hierarchy page")
		if err != nil {
			return nil, err
		}
		summary.PageReadCount++
		summary.PageReadByteSize += int64(page.Size)
		for _, entry := range entries {
			if err := addHierarchyEntry(&summary.Total, entry); err != nil {
				return nil, err
			}
			if summary.PageReadCount == 1 {
				if err := addHierarchyEntry(&summary.Root, entry); err != nil {
					return nil, err
				}
			}
			if entry.PointCount == -1 {
				if entry.Offset == 0 || entry.ByteSize <= 0 || entry.ByteSize%copcHierarchyEntrySize != 0 {
					return nil, fmt.Errorf("invalid COPC child hierarchy page reference")
				}
				queue = append(queue, hierarchyPageRef{Offset: entry.Offset, Size: uint64(entry.ByteSize)})
			}
		}
	}
	return summary, nil
}

func readHierarchyPage(ctx context.Context, reader formatRangeReader, ref contentio.Ref, page hierarchyPageRef, label string) ([]hierarchyEntry, error) {
	if page.Offset == 0 || page.Size == 0 || page.Size%copcHierarchyEntrySize != 0 {
		return nil, fmt.Errorf("invalid COPC hierarchy page reference")
	}
	rc, err := reader.OpenRange(ctx, ref, int64(page.Offset), int64(page.Size))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}
	if uint64(len(data)) != page.Size || len(data)%copcHierarchyEntrySize != 0 {
		return nil, fmt.Errorf("invalid COPC root hierarchy page size: %d", len(data))
	}
	entries := make([]hierarchyEntry, 0, len(data)/copcHierarchyEntrySize)
	for offset := 0; offset < len(data); offset += copcHierarchyEntrySize {
		entries = append(entries, hierarchyEntry{
			Level:      int32(binary.LittleEndian.Uint32(data[offset : offset+4])),
			X:          int32(binary.LittleEndian.Uint32(data[offset+4 : offset+8])),
			Y:          int32(binary.LittleEndian.Uint32(data[offset+8 : offset+12])),
			Z:          int32(binary.LittleEndian.Uint32(data[offset+12 : offset+16])),
			Offset:     binary.LittleEndian.Uint64(data[offset+16 : offset+24]),
			ByteSize:   int32(binary.LittleEndian.Uint32(data[offset+24 : offset+28])),
			PointCount: int32(binary.LittleEndian.Uint32(data[offset+28 : offset+32])),
		})
	}
	return entries, nil
}

func addHierarchyEntry(summary *hierarchyAggregate, entry hierarchyEntry) error {
	if summary == nil {
		return nil
	}
	summary.EntryCount++
	if summary.MinimumLevel < 0 || entry.Level < summary.MinimumLevel {
		summary.MinimumLevel = entry.Level
	}
	if summary.MaximumLevel < 0 || entry.Level > summary.MaximumLevel {
		summary.MaximumLevel = entry.Level
	}
	switch {
	case entry.PointCount > 0:
		summary.LeafEntryCount++
		summary.PointCount += int64(entry.PointCount)
		if entry.ByteSize > 0 {
			summary.ByteSize += int64(entry.ByteSize)
		}
	case entry.PointCount == 0:
		summary.InternalEntryCount++
	case entry.PointCount == -1:
		summary.PageEntryCount++
	default:
		return fmt.Errorf("invalid COPC hierarchy point count: %d", entry.PointCount)
	}
	return nil
}

func mergeCOPCInfo(formatInfo map[string]interface{}, info *copcInfo) {
	if formatInfo == nil || info == nil {
		return
	}
	formatInfo["copc_version"] = "1.0"
	formatInfo["info_vlr_offset"] = copcInfoVLROffset
	formatInfo["info_vlr_record_id"] = copcInfoVLRRecordID
	formatInfo["hierarchy_vlr_record_id"] = copcHierarchyVLRRecordID
	formatInfo["hierarchy_entry_size"] = copcHierarchyEntrySize
	formatInfo["octree_center"] = []float64{info.CenterX, info.CenterY, info.CenterZ}
	formatInfo["octree_half_size"] = info.HalfSize
	formatInfo["root_spacing"] = info.Spacing
	formatInfo["root_hierarchy_offset"] = info.RootHierarchyOffset
	formatInfo["root_hierarchy_size"] = info.RootHierarchySize
	formatInfo["gpstime_minimum"] = info.GPSTimeMinimum
	formatInfo["gpstime_maximum"] = info.GPSTimeMaximum
}

func mergeHierarchyInfo(formatInfo map[string]interface{}, summary *hierarchySummary) {
	if formatInfo == nil || summary == nil {
		return
	}
	mergeHierarchyAggregate(formatInfo, "root_hierarchy", summary.Root)
	mergeHierarchyAggregate(formatInfo, "hierarchy", summary.Total)
	formatInfo["hierarchy_page_read_count"] = summary.PageReadCount
	formatInfo["hierarchy_read_byte_size"] = summary.PageReadByteSize
	formatInfo["hierarchy_read_complete"] = summary.ReadComplete
	formatInfo["hierarchy_max_pages"] = summary.BudgetMaxPages
	formatInfo["hierarchy_max_bytes"] = summary.BudgetMaxBytes
}

func mergeHierarchyAggregate(formatInfo map[string]interface{}, prefix string, summary hierarchyAggregate) {
	if formatInfo == nil || prefix == "" {
		return
	}
	formatInfo[prefix+"_entry_count"] = summary.EntryCount
	formatInfo[prefix+"_leaf_entry_count"] = summary.LeafEntryCount
	formatInfo[prefix+"_internal_entry_count"] = summary.InternalEntryCount
	formatInfo[prefix+"_page_entry_count"] = summary.PageEntryCount
	if summary.MinimumLevel >= 0 {
		formatInfo[prefix+"_min_level"] = summary.MinimumLevel
	}
	if summary.MaximumLevel >= 0 {
		formatInfo[prefix+"_max_level"] = summary.MaximumLevel
	}
	if summary.PointCount > 0 {
		formatInfo[prefix+"_point_count"] = summary.PointCount
	}
	if summary.ByteSize > 0 {
		formatInfo[prefix+"_byte_size"] = summary.ByteSize
	}
}

func float64At(buf []byte) float64 {
	return math.Float64frombits(binary.LittleEndian.Uint64(buf))
}

func allZero(buf []byte) bool {
	for _, value := range buf {
		if value != 0 {
			return false
		}
	}
	return true
}

type formatRangeReader interface {
	OpenRange(ctx context.Context, ref contentio.Ref, offset, length int64) (io.ReadCloser, error)
}
