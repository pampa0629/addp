package preview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/manager/internal/models"
)

const (
	TrajectoryPointLimit = 2000
	trajectoryByteLimit  = 32 * 1024 * 1024
	trajectoryRowLimit   = 250000
	trajectoryTimeout    = 15 * time.Second
)

var (
	ErrTrajectorySelection = errors.New("trajectory_selection_invalid")
	ErrTrajectoryPoints    = errors.New("trajectory_point_limit_exceeded")
	ErrTrajectoryBudget    = errors.New("trajectory_scan_budget_exceeded")
	ErrTrajectorySource    = errors.New("trajectory_source_invalid")
	trajectoryISO          = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[tT]\d{2}:\d{2}:\d{2}(?:\.\d{1,3})?(?:[zZ]|[+-]\d{2}:\d{2})$`)
	trajectoryEpoch        = regexp.MustCompile(`^-?\d+(?:\.\d+)?$`)
)

// ParseTrajectorySelection rejects duplicate/unknown members instead of letting
// encoding/json silently replace a predicate supplied twice.
func ParseTrajectorySelection(raw string) (*models.TrajectorySelection, error) {
	if len(raw) == 0 || len(raw) > 8192 {
		return nil, ErrTrajectorySelection
	}
	d := json.NewDecoder(strings.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrTrajectorySelection
	}
	allowed := map[string]bool{"device_field": true, "time_field": true, "time_encoding": true, "devices": true, "start": true, "end": true}
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return nil, ErrTrajectorySelection
		}
		key, ok := token.(string)
		if !ok || !allowed[key] || seen[key] {
			return nil, ErrTrajectorySelection
		}
		seen[key] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || string(value) == "null" {
			return nil, ErrTrajectorySelection
		}
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') || len(seen) != len(allowed) {
		return nil, ErrTrajectorySelection
	}
	if _, err = d.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrTrajectorySelection
	}
	d = json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	var selection models.TrajectorySelection
	if d.Decode(&selection) != nil {
		return nil, ErrTrajectorySelection
	}
	if _, _, _, err := validateTrajectorySelection(&selection); err != nil {
		return nil, err
	}
	return &selection, nil
}

func validateTrajectorySelection(s *models.TrajectorySelection) (map[string]bool, int64, int64, error) {
	if s == nil || strings.TrimSpace(s.DeviceField) == "" || len(s.DeviceField) > 256 || strings.TrimSpace(s.TimeField) == "" || len(s.TimeField) > 256 || s.DeviceField == s.TimeField || len(s.Devices) < 1 || len(s.Devices) > 100 {
		return nil, 0, 0, ErrTrajectorySelection
	}
	if s.TimeEncoding != "iso" && s.TimeEncoding != "unix_s" && s.TimeEncoding != "unix_ms" {
		return nil, 0, 0, ErrTrajectorySelection
	}
	start, err := trajectoryMillis(s.Start, "iso")
	if err != nil {
		return nil, 0, 0, ErrTrajectorySelection
	}
	end, err := trajectoryMillis(s.End, "iso")
	if err != nil || start >= end {
		return nil, 0, 0, ErrTrajectorySelection
	}
	devices := map[string]bool{}
	for _, device := range s.Devices {
		key, err := trajectoryDeviceKey(device)
		if err != nil || devices[key] {
			return nil, 0, 0, ErrTrajectorySelection
		}
		devices[key] = true
	}
	return devices, start, end, nil
}

func trajectoryDeviceKey(value any) (string, error) {
	if text, ok := value.(string); ok {
		if strings.TrimSpace(text) == "" || len(text) > 256 {
			return "", ErrTrajectorySource
		}
		return "s:" + text, nil
	}
	if value == nil {
		return "", ErrTrajectorySource
	}
	if _, ok := value.(json.Number); !ok {
		switch reflect.TypeOf(value).Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		default:
			return "", ErrTrajectorySource
		}
	}
	text := fmt.Sprint(value)
	if len(text) > 64 {
		return "", ErrTrajectorySource
	}
	approx, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(approx) || math.IsInf(approx, 0) || math.Abs(approx) > 9007199254740991 || math.Trunc(approx) != approx {
		return "", ErrTrajectorySource
	}
	if approx == 0 {
		return "n:0", nil
	}
	number, ok := new(big.Rat).SetString(text)
	if !ok || !number.IsInt() || !number.Num().IsInt64() {
		return "", ErrTrajectorySource
	}
	integer := number.Num().Int64()
	if integer < -9007199254740991 || integer > 9007199254740991 {
		return "", ErrTrajectorySource
	}
	return "n:" + number.Num().String(), nil
}

func trajectoryMillis(value any, encoding string) (int64, error) {
	if encoding == "iso" {
		text, ok := value.(string)
		if !ok || !trajectoryISO.MatchString(text) {
			return 0, ErrTrajectorySource
		}
		if text[10] == 't' {
			text = text[:10] + "T" + text[11:]
		}
		if strings.HasSuffix(text, "z") {
			text = text[:len(text)-1] + "Z"
		}
		// Go accepts zone offsets of 24 hours/60 minutes; the public contract does not.
		zone := text[len(text)-6:]
		if zone[0] == '+' || zone[0] == '-' {
			if zone[1:3] > "23" || zone[4:6] > "59" {
				return 0, ErrTrajectorySource
			}
		}
		at, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return 0, ErrTrajectorySource
		}
		return at.UnixMilli(), nil
	}
	if value == nil {
		return 0, ErrTrajectorySource
	}
	switch value.(type) {
	case string, json.Number, float64, float32, int, int64, int32, uint, uint64, uint32:
	default:
		return 0, ErrTrajectorySource
	}
	text := fmt.Sprint(value)
	if len(text) > 64 {
		return 0, ErrTrajectorySource
	}
	if _, isText := value.(string); isText {
		if !trajectoryEpoch.MatchString(text) {
			return 0, ErrTrajectorySource
		}
	} else {
		approx, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsNaN(approx) || math.IsInf(approx, 0) || math.Abs(approx) > 8640000000000000 {
			return 0, ErrTrajectorySource
		}
		if approx == 0 {
			return 0, nil
		}
	}
	number, ok := new(big.Rat).SetString(text)
	if !ok {
		return 0, ErrTrajectorySource
	}
	if encoding == "unix_s" {
		number.Mul(number, big.NewRat(1000, 1))
	} else if encoding != "unix_ms" {
		return 0, ErrTrajectorySelection
	}
	if !number.IsInt() || !number.Num().IsInt64() {
		return 0, ErrTrajectorySource
	}
	ms := number.Num().Int64()
	if ms < -8640000000000000 || ms > 8640000000000000 {
		return 0, ErrTrajectorySource
	}
	return ms, nil
}

func validateTrajectoryTarget(req *PreviewRequest) error {
	if req.TrajectorySelection == nil {
		return nil
	}
	f := formatTypeFromMetaAttributes(req.Attributes)
	if !isContentFileItemType(req.ItemType) || itemLayoutFromMetaAttributes(req.Attributes) != "single" || itemDataTypeFromMetaAttributes(req.Attributes) != "table" || (f != format.FormatCSV && f != format.FormatJSON && f != format.FormatGeoJSON) || req.ChildName != "" || req.RefPath != "" || req.NestedChildPath != "" || req.Page != 1 || req.PageSize != TrajectoryPointLimit || req.CheckTrajectoryRead == nil {
		return ErrTrajectorySelection
	}
	info := tableInfoFromMetaAttributes(req.Attributes, "table")
	if info == nil {
		return ErrTrajectorySelection
	}
	fields := map[string]bool{}
	for _, field := range info.Fields {
		if (field.Name == req.TrajectorySelection.DeviceField || field.Name == req.TrajectorySelection.TimeField) && (len(field.Path) > 1 || (len(field.Path) == 1 && field.Path[0] != field.Name)) {
			return ErrTrajectorySelection
		}
		if fields[field.Name] {
			return ErrTrajectorySelection
		}
		fields[field.Name] = true
	}
	if !fields[req.TrajectorySelection.DeviceField] || !fields[req.TrajectorySelection.TimeField] {
		return ErrTrajectorySelection
	}
	_, _, _, err := validateTrajectorySelection(req.TrajectorySelection)
	return err
}

// A separate byte probe distinguishes a genuine EOF at the budget boundary
// from artificial EOF produced by io.LimitReader.
type trajectoryBudgetReader struct {
	source    io.Reader
	remaining int64
	ctx       context.Context
}

func (r *trajectoryBudgetReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining == 0 {
		var probe [1]byte
		n, err := r.source.Read(probe[:])
		if n > 0 {
			return 0, ErrTrajectoryBudget
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.source.Read(p)
	r.remaining -= int64(n)
	return n, err
}

func (p *FileTablePreviewProvider) previewTrajectory(ctx context.Context, content contentio.Reader, bucket, fullPath string, f format.FormatType, opts *format.ParseOptions, req *PreviewRequest) (*models.TablePreview, error) {
	if err := validateTrajectoryTarget(req); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, trajectoryTimeout)
	defer cancel()
	if err := req.CheckTrajectoryRead(); err != nil {
		return nil, err
	}
	readerProvider, err := format.GetTableReaderProvider(f)
	if err != nil {
		return nil, ErrTrajectorySelection
	}
	source, err := content.Open(ctx, contentio.NewRef(fullPath, contentio.RoleMain))
	if err != nil {
		return nil, err
	}
	defer source.Close()
	stopClose := context.AfterFunc(ctx, func() { _ = source.Close() })
	defer stopClose()
	input := &trajectoryBudgetReader{source: source, remaining: trajectoryByteLimit, ctx: ctx}
	reader, err := readerProvider.OpenTableReader(ctx, input, opts)
	if err != nil {
		return nil, trajectoryReadError(ctx, err)
	}
	defer reader.Close(context.WithoutCancel(ctx))
	devices, start, end, _ := validateTrajectorySelection(req.TrajectorySelection)
	info := tableInfoFromMetaAttributes(req.Attributes, "table")
	columns := make([]string, 0, len(info.Fields))
	known := map[string]bool{}
	deviceBigint := false
	for _, field := range info.Fields {
		columns = append(columns, field.Name)
		known[field.Name] = true
		if field.Name == req.TrajectorySelection.DeviceField {
			deviceBigint = field.Type == datatype.FieldTypeBigInt
		}
	}
	rows := make([]map[string]interface{}, 0)
	scanned := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, trajectoryReadError(ctx, err)
		}
		if err := req.CheckTrajectoryRead(); err != nil {
			return nil, err
		}
		batch, err := reader.ReadRows(ctx, min(512, trajectoryRowLimit-scanned+1))
		if checkErr := req.CheckTrajectoryRead(); checkErr != nil {
			return nil, checkErr
		}
		if err := ctx.Err(); err != nil {
			return nil, trajectoryReadError(ctx, err)
		}
		if err != nil {
			return nil, trajectoryReadError(ctx, err)
		}
		if len(batch) == 0 {
			break
		}
		for _, row := range batch {
			scanned++
			if scanned > trajectoryRowLimit {
				return nil, ErrTrajectoryBudget
			}
			for key := range row {
				if !known[key] {
					return nil, ErrTrajectorySource
				}
			}
			device := row[req.TrajectorySelection.DeviceField]
			if deviceBigint && device != nil {
				device, err = datatype.BigintJSONText(device)
				if err != nil {
					return nil, ErrTrajectorySource
				}
			}
			key, err := trajectoryDeviceKey(device)
			if err != nil {
				return nil, err
			}
			if !devices[key] {
				continue
			}
			at, err := trajectoryMillis(row[req.TrajectorySelection.TimeField], req.TrajectorySelection.TimeEncoding)
			if err != nil {
				return nil, err
			}
			if at < start || at >= end {
				continue
			}
			if len(rows) == TrajectoryPointLimit {
				return nil, ErrTrajectoryPoints
			}
			rows = append(rows, row)
		}
	}
	// Consume metadata/trailing bytes beyond the format reader's logical end;
	// these still count toward the physical scan budget.
	if _, err := io.Copy(io.Discard, input); err != nil {
		return nil, trajectoryReadError(ctx, err)
	}
	if err := req.CheckTrajectoryRead(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, trajectoryReadError(ctx, err)
	}
	spatial := spatialInfoFromMetaAttributes(req.Attributes)
	var srid int
	var crs string
	if spatial != nil {
		srid = spatial.PrimarySRIDValue()
		crs = spatial.PrimaryCRSRef()
	}
	geometry := p.detectGeometryColumns(info)
	definition := spatial.CRSDefinitionByID(crs)
	contract := tablePreviewSpatialCRSContract(geometry, srid, crs, definition)
	return &models.TablePreview{Mode: PreviewModeTable, Columns: columns, Fields: info.Fields, Rows: rows, Total: models.ExactPreviewTotal(len(rows)), Page: 1, PageSize: TrajectoryPointLimit, GeometryColumns: geometry, GeometryColumn: contract.GeometryColumn, SourceSRID: contract.SourceSRID, SourceCRS: contract.SourceCRS, SourceCRSDefinition: contract.SourceCRSDefinition, TransformStatus: contract.TransformStatus, PreviewHint: contract.PreviewHint, SRID: srid, TrajectoryScope: &models.TrajectoryScope{Selection: *req.TrajectorySelection, Complete: true, ScannedRows: scanned}}, nil
}

func trajectoryReadError(ctx context.Context, err error) error {
	if errors.Is(err, ErrTrajectoryBudget) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrTrajectoryBudget
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return ErrTrajectorySource
}
