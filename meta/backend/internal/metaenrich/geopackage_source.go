package metaenrich

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

// The scan owner observes only four exact targets, not a sibling directory or
// an additional ACL. The existing NFS provider rejects links and special files.
func observeGeoPackageSource(ctx context.Context, input ResourceAttributesInput) (*plugin.EngineCatalogFacts, error) {
	p, ok := input.ContentReader.(plugin.EngineCatalogFactsProvider)
	if !ok || p.Type() != "nfs" {
		return nil, nil
	}
	target := input.EngineCatalogPathFor(input.PhysicalPath)
	if !reflect.DeepEqual(target, plugin.FileItemPath(input.EngineID, input.PhysicalPath)) {
		return nil, nil
	}
	facts, err := p.DescribeEngineCatalogFacts(ctx, input.ConnInfo, target, plugin.EngineCatalogFactsOptions{})
	if err != nil {
		return nil, err
	}
	if facts == nil || !reflect.DeepEqual(facts.Path, target) || facts.Kind != "file" || facts.Storage == nil ||
		facts.Storage.Path != input.PhysicalPath || facts.Storage.SizeBytes == nil || *facts.Storage.SizeBytes < 100 ||
		(facts.Storage.ETag == "" && (facts.UpdatedAt == nil || facts.UpdatedAt.IsZero())) {
		return nil, nil
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		_, err := p.DescribeEngineCatalogFacts(ctx, input.ConnInfo, plugin.FileItemPath(input.EngineID, input.PhysicalPath+suffix), plugin.EngineCatalogFactsOptions{})
		if err == nil {
			return nil, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err // Permission, transport and unknown failures are not absence.
		}
	}
	return facts, nil
}

func enrichGeoPackageSource(ctx context.Context, attrs models.JSONMap, input ResourceAttributesInput) error {
	metaattr.ReplaceAttributeNamespace(attrs, "format_info", "geopackage", map[string]interface{}{"composition_complete": false})
	before, err := observeGeoPackageSource(ctx, input)
	if err != nil {
		return err
	}
	r, err := input.ContentReader.OpenContent(ctx, input.ConnInfo, input.EngineCatalogPathFor(input.PhysicalPath), plugin.ReadOptions{})
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
	if before == nil || native["standalone_vector"] != true {
		return nil
	}
	after, err := observeGeoPackageSource(ctx, input)
	if err != nil {
		return err
	}
	if after == nil || !reflect.DeepEqual(before, after) || int64(len(data)) != *before.Storage.SizeBytes {
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
