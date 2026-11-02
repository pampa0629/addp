package metaitem

import (
	"context"
	"fmt"
	"io"
	"reflect"

	"github.com/addp/common/contentio"
	"github.com/addp/common/engine/contentadapter"
	"github.com/addp/common/engine/plugin"
)

func fileStructureReaderAt(ctx context.Context, input DirectoryResolveInput, file StorageFileRef) (io.ReaderAt, error) {
	if _, ok := input.StructureReader.(plugin.RangeReadableProvider); !ok {
		return nil, fmt.Errorf("file structure requires native range access")
	}
	target, err := structureCatalogPath(input, file)
	if err != nil {
		return nil, err
	}
	reader := contentadapter.NewMappedReader(input.StructureReader, input.ConnInfo,
		func(ref contentio.Ref) (plugin.EngineCatalogPath, error) {
			if ref.Path != file.Path {
				return plugin.EngineCatalogPath{}, fmt.Errorf("file structure reference is outside the exact file")
			}
			return target, nil
		}, plugin.ReadOptions{})
	return contentio.NewReaderAt(ctx, reader.(contentio.RangeReader), contentio.NewRef(file.Path, "main")), nil
}

func structureCatalogPath(input DirectoryResolveInput, file StorageFileRef) (plugin.EngineCatalogPath, error) {
	target := resolveCatalogPath(input.EngineID, file.Path, input.EngineCatalogPathFor)
	exact := reflect.DeepEqual(target, plugin.FileItemPath(input.EngineID, file.Path))
	if len(target.Segments) >= 3 && target.Segments[1].Term == plugin.EngineCatalogTermBucket {
		exact = reflect.DeepEqual(target, plugin.ObjectItemPath(input.EngineID, target.Segments[1].Name, file.Path))
	}
	if !exact {
		return plugin.EngineCatalogPath{}, fmt.Errorf("file structure requires the exact typed file")
	}
	return target, nil
}
