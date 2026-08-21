package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/gin-gonic/gin"
)

type TransferCapabilityHandler struct{}

type TransferCapabilitiesResponse struct {
	TableFormats   []TransferTableFormatSupport   `json:"table_formats"`
	RawCopyFormats []TransferRawCopyFormatSupport `json:"raw_copy_formats"`
	Continuous     TransferContinuousCapabilities `json:"continuous"`
}

type TransferContinuousCapabilities struct {
	BusinessKafka TransferBusinessKafkaCapabilities `json:"business_kafka"`
	DatabaseCDC   TransferDatabaseCDCCapability     `json:"database_cdc"`
}

type TransferDatabaseCDCCapability struct {
	Sources   []string `json:"sources"`
	Targets   []string `json:"targets"`
	Bootstrap []string `json:"bootstrap"`
	ApplyMode string   `json:"apply_mode"`
}

type TransferBusinessKafkaCapabilities struct {
	RecordFailureModes []string                     `json:"record_failure_modes"`
	DeadLetters        TransferDeadLetterCapability `json:"dead_letters"`
	BoundedReplay      TransferReplayCapability     `json:"bounded_replay"`
}

type TransferDeadLetterCapability struct {
	Supported      bool     `json:"supported"`
	ListEndpoint   string   `json:"list_endpoint"`
	DetailEndpoint string   `json:"detail_endpoint"`
	Filters        []string `json:"filters"`
	ExposesPayload bool     `json:"exposes_payload"`
}

type TransferReplayCapability struct {
	Supported               bool     `json:"supported"`
	Endpoint                string   `json:"endpoint"`
	OwnerRecordFailureModes []string `json:"owner_record_failure_modes"`
}

type TransferTableFormatSupport struct {
	Value               string                                 `json:"value"`
	BackendType         string                                 `json:"backend_type"`
	Label               string                                 `json:"label"`
	Extension           string                                 `json:"extension,omitempty"`
	Group               string                                 `json:"group"`
	Spatial             bool                                   `json:"spatial,omitempty"`
	Read                bool                                   `json:"read"`
	Write               bool                                   `json:"write"`
	Options             map[string]any                         `json:"options,omitempty"`
	ColumnarCompression *TransferColumnarCompressionCapability `json:"columnar_compression,omitempty"`
	Layouts             []string                               `json:"layouts,omitempty"`
	MultiFile           bool                                   `json:"multi_file,omitempty"`
	ProviderKind        string                                 `json:"provider_kind,omitempty"`
}

type TransferColumnarCompressionCapability struct {
	// Codecs 是 writer 当前支持的 canonical Parquet column/page compression codec。
	Codecs []string `json:"codecs"`
	// Default 是未指定 target.options.compression 时使用的唯一默认 codec。
	Default string `json:"default"`
}

type TransferRawCopyFormatSupport struct {
	Value     string   `json:"value"`
	Label     string   `json:"label"`
	DataType  string   `json:"data_type"`
	Extension string   `json:"extension,omitempty"`
	Layouts   []string `json:"layouts,omitempty"`
}

func NewTransferCapabilityHandler() *TransferCapabilityHandler {
	return &TransferCapabilityHandler{}
}

// Get returns Transfer capabilities backed by common format descriptors and loaded plugins.
// @Summary 获取传输能力 | Get transfer capabilities
// @Description 返回 Transfer 当前可用的格式读写、列式压缩、业务 Kafka continuous 能力和数据库 CDC 支持矩阵。| Returns available Transfer format read/write and columnar compression capabilities, business Kafka continuous features, and the database CDC support matrix.
// @Tags 传输能力 | Capabilities
// @Produce json
// @Success 200 {object} api.TransferCapabilitiesResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.task.read"]
// @Router /capabilities [get]
// @Security BearerAuth
func (h *TransferCapabilityHandler) Get(c *gin.Context) {
	c.JSON(http.StatusOK, TransferCapabilitiesResponse{
		TableFormats:   buildTableFormatCapabilities(),
		RawCopyFormats: buildRawCopyFormatCapabilities(),
		Continuous:     buildContinuousCapabilities(),
	})
}

func buildContinuousCapabilities() TransferContinuousCapabilities {
	return TransferContinuousCapabilities{
		DatabaseCDC: TransferDatabaseCDCCapability{
			Sources: []string{"postgresql", "mysql", "oracle"}, Targets: []string{"postgresql", "mysql", "oracle"},
			Bootstrap: []string{"initial_snapshot"}, ApplyMode: "upsert_delete",
		},
		BusinessKafka: TransferBusinessKafkaCapabilities{
			RecordFailureModes: []string{"block", "dead_letter"},
			DeadLetters: TransferDeadLetterCapability{
				Supported:      true,
				ListEndpoint:   "/api/v1/transfer/task-definitions/{id}/dead-letters",
				DetailEndpoint: "/api/v1/transfer/task-definitions/{id}/dead-letters/{identity}",
				Filters:        []string{"source_partition", "error_category", "error_code", "payload_available"},
				ExposesPayload: false,
			},
			BoundedReplay: TransferReplayCapability{
				Supported: true, Endpoint: "/api/v1/transfer/task-definitions/{id}/replay",
				OwnerRecordFailureModes: []string{"block"},
			},
		},
	}
}

func buildTableFormatCapabilities() []TransferTableFormatSupport {
	descriptors := format.ListFormatDescriptors()
	result := make([]TransferTableFormatSupport, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if !isTransferTableDescriptor(descriptor) {
			continue
		}
		item := tableCapabilityFromDescriptor(descriptor, string(descriptor.Format), nil)
		if item.Read || item.Write {
			result = append(result, item)
		}
		if descriptor.Format == format.FormatJSON {
			result = appendJSONLTableEncodingCapability(result, descriptor)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		leftRank := formatSortRank(result[i].Value)
		rightRank := formatSortRank(result[j].Value)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return result[i].Value < result[j].Value
	})
	return result
}

func appendJSONLTableEncodingCapability(result []TransferTableFormatSupport, descriptor format.FormatDescriptor) []TransferTableFormatSupport {
	item := tableCapabilityFromDescriptor(descriptor, "jsonl", map[string]any{"json_mode": "jsonl"})
	if item.Read || item.Write {
		result = append(result, item)
	}
	return result
}

func isTransferTableDescriptor(descriptor format.FormatDescriptor) bool {
	if descriptor.Format == "" || descriptor.Format == format.FormatUnknown {
		return false
	}
	if descriptor.DataType == datatype.Table {
		return true
	}
	write, _ := transferWritable(descriptor.Format)
	return hasTableReader(descriptor.Format) || write
}

func tableCapabilityFromDescriptor(descriptor format.FormatDescriptor, value string, options map[string]any) TransferTableFormatSupport {
	backendType := descriptor.Format
	read := hasTableReader(backendType)
	write, providerKind := transferWritable(backendType)
	spatial := format.IsGeospatialFormat(backendType)
	return TransferTableFormatSupport{
		Value:               value,
		BackendType:         string(backendType),
		Label:               tableFormatLabel(value),
		Extension:           tableFormatExtension(backendType, value, options),
		Group:               tableFormatGroup(spatial),
		Spatial:             spatial,
		Read:                read,
		Write:               write,
		Options:             options,
		ColumnarCompression: tableColumnarCompressionCapability(backendType, write),
		Layouts:             append([]string(nil), descriptor.Layouts...),
		MultiFile:           containsString(descriptor.Layouts, format.LayoutMulti),
		ProviderKind:        providerKind,
	}
}

func tableColumnarCompressionCapability(formatType format.FormatType, writable bool) *TransferColumnarCompressionCapability {
	if !writable {
		return nil
	}
	capability, err := format.GetColumnarCompressionCapability(formatType)
	if err != nil {
		return nil
	}
	return &TransferColumnarCompressionCapability{
		Codecs:  append([]string(nil), capability.Codecs...),
		Default: capability.Default,
	}
}

func hasTableReader(formatType format.FormatType) bool {
	if _, err := format.GetTableReaderProvider(formatType); err == nil {
		return true
	}
	if _, err := format.GetMultiTableReaderProvider(formatType); err == nil {
		return true
	}
	if _, err := format.GetScopeTableReaderProvider(formatType); err == nil {
		return true
	}
	if _, err := format.GetRuntimeScopeTableReaderFactory(formatType); err == nil {
		return true
	}
	return false
}

func transferWritable(formatType format.FormatType) (bool, string) {
	if _, err := format.GetTableWriterProvider(formatType); err == nil {
		return true, "table"
	}
	if _, err := format.GetMultiTableWriterProvider(formatType); err == nil {
		return true, "multi_table"
	}
	if _, err := format.GetScopeTableWriterProvider(formatType); err == nil {
		return true, "scope"
	}
	if _, err := format.GetRuntimeScopeTableWriterFactory(formatType); err == nil {
		return true, "runtime_scope"
	}
	return false, ""
}

func tableFormatExtension(backendType format.FormatType, value string, options map[string]any) string {
	extension := format.DefaultWriteExtension(backendType, &format.WriteOptions{ExtraParams: options})
	if extension != "" {
		return strings.TrimPrefix(extension, ".")
	}
	return value
}

func tableFormatGroup(spatial bool) string {
	if spatial {
		return "spatial"
	}
	return "table"
}

func tableFormatLabel(value string) string {
	switch value {
	case "csv":
		return "CSV"
	case "tsv":
		return "TSV"
	case "json":
		return "JSON 数组"
	case "jsonl":
		return "JSON Lines"
	case "geojson":
		return "GeoJSON"
	case "parquet":
		return "Parquet"
	case "shapefile":
		return "Shapefile"
	default:
		return value
	}
}

func formatSortRank(value string) int {
	order := map[string]int{
		"csv":       10,
		"tsv":       20,
		"jsonl":     30,
		"json":      40,
		"parquet":   50,
		"geojson":   60,
		"shapefile": 70,
	}
	if rank, ok := order[value]; ok {
		return rank
	}
	return 1000
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func buildRawCopyFormatCapabilities() []TransferRawCopyFormatSupport {
	descriptors := format.ListFormatDescriptors()
	result := make([]TransferRawCopyFormatSupport, 0, len(descriptors))
	seen := make(map[string]struct{}, len(descriptors))
	for _, descriptor := range descriptors {
		if !isRawCopyDataType(descriptor.DataType) || !containsString(descriptor.Layouts, format.LayoutSingle) {
			continue
		}
		value := string(descriptor.Format)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, TransferRawCopyFormatSupport{
			Value:     value,
			Label:     rawCopyFormatLabel(value),
			DataType:  string(descriptor.DataType),
			Extension: firstDescriptorExtension(descriptor),
			Layouts:   append([]string(nil), descriptor.Layouts...),
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		leftRank := rawCopyDataTypeRank(result[i].DataType)
		rightRank := rawCopyDataTypeRank(result[j].DataType)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return result[i].Value < result[j].Value
	})
	return result
}

func isRawCopyDataType(dataType datatype.DataType) bool {
	switch dataType {
	case datatype.Document, datatype.Media, datatype.CAD, datatype.Unknown:
		return true
	default:
		return false
	}
}

func firstDescriptorExtension(descriptor format.FormatDescriptor) string {
	for _, extension := range descriptor.Identification.Extensions {
		normalized := strings.TrimPrefix(strings.TrimSpace(extension), ".")
		if normalized != "" {
			return normalized
		}
	}
	return ""
}

func rawCopyDataTypeRank(dataType string) int {
	switch dataType {
	case string(datatype.Document):
		return 10
	case string(datatype.Media):
		return 20
	case string(datatype.CAD):
		return 30
	case string(datatype.Unknown):
		return 40
	default:
		return 1000
	}
}

func rawCopyFormatLabel(value string) string {
	switch value {
	case "pdf":
		return "PDF"
	case "docx":
		return "DOCX"
	case "pptx":
		return "PPTX"
	case "wps":
		return "WPS"
	case "dwg":
		return "DWG"
	case "dxf":
		return "DXF"
	case "text":
		return "Text"
	case "markdown":
		return "Markdown"
	case "jpeg":
		return "JPEG"
	case "png":
		return "PNG"
	case "gif":
		return "GIF"
	case "tiff":
		return "TIFF"
	case "webp":
		return "WebP"
	case "bmp":
		return "BMP"
	case "svg":
		return "SVG"
	case "avif":
		return "AVIF"
	case "heic":
		return "HEIC"
	case "mp4":
		return "MP4"
	case "mov":
		return "MOV"
	case "mkv":
		return "MKV"
	case "avi":
		return "AVI"
	case "webm":
		return "WebM"
	case "mp3":
		return "MP3"
	case "wav":
		return "WAV"
	case "flac":
		return "FLAC"
	case "aac":
		return "AAC"
	case "ogg":
		return "OGG"
	case "unknown":
		return "Unknown"
	default:
		return value
	}
}
