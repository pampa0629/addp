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

type RuntimeFormatDetector interface {
	DetectFormat(ctx context.Context, source *commonModels.Engine, tenantID uint, physicalPath, candidateFormat, sourceLayout string) (format.FormatType, error)
}

func RefineRuntimeFormat(
	ctx context.Context,
	attrs models.JSONMap,
	detector RuntimeFormatDetector,
	source *commonModels.Engine,
	tenantID uint,
	item *metaitem.DetectedItem,
	physicalPath string,
) error {
	if item == nil {
		return nil
	}
	candidate := format.NormalizeFormat(item.Format)
	if _, err := format.GetRuntimeFormatDetectorFactory(candidate); err != nil {
		return nil
	}
	if detector == nil {
		return fmt.Errorf("runtime format detection for %s requires a configured detector", candidate)
	}
	detected, err := detector.DetectFormat(ctx, source, tenantID, physicalPath, string(candidate), item.Layout)
	if err != nil {
		return fmt.Errorf("refine %s format: %w", candidate, err)
	}
	descriptor, ok := format.GetFormatDescriptor(detected)
	if !ok || descriptor.DataType == datatype.Unknown || !format.HasLayout(descriptor.Layouts, item.Layout) {
		return fmt.Errorf("runtime detector returned incompatible format %q for %s/%s", detected, item.DataType, item.Layout)
	}
	if detected == candidate {
		return nil
	}
	item.Format = string(detected)
	item.DataType = descriptor.DataType
	metaattr.MergeDataItemAttributes(attrs, metaitem.AttributeInput(item))
	return nil
}
