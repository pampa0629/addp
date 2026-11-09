package metaenrich

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

func enrichGeoPackageSource(ctx context.Context, attrs models.JSONMap, input ResourceAttributesInput) error {
	metaattr.ReplaceAttributeNamespace(attrs, "format_info", "geopackage", map[string]interface{}{"composition_complete": false})
	provider, _ := input.ContentReader.(plugin.EngineCatalogFactsProvider)
	target := input.EngineCatalogPathFor(input.PhysicalPath)
	if !reflect.DeepEqual(target, plugin.FileItemPath(input.EngineID, input.PhysicalPath)) {
		provider = nil
	}
	before, err := format.ObserveGeoPackageSource(ctx, provider, input.ConnInfo, target)
	if err != nil {
		return err
	}
	r, err := input.ContentReader.OpenContent(ctx, input.ConnInfo, target, plugin.ReadOptions{})
	if err != nil {
		return err
	}
	data, readErr := io.ReadAll(r)
	closeErr := r.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return err
	}
	if err := enrichContainerChildrenData(ctx, attrs, input.Item, format.FormatGeoPackage, data); err != nil {
		return err
	}
	native := commonJSON.Section(attrs, "format_info.geopackage")
	if before == nil || before.SizeBytes < 100 || native["standalone_vector"] != true {
		return nil
	}
	after, err := format.ObserveGeoPackageSource(ctx, provider, input.ConnInfo, target)
	if err != nil {
		return err
	}
	if after == nil || !before.Matches(*after) || int64(len(data)) != before.SizeBytes {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(input.Item.RefList) > 1 {
		return fmt.Errorf("GeoPackage standalone source has additional content references")
	}
	input.Item.RefList = []dataitem.ItemRef{{Path: input.PhysicalPath, Role: "container", Required: true, Primary: true, Extension: ".gpkg"}}
	metaattr.MergeDataItemAttributes(attrs, metaitem.AttributeInput(input.Item))
	native["composition_complete"] = true
	metaattr.ReplaceAttributeNamespace(attrs, "format_info", "geopackage", native)
	return nil
}
