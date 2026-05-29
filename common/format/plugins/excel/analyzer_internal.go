package excel

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

const (
	defaultSheetLimit      = 5
	defaultRowLimit        = 50
	defaultColumnLimit     = 50
	maxReadRows            = 200
	sampleRowsFallback     = 20
	defaultTypeDetectLimit = 100
)

// Options 控制 Excel 样本与结构分析的行为
type Options struct {
	SheetLimit      int
	RowLimit        int
	ColumnLimit     int
	TypeDetectLimit int
}

// DefaultOptions 返回默认配置
func DefaultOptions() Options {
	return Options{
		SheetLimit:      defaultSheetLimit,
		RowLimit:        defaultRowLimit,
		ColumnLimit:     defaultColumnLimit,
		TypeDetectLimit: defaultTypeDetectLimit,
	}
}

// WorkbookAnalysis Excel 工作簿分析结果
type WorkbookAnalysis struct {
	SheetCount   int
	DefaultSheet string
	ActiveSheet  string
	Sheets       []SheetSummary
	Summary      map[string]interface{}
}

// SheetSummary 单个工作表的样本和结构摘要
type SheetSummary struct {
	Name          string                   `json:"name"`
	Index         int                      `json:"index"`
	RowCount      int                      `json:"row_count"`
	ColumnCount   int                      `json:"column_count"`
	HasHeader     bool                     `json:"has_header"`
	Headers       []string                 `json:"headers"`
	ColumnTypes   []string                 `json:"column_types"`
	SampleRows    []map[string]interface{} `json:"sample_rows"`
	RowsTruncated bool                     `json:"rows_truncated"`
}

// Analyze 对 workbook 进行分析并返回样本和结构摘要
func Analyze(ctx context.Context, workbook *excelize.File, opts *Options) (*WorkbookAnalysis, error) {
	if workbook == nil {
		return nil, fmt.Errorf("excel analyzer: workbook is nil")
	}

	options := DefaultOptions()
	if opts != nil {
		if opts.SheetLimit > 0 {
			options.SheetLimit = opts.SheetLimit
		}
		if opts.RowLimit > 0 {
			options.RowLimit = opts.RowLimit
		}
		if opts.ColumnLimit > 0 {
			options.ColumnLimit = opts.ColumnLimit
		}
		if opts.TypeDetectLimit > 0 {
			options.TypeDetectLimit = opts.TypeDetectLimit
		}
	}

	sheetNames := workbook.GetSheetList()
	if len(sheetNames) == 0 {
		return &WorkbookAnalysis{
			SheetCount: 0,
			Sheets:     []SheetSummary{},
			Summary: map[string]interface{}{
				"sheet_count":    0,
				"sampled_sheets": 0,
				"row_limit":      options.RowLimit,
				"column_limit":   options.ColumnLimit,
			},
		}, nil
	}

	activeIndex := workbook.GetActiveSheetIndex()
	if activeIndex < 0 || activeIndex >= len(sheetNames) {
		activeIndex = 0
	}
	defaultSheet := sheetNames[activeIndex]

	limitSheets := options.SheetLimit
	if limitSheets <= 0 || limitSheets > len(sheetNames) {
		limitSheets = len(sheetNames)
	}

	summaries := make([]SheetSummary, 0, limitSheets)
	sheetsTruncated := false
	rowsTruncated := false

	for idx, name := range sheetNames {
		if len(summaries) >= limitSheets {
			sheetsTruncated = true
			break
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		summary, truncated, err := analyzeSheet(workbook, name, idx, options)
		if err != nil {
			return nil, fmt.Errorf("excel analyzer: analyze sheet %s failed: %w", name, err)
		}
		if truncated {
			rowsTruncated = true
		}
		summaries = append(summaries, summary)
	}

	result := &WorkbookAnalysis{
		SheetCount:   len(sheetNames),
		DefaultSheet: defaultSheet,
		ActiveSheet:  defaultSheet,
		Sheets:       summaries,
		Summary: map[string]interface{}{
			"sheet_count":      len(sheetNames),
			"sampled_sheets":   len(summaries),
			"sheet_limit":      limitSheets,
			"row_limit":        options.RowLimit,
			"column_limit":     options.ColumnLimit,
			"sheets_truncated": sheetsTruncated,
			"rows_truncated":   rowsTruncated,
		},
	}

	return result, nil
}

func analyzeSheet(workbook *excelize.File, sheetName string, index int, opts Options) (SheetSummary, bool, error) {
	dimCols, dimRows := sheetDimensions(workbook, sheetName)

	rowsIter, err := workbook.Rows(sheetName)
	if err != nil {
		headers := buildHeaders(nil, choosePositive(dimCols, opts.ColumnLimit), false)
		return SheetSummary{
			Name:          sheetName,
			Index:         index,
			RowCount:      estimateRowCount(dimRows, 0, false),
			ColumnCount:   len(headers),
			HasHeader:     false,
			Headers:       headers,
			ColumnTypes:   make([]string, len(headers)),
			SampleRows:    []map[string]interface{}{},
			RowsTruncated: false,
		}, false, nil
	}
	defer rowsIter.Close()

	rowLimit := maxInt(opts.RowLimit, sampleRowsFallback)
	readLimit := rowLimit + 1
	if readLimit <= 0 {
		readLimit = maxReadRows
	}
	if readLimit > maxReadRows {
		readLimit = maxReadRows
	}

	headerCandidate := []string{}
	rawRows := make([][]string, 0, minInt(rowLimit, sampleRowsFallback))
	maxColumns := dimCols
	rowIndex := 0

	for rowsIter.Next() {
		rowIndex++
		if readLimit > 0 && rowIndex > readLimit {
			break
		}

		raw, err := rowsIter.Columns()
		if err != nil {
			continue
		}

		trimmed := trimStringSlice(raw)
		if len(trimmed) > maxColumns {
			maxColumns = len(trimmed)
		}

		if rowIndex == 1 {
			headerCandidate = trimmed
			continue
		}

		rawRows = append(rawRows, trimmed)
	}

	hasHeader := looksLikeHeaderRow(headerCandidate)
	if !hasHeader && len(headerCandidate) > 0 {
		rawRows = append([][]string{headerCandidate}, rawRows...)
	}

	if maxColumns == 0 {
		maxColumns = len(headerCandidate)
	}
	if opts.ColumnLimit > 0 && maxColumns > opts.ColumnLimit {
		maxColumns = opts.ColumnLimit
	}

	headers := buildHeaders(headerCandidate, maxColumns, hasHeader)
	normalized := normalizeRows(rawRows, maxColumns)
	estimatedRows := estimateRowCount(dimRows, len(normalized), hasHeader)
	columnTypes := inferColumnTypes(normalized, maxColumns, opts.TypeDetectLimit)
	sampleRows := buildSampleRows(headers, normalized, sampleRowsFallback)
	rowsTruncated := estimatedRows > len(sampleRows)

	return SheetSummary{
		Name:          sheetName,
		Index:         index,
		RowCount:      estimatedRows,
		ColumnCount:   maxColumns,
		HasHeader:     hasHeader,
		Headers:       headers,
		ColumnTypes:   columnTypes,
		SampleRows:    sampleRows,
		RowsTruncated: rowsTruncated,
	}, rowsTruncated, nil
}

func sheetDimensions(workbook *excelize.File, sheetName string) (int, int) {
	axis, err := workbook.GetSheetDimension(sheetName)
	if err != nil || axis == "" {
		return 0, 0
	}

	parts := strings.Split(axis, ":")
	if len(parts) != 2 {
		return 0, 0
	}

	startCol, startRow, err := excelize.CellNameToCoordinates(parts[0])
	if err != nil {
		return 0, 0
	}

	endCol, endRow, err := excelize.CellNameToCoordinates(parts[1])
	if err != nil {
		return 0, 0
	}

	return endCol - startCol + 1, endRow - startRow + 1
}

func buildHeaders(candidate []string, columnCount int, hasHeader bool) []string {
	if columnCount <= 0 {
		columnCount = len(candidate)
	}
	headers := make([]string, columnCount)

	for i := 0; i < columnCount; i++ {
		if hasHeader && i < len(candidate) && strings.TrimSpace(candidate[i]) != "" {
			headers[i] = strings.TrimSpace(candidate[i])
		} else {
			headers[i] = fmt.Sprintf("Column%d", i+1)
		}
	}

	return headers
}

func normalizeRows(rows [][]string, columnCount int) [][]string {
	normalized := make([][]string, len(rows))
	for i, row := range rows {
		normalized[i] = padRow(row, columnCount)
	}
	return normalized
}

func padRow(row []string, columnCount int) []string {
	if len(row) == columnCount {
		return row
	}
	padded := make([]string, columnCount)
	copy(padded, row)
	return padded
}

func buildSampleRows(headers []string, rows [][]string, limit int) []map[string]interface{} {
	if len(rows) == 0 {
		return []map[string]interface{}{}
	}

	max := minInt(limit, len(rows))
	sample := make([]map[string]interface{}, 0, max)
	for i := 0; i < max; i++ {
		row := make(map[string]interface{}, len(headers))
		for j, header := range headers {
			if j < len(rows[i]) {
				row[header] = strings.TrimSpace(rows[i][j])
			} else {
				row[header] = ""
			}
		}
		sample = append(sample, row)
	}
	return sample
}

func inferColumnTypes(rows [][]string, columnCount, sampleLimit int) []string {
	columnTypes := make([]string, columnCount)
	samples := make([][]string, columnCount)

	for i := 0; i < columnCount; i++ {
		samples[i] = make([]string, 0, sampleLimit)
	}

	for rowIdx, row := range rows {
		if rowIdx >= sampleLimit {
			break
		}
		for colIdx := 0; colIdx < columnCount; colIdx++ {
			value := ""
			if colIdx < len(row) {
				value = row[colIdx]
			}
			samples[colIdx] = append(samples[colIdx], value)
		}
	}

	for i := 0; i < columnCount; i++ {
		columnTypes[i] = inferColumnType(samples[i])
	}

	return columnTypes
}

func inferColumnType(values []string) string {
	hasValues := false
	allIntegers := true
	allFloats := true
	allBools := true
	allDates := true

	for _, value := range values {
		v := strings.TrimSpace(value)
		if v == "" {
			continue
		}
		hasValues = true

		if !isInteger(v) {
			allIntegers = false
		}
		if !isFloat(v) {
			allFloats = false
		}
		if !isBool(v) {
			allBools = false
		}
		if !isDate(v) {
			allDates = false
		}
	}

	switch {
	case !hasValues:
		return "string"
	case allIntegers:
		return "int"
	case allFloats:
		return "float"
	case allBools:
		return "bool"
	case allDates:
		return "date"
	default:
		return "string"
	}
}

func looksLikeHeaderRow(row []string) bool {
	if len(row) == 0 {
		return false
	}

	nonEmpty := 0
	numeric := 0
	for _, value := range row {
		v := strings.TrimSpace(value)
		if v == "" {
			continue
		}
		nonEmpty++
		if isNumeric(v) {
			numeric++
		}
	}

	if nonEmpty == 0 {
		return false
	}
	return numeric < nonEmpty
}

func estimateRowCount(dimensionRows, loadedRows int, hasHeader bool) int {
	if dimensionRows > 0 {
		if hasHeader {
			return maxInt(dimensionRows-1, loadedRows)
		}
		return dimensionRows
	}
	if hasHeader {
		return maxInt(loadedRows-1, 0)
	}
	return loadedRows
}

func trimStringSlice(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = strings.TrimSpace(v)
	}
	return out
}

func isNumeric(value string) bool {
	return isInteger(value) || isFloat(value)
}

func isInteger(value string) bool {
	if value == "" {
		return false
	}
	if value[0] == '+' || value[0] == '-' {
		value = value[1:]
	}
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

var floatPattern = regexp.MustCompile(`^[+-]?(\d+\.\d+|\d+\.|\.\d+|\d+)$`)

func isFloat(value string) bool {
	if value == "" {
		return false
	}
	if !floatPattern.MatchString(value) {
		return false
	}
	if strings.ContainsRune(value, '.') {
		return true
	}
	_, err := time.Parse("20060102", value)
	return err != nil
}

func isBool(value string) bool {
	switch strings.ToLower(value) {
	case "true", "false", "yes", "no", "y", "n", "1", "0":
		return true
	default:
		return false
	}
}

var excelDateLayouts = []string{
	time.RFC3339,
	"2006-01-02",
	"2006/01/02",
	"01/02/2006",
	"02/01/2006",
	"2006.01.02",
	"02-Jan-2006",
	"02/Jan/2006",
	"02-Jan-06",
	"2006-01-02 15:04:05",
	"01/02/2006 15:04:05",
}

func isDate(value string) bool {
	if value == "" {
		return false
	}
	for _, layout := range excelDateLayouts {
		if _, err := time.Parse(layout, value); err == nil {
			return true
		}
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func choosePositive(primary, fallback int) int {
	if primary > 0 {
		return primary
	}
	return fallback
}
