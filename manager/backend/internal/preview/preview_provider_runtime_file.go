package preview

import (
	"context"
	"errors"
	"fmt"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/manager/internal/models"
	"math"
)

func previewRuntimeFileTable(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	page, size := req.Page, req.PageSize
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 2000 || page > math.MaxInt/size {
		return nil, fmt.Errorf("runtime table preview window exceeds budget")
	}
	options := format.DefaultParseOptions()
	options.ExtraParams = map[string]interface{}{}
	// Bind the metadata version as well as the version of this particular read.
	version := commonJSON.String(req.Attributes, "format_info."+string(req.RuntimeTableProvider.Format()), "source_version")
	if version == "" {
		return nil, ErrPreviewSourceRefreshRequired
	}
	options.ExtraParams["source_version"] = version
	described, err := req.RuntimeTableProvider.DescribeTable(ctx, options)
	if err != nil {
		if errors.Is(err, format.ErrTableSourceChanged) {
			return nil, ErrPreviewSourceRefreshRequired
		}
		return nil, err
	}
	rows, err := req.RuntimeTableProvider.SampleTable(ctx, int64((page-1)*size), int64(size), options)
	if err != nil {
		if errors.Is(err, format.ErrTableSourceChanged) {
			return nil, ErrPreviewSourceRefreshRequired
		}
		return nil, err
	}
	fields := described.Table.Fields
	columns := make([]string, len(fields))
	meta := make([]models.ColumnMetadata, len(fields))
	for i, f := range fields {
		columns[i] = f.Name
		meta[i] = models.ColumnMetadata{ColumnName: f.Name, Type: string(f.Type), IsNullable: f.Nullable}
	}
	total := int(*described.Table.RowCount)
	return &models.TablePreview{Mode: PreviewModeTable, Fields: fields, Columns: columns, ColumnMetadata: meta, Rows: rows, Total: models.ExactPreviewTotal(total), HasMore: page*size < total, Page: page, PageSize: size}, nil
}
