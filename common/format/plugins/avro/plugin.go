package avro

import (
	"context"
	"fmt"
	"io"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/resume"
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatAvro
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:             "builtin-avro",
		Format:         p.Format(),
		I18nKey:        "format.avro",
		DataType:       datatype.Table,
		Layouts:        []string{format.LayoutSingle, format.LayoutWhole},
		Identification: format.FormatIdentification{Extensions: []string{".avro"}, MimeTypes: []string{"application/avro", "application/x-avro-binary"}, ContentSignatures: []string{"hex:4f626a01"}},
	}
}

func (p *Plugin) OpenTableReader(ctx context.Context, input io.Reader, options *format.ParseOptions) (format.TableReader, error) {
	if options != nil {
		if err := resume.RejectUnsupported(options.ResumeMarker, "avro.table_reader"); err != nil {
			return nil, err
		}
		if options.TableSample != nil && options.TableSample.InputIsPositioned {
			return nil, fmt.Errorf("Avro requires input from the file start")
		}
	}
	r, err := newReader(ctx, input)
	if err != nil {
		return nil, err
	}
	if options != nil {
		selected, err := format.ApplyFieldSelectionToTableInfo(&datatype.TableInfo{Fields: r.fields}, options.FieldSelection)
		if err != nil {
			return nil, err
		}
		r.selected = selected.Fields
	}
	return r, nil
}

func (p *Plugin) DescribeTable(ctx context.Context, input io.Reader, options *format.ParseOptions) (result *format.TableDescribeResult, err error) {
	// A declared Avro source must not degrade into descriptor-only metadata when
	// its schema, framing, or source read fails.
	defer func() { err = format.NewDefinitiveParseError(err) }()
	opened, err := p.OpenTableReader(ctx, input, options)
	if err != nil {
		return nil, err
	}
	r := opened.(*tableReader)
	defer r.Close(ctx)
	var count int64
	for {
		rows, err := r.ReadRows(ctx, 1024)
		if err != nil {
			return nil, err
		}
		count += int64(len(rows))
		if len(rows) == 0 {
			break
		}
	}
	return &format.TableDescribeResult{
		Table:      &datatype.TableInfo{Fields: r.Fields(), RowCount: &count},
		FormatInfo: map[string]interface{}{"schema": r.schema, "codec": r.compression},
	}, nil
}

func (p *Plugin) SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	if offset < 0 || limit <= 0 || limit > int64(int(^uint(0)>>1)) {
		return nil, fmt.Errorf("Avro sample requires a nonnegative offset and positive limit")
	}
	r, err := p.OpenTableReader(ctx, input, options)
	if err != nil {
		return nil, err
	}
	defer r.Close(ctx)
	for offset > 0 {
		n := min(offset, 1024)
		rows, err := r.ReadRows(ctx, int(n))
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return []map[string]interface{}{}, nil
		}
		offset -= int64(len(rows))
	}
	return r.ReadRows(ctx, int(limit))
}

var (
	_ format.TableInfoProvider   = (*Plugin)(nil)
	_ format.TableSampleReader   = (*Plugin)(nil)
	_ format.TableReaderProvider = (*Plugin)(nil)
)

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
