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

type CADInspection struct {
	CAD        *datatype.CADInfo
	FormatInfo map[string]interface{}
}

type CADInspector interface {
	InspectCAD(ctx context.Context, source *commonModels.Engine, tenantID uint, physicalPath, sourceFormat string, sizeBytes int64) (*CADInspection, error)
}

func EnrichSingleCADItem(
	ctx context.Context,
	attrs models.JSONMap,
	inspector CADInspector,
	source *commonModels.Engine,
	tenantID uint,
	item *metaitem.DetectedItem,
	physicalPath string,
	sizeBytes int64,
) error {
	if item == nil || item.Layout != format.LayoutSingle || item.DataType != datatype.CAD || !format.IsCADFormat(format.NormalizeFormat(item.Format)) {
		return nil
	}
	if inspector == nil {
		return fmt.Errorf("CAD deep scan requires a configured CAD inspector")
	}
	inspection, err := inspector.InspectCAD(ctx, source, tenantID, physicalPath, item.Format, sizeBytes)
	if err != nil {
		return err
	}
	if inspection == nil || inspection.CAD == nil {
		return fmt.Errorf("CAD inspector returned no CAD type info")
	}
	item.CAD = inspection.CAD.Clone()
	metaitem.ApplyCADInfo(attrs, item)
	if len(inspection.FormatInfo) > 0 {
		metaattr.MergeStandardAttributes(attrs, metaattr.FormatInfoAttributes(item.Format, inspection.FormatInfo))
	}
	return nil
}
