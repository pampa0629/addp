package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/addp/common/format"
	"github.com/gin-gonic/gin"
)

type TransferCapabilityHandler struct{}

type TransferCapabilitiesResponse struct {
	TableFormats []TransferTableFormatCapability `json:"table_formats"`
}

type TransferTableFormatCapability struct {
	Value        string         `json:"value"`
	BackendType  string         `json:"backend_type"`
	Label        string         `json:"label"`
	Extension    string         `json:"extension,omitempty"`
	Group        string         `json:"group"`
	Spatial      bool           `json:"spatial,omitempty"`
	Read         bool           `json:"read"`
	Write        bool           `json:"write"`
	Options      map[string]any `json:"options,omitempty"`
	Layouts      []string       `json:"layouts,omitempty"`
	MultiFile    bool           `json:"multi_file,omitempty"`
	ProviderKind string         `json:"provider_kind,omitempty"`
}

func NewTransferCapabilityHandler() *TransferCapabilityHandler {
	return &TransferCapabilityHandler{}
}

// Get returns Transfer capabilities backed by common format providers.
// @Summary 获取传输能力 | Get transfer capabilities
// @Description 返回 Transfer 当前可用于表格传输的格式能力，来源于 common format descriptor 与 provider registry | Returns table transfer format capabilities backed by common format descriptors and providers
// @Tags 传输能力 | Capabilities
// @Produce json
// @Success 200 {object} api.TransferCapabilitiesResponse
// @Router /capabilities [get]
// @Security BearerAuth
func (h *TransferCapabilityHandler) Get(c *gin.Context) {
	c.JSON(http.StatusOK, TransferCapabilitiesResponse{
		TableFormats: buildTableFormatCapabilities(),
	})
}

func buildTableFormatCapabilities() []TransferTableFormatCapability {
	formats := []TransferTableFormatCapability{
		tableCapabilityFromFormat(format.FormatCSV, "csv", nil),
		tableCapabilityFromFormat(format.FormatTSV, "tsv", nil),
		tableCapabilityFromFormat(format.FormatJSON, "json", map[string]any{"json_mode": "array"}),
		tableCapabilityFromFormat(format.FormatJSON, "jsonl", map[string]any{"json_mode": "jsonl"}),
		tableCapabilityFromFormat(format.FormatJSON, "geojson", map[string]any{"spatial.target_encoding": "geojson"}),
		tableCapabilityFromFormat(format.FormatParquet, "parquet", nil),
		tableCapabilityFromFormat(format.FormatShapefile, "shapefile", nil),
	}

	result := make([]TransferTableFormatCapability, 0, len(formats))
	for _, item := range formats {
		if item.Read || item.Write {
			result = append(result, item)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		return formatSortRank(result[i].Value) < formatSortRank(result[j].Value)
	})
	return result
}

func tableCapabilityFromFormat(backendType format.FormatType, value string, options map[string]any) TransferTableFormatCapability {
	descriptor, ok := format.GetFormatDescriptor(backendType)
	read := ok && descriptor.TransferRead && hasTableReader(backendType)
	write, providerKind := transferWritable(backendType)
	write = ok && descriptor.TransferWrite && write
	spatial := descriptor.Spatial || value == "geojson" || value == "shapefile"
	return TransferTableFormatCapability{
		Value:        value,
		BackendType:  string(backendType),
		Label:        tableFormatLabel(value),
		Extension:    tableFormatExtension(backendType, value, options),
		Group:        tableFormatGroup(spatial),
		Spatial:      spatial,
		Read:         read,
		Write:        write,
		Options:      options,
		Layouts:      append([]string(nil), descriptor.Layouts...),
		MultiFile:    containsString(descriptor.Layouts, format.FormatLayoutMulti),
		ProviderKind: providerKind,
	}
}

func hasTableReader(formatType format.FormatType) bool {
	if _, err := format.GetTableReaderProvider(formatType); err == nil {
		return true
	}
	if _, err := format.GetMultiTableReaderProvider(formatType); err == nil {
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
