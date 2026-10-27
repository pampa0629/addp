package metaenrich

import (
	"context"
	"fmt"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

type RuntimeTableInspector interface {
	InspectTable(context.Context, *commonModels.Engine, uint, string, string) (*format.TableDescribeResult, error)
}

func EnrichRuntimeTableItem(ctx context.Context, attrs models.JSONMap, inspector RuntimeTableInspector, engine *commonModels.Engine, tenant uint, item *metaitem.DetectedItem, path string) (bool, error) {
	if item == nil || item.DataType != datatype.Table || item.Layout != format.LayoutSingle {
		return false, nil
	}
	if _, err := format.GetRuntimeTableProviderFactory(format.NormalizeFormat(item.Format)); err != nil {
		return false, nil
	}
	if inspector == nil {
		return true, fmt.Errorf("runtime table inspection requires a configured inspector")
	}
	result, err := inspector.InspectTable(ctx, engine, tenant, path, item.Format)
	if err != nil {
		return true, err
	}
	if result == nil || result.Table == nil {
		return true, fmt.Errorf("runtime table inspector returned no table facts")
	}
	item.Fields = result.Table.Fields
	metaattr.MergeStandardAttributes(attrs, metaattr.TableDescribeAttributes(metaattr.TableDescribeAttributesInput{FormatName: item.Format, Table: result.Table, FormatInfo: result.FormatInfo}))
	return true, nil
}
