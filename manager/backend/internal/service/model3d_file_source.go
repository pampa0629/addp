package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/engineaccess"
	managerModels "github.com/addp/manager/internal/models"
)

// withFileModel3DSource is the source I/O adapter for the System-bound Manager
// authorization object. It has no directory enumeration or alternate locator.
func (e *ManagerModel3DGLBExecutor) withFileModel3DSource(ctx context.Context, req Model3DGLBExecutionRequest, engine *commonModels.Engine, consume func(context.Context, workflowaccess.Source, commonModels.JSONMap) error) error {
	auth := req.SourceAuthorization
	if e == nil || req.Task == nil || req.Config.ConfigVersion != managerModels.Model3DGLBFileConfigVersion || auth == nil || req.inputOwner == nil || consume == nil || engine == nil || !commonExecution.ManagerModel3DSourceEngineType(engine.EngineType) || engine.ID != req.Config.Source.SourceEngineID || engine.TenantID == nil || *engine.TenantID != req.Task.TenantID {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	if err := auth.ValidateConversion(req.Config); err != nil {
		return err
	}
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || lease.ExecutionID != req.ExecutionID || lease.TenantID <= 0 || uint(lease.TenantID) != req.Task.TenantID || lease.Attempt <= 0 {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	composition := auth.Composition()
	if composition == nil || composition.Validate(engine.ID) != nil || (composition.Format != "ifc" && composition.Format != "3ds" && composition.Format != "dae") || composition.Format != req.Config.Source.Format {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	primary, err := composition.PrimaryTarget()
	if err != nil {
		return err
	}
	loc, err := resourcetree.ParseURI(req.Config.Source.ItemLocator)
	if err != nil || loc.EngineID != engine.ID || (loc.ItemID != nil && *loc.ItemID != req.Config.Source.ItemID) || loc.Type != resourcetree.TypeFile || strings.Join(loc.Path, "/") != primary.StringPath() || commonModels.GenerateItemFingerprint(engine.ID, primary.StringPath()) != req.Config.Source.ItemFingerprint {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	if composition.Format == "ifc" && len(composition.Refs) != 1 {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	if err := auth.Checkpoint(ctx); err != nil {
		return err
	}
	registered, err := plugin.Get(engine.EngineType)
	if err != nil {
		return err
	}
	factsProvider, ok := registered.(plugin.EngineCatalogFactsProvider)
	if !ok {
		return errors.New("file model input requires exact file facts")
	}
	readerProvider, ok := registered.(plugin.ContentReadableProvider)
	if !ok {
		return errors.New("file model input requires content reads")
	}
	currentInfo := func(ctx context.Context, target plugin.EngineCatalogPath) (plugin.ConnectionInfo, error) {
		current, err := auth.BeforeSourceRead(ctx, target)
		if err != nil {
			return nil, err
		}
		if current == nil || current.ID != engine.ID || current.EngineType != engine.EngineType || current.Version <= 0 || engineaccess.EnsureAvailable(current) != nil || current.TenantID == nil || *current.TenantID != req.Task.TenantID {
			return nil, ErrModel3DGLBSourceAuthorizationRequired
		}
		return plugin.ConnectionInfo(current.ConnectionInfo), nil
	}
	stat := func(target plugin.EngineCatalogPath) (int64, error) {
		info, err := currentInfo(ctx, target)
		if err != nil {
			return 0, err
		}
		facts, err := factsProvider.DescribeEngineCatalogFacts(ctx, info, target, plugin.EngineCatalogFactsOptions{})
		if err != nil {
			return 0, fmt.Errorf("inspect exact file model input: %w", err)
		}
		if facts == nil || facts.Kind != plugin.EngineCatalogKindFile || !reflect.DeepEqual(facts.Path, target) || facts.Storage == nil || facts.Storage.SizeBytes == nil || facts.Storage.Path != target.StringPath() || *facts.Storage.SizeBytes <= 0 || *facts.Storage.SizeBytes == math.MaxInt64 {
			return 0, errors.New("file model input facts do not describe a nonempty authorized file")
		}
		return *facts.Storage.SizeBytes, nil
	}
	open := func(ctx context.Context, target plugin.EngineCatalogPath) (io.ReadCloser, error) {
		info, err := currentInfo(ctx, target)
		if err != nil {
			return nil, err
		}
		stream, err := readerProvider.OpenContent(ctx, info, target, plugin.ReadOptions{})
		if err != nil {
			if stream != nil {
				err = errors.Join(err, stream.Close())
			}
			return nil, err
		}
		if stream == nil {
			return nil, errors.New("file model input reader is missing")
		}
		// Recheck after opening the native stream, immediately before staging can
		// write its contents to Infra. A rejected stream is always closed.
		if err := auth.Checkpoint(ctx); err != nil {
			return nil, errors.Join(err, stream.Close())
		}
		return stream, nil
	}
	primarySize, err := stat(primary)
	if err != nil {
		return err
	}
	var primaryBytes []byte
	if composition.Format == "3ds" || composition.Format == "dae" {
		refs, err := auth.DeclaredTextureRefs()
		if err != nil {
			return err
		}
		stream, err := open(ctx, primary)
		if err != nil {
			return err
		}
		primaryBytes, err = bufferExchangeModelStagingEntrypoint(ctx, stream, composition.Format, refs)
		closeErr := stream.Close()
		if err != nil || closeErr != nil {
			return errors.Join(err, closeErr)
		}
		if int64(len(primaryBytes)) != primarySize {
			return errors.New("model main file size changed during preparation")
		}
	}
	parentDepth := len(primary.Segments) - 1
	relative := func(target plugin.EngineCatalogPath) (string, error) {
		names := make([]string, 0, len(target.Segments)-parentDepth)
		for _, segment := range target.Segments[parentDepth:] {
			names = append(names, segment.Name)
		}
		name := strings.Join(names, "/")
		if !validModel3DStagingPath(name) {
			return "", errors.New("file model input has an invalid relative path")
		}
		return name, nil
	}
	entrypoint, err := relative(primary)
	if err != nil {
		return err
	}
	var files []model3DStagingFile
	// Primary always precedes dependencies; its real declarations have already
	// been checked before any dependency Facts or OpenContent call.
	primaryOpen := func(ctx context.Context) (io.ReadCloser, error) { return open(ctx, primary) }
	if primaryBytes != nil {
		primaryOpen = func(ctx context.Context) (io.ReadCloser, error) {
			if err := auth.Checkpoint(ctx); err != nil {
				return nil, err
			}
			return io.NopCloser(bytes.NewReader(primaryBytes)), nil
		}
	}
	files = append(files, model3DStagingFile{RelativePath: entrypoint, SizeBytes: primarySize, Open: primaryOpen})
	for _, ref := range composition.Refs {
		if ref.Primary {
			continue
		}
		target := ref.Target
		name, err := relative(target)
		if err != nil {
			return err
		}
		size, err := stat(target)
		if err != nil {
			return err
		}
		files = append(files, model3DStagingFile{RelativePath: name, SizeBytes: size, Open: func(ctx context.Context) (io.ReadCloser, error) { return open(ctx, target) }})
	}
	kind := workflowaccess.KindFile
	if composition.Format == "3ds" || composition.Format == "dae" {
		kind = workflowaccess.KindDirectory
	}
	if err := auth.Checkpoint(ctx); err != nil {
		return err
	}
	return e.withStagedModel3DSource(ctx, lease, composition.Format, kind, entrypoint, files, req.inputOwner, func(ctx context.Context, source workflowaccess.Source) error {
		if err := auth.Checkpoint(ctx); err != nil {
			return err
		}
		return consume(ctx, source, commonModels.JSONMap{"engine_id": engine.ID, "engine_type": engine.EngineType, "access_method": workflowaccess.MethodObjectStore, "item_locator": req.Config.Source.ItemLocator, "file_count": len(files)})
	})
}
