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

	"github.com/addp/common/dataitem"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/fbx"
	"github.com/addp/common/format/plugins/obj"
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
	if composition == nil || !commonExecution.ManagerModel3DSourceComposition(engine.EngineType, composition) || composition.Format != req.Config.Source.Format {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	primary, err := composition.PrimaryTarget()
	if err != nil {
		return err
	}
	sourceType, sourceKind := resourcetree.TypeFile, plugin.EngineCatalogKindFile
	if engine.EngineType == "minio" {
		sourceType, sourceKind = resourcetree.TypeObject, plugin.EngineCatalogKindObject
	}
	loc, err := resourcetree.ParseURI(req.Config.Source.ItemLocator)
	if err != nil || loc.EngineID != engine.ID || (loc.ItemID != nil && *loc.ItemID != req.Config.Source.ItemID) || loc.Type != sourceType || strings.Join(loc.Path, "/") != primary.StringPath() || commonModels.GenerateItemFingerprint(engine.ID, primary.StringPath()) != req.Config.Source.ItemFingerprint {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	if (composition.Format == "ifc" || composition.Format == "stl" || composition.Format == "fbx") && len(composition.Refs) != 1 {
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
	stat := func(target plugin.EngineCatalogPath, allowEmpty bool) (int64, error) {
		info, err := currentInfo(ctx, target)
		if err != nil {
			return 0, err
		}
		facts, err := factsProvider.DescribeEngineCatalogFacts(ctx, info, target, plugin.EngineCatalogFactsOptions{})
		if err != nil {
			return 0, fmt.Errorf("inspect exact file model input: %w", err)
		}
		if facts == nil || facts.Kind != sourceKind || !reflect.DeepEqual(facts.Path, target) || facts.Storage == nil || facts.Storage.SizeBytes == nil || facts.Storage.Path != target.StringPath() || *facts.Storage.SizeBytes < 0 || (!allowEmpty && *facts.Storage.SizeBytes == 0) || *facts.Storage.SizeBytes == math.MaxInt64 {
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
	primarySize, err := stat(primary, false)
	if err != nil {
		return err
	}
	var primaryBytes []byte
	buffered := map[string][]byte{}
	if composition.Format == "gltf" {
		if primarySize > format.MaxGLTFManifestBytes {
			return ErrModel3DGLBSourceAuthorizationRequired
		}
		stream, err := open(ctx, primary)
		if err != nil {
			return err
		}
		raw, readErr := io.ReadAll(io.LimitReader(&model3DStagingContextReader{ctx: ctx, reader: stream}, primarySize+1))
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil {
			return errors.Join(readErr, closeErr)
		}
		if int64(len(raw)) != primarySize {
			return ErrModel3DGLBSourceAuthorizationRequired
		}
		if err := validateGLTFCompositionBytes(ctx, raw, composition); err != nil {
			return err
		}
		primaryBytes = raw
	}
	if composition.Format == "obj" {
		libraries, err := auth.OBJMaterialLibraries()
		if err != nil || primarySize > obj.MaxCompositionBytes {
			return ErrModel3DGLBSourceAuthorizationRequired
		}
		readManifest := func(target plugin.EngineCatalogPath, size int64, material bool) ([]byte, []string, error) {
			stream, err := open(ctx, target)
			if err != nil {
				return nil, nil, err
			}
			raw, refs, err := bufferOBJStagingManifest(ctx, stream, size, material)
			return raw, refs, errors.Join(err, stream.Close())
		}
		var declared []string
		primaryBytes, declared, err = readManifest(primary, primarySize, false)
		if err != nil {
			return err
		}
		expected := make([]string, len(libraries))
		for i, library := range libraries {
			expected[i] = library.Reference
		}
		if !sameOBJDeclarations(declared, expected) {
			return errors.New("OBJ material declarations changed from the frozen composition")
		}
		var materialBytes int64
		for _, library := range libraries {
			var target plugin.EngineCatalogPath
			for _, ref := range composition.Refs {
				if ref.Target.Segments[len(ref.Target.Segments)-1].Name == library.Reference &&
					(ref.Role == "material_library" || strings.HasPrefix(ref.Role, "material_library_")) {
					target = ref.Target
					break
				}
			}
			if target.EngineID == 0 {
				return ErrModel3DGLBSourceAuthorizationRequired
			}
			size, err := stat(target, true)
			if err != nil {
				return err
			}
			if size > obj.MaxCompositionBytes-materialBytes {
				return errors.New("OBJ material libraries exceed the aggregate read budget")
			}
			materialBytes += size
			raw, textures, err := readManifest(target, size, true)
			if err != nil {
				return err
			}
			if !sameOBJDeclarations(textures, library.TextureRefs) {
				return errors.New("OBJ texture declarations changed from the frozen composition")
			}
			buffered[target.StringPath()] = raw
		}
	}
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
	if composition.Format == "stl" || composition.Format == "fbx" {
		// The same strictly revalidated bytes are staged; the source is not reopened.
		maxPreparationBytes := int64(64 << 20)
		if composition.Format == "fbx" {
			maxPreparationBytes = fbx.MaxCompositionBytes
		}
		if primarySize > maxPreparationBytes {
			return fmt.Errorf("%s preparation exceeds the model budget", composition.Format)
		}
		stream, err := open(ctx, primary)
		if err != nil {
			return err
		}
		raw, readErr := io.ReadAll(io.LimitReader(&model3DStagingContextReader{ctx: ctx, reader: stream}, primarySize+1))
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil {
			return errors.Join(readErr, closeErr)
		}
		if int64(len(raw)) != primarySize {
			return fmt.Errorf("%s size changed during preparation", composition.Format)
		}
		provider, err := format.GetModel3DCompositionProvider(format.FormatType(composition.Format))
		if err != nil {
			return err
		}
		refs, err := provider.ReadModel3DComposition(ctx, bytes.NewReader(raw), primarySize)
		if err != nil || refs == nil || len(refs) != 0 {
			return fmt.Errorf("%s structure no longer proves the frozen single-file composition", composition.Format)
		}
		primaryBytes = raw
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
		if raw, exists := buffered[target.StringPath()]; exists {
			files = append(files, model3DStagingFile{RelativePath: name, SizeBytes: int64(len(raw)), Open: func(ctx context.Context) (io.ReadCloser, error) {
				if err := auth.Checkpoint(ctx); err != nil {
					return nil, err
				}
				return io.NopCloser(bytes.NewReader(raw)), nil
			}})
			continue
		}
		size, err := stat(target, false)
		if err != nil {
			return err
		}
		files = append(files, model3DStagingFile{RelativePath: name, SizeBytes: size, Open: func(ctx context.Context) (io.ReadCloser, error) { return open(ctx, target) }})
	}
	kind := workflowaccess.KindFile
	if composition.Format == "3ds" || composition.Format == "dae" || composition.Format == "obj" || composition.Format == "gltf" {
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

// Validate the already-authorized manifest before opening dependencies. The
// same bytes are then staged, so no second primary read can change the input.
func validateGLTFCompositionBytes(ctx context.Context, raw []byte, composition *authorizationfacts.FileCompositionBoundary) error {
	if composition == nil || composition.Format != "gltf" {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	primary, err := composition.PrimaryTarget()
	if err != nil {
		return err
	}
	parsed, err := format.ParseGLTFComposition(ctx, raw)
	if err != nil {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	expected, err := dataitem.GLTFCompositionRefs(primary.StringPath(), parsed.Resources)
	if err != nil || len(expected) != len(composition.Refs) {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	for _, ref := range expected {
		found := false
		for _, actual := range composition.Refs {
			if ref.Path == actual.Target.StringPath() && ref.Role == actual.Role && ref.Primary == actual.Primary && ref.Required == actual.Required {
				found = true
				break
			}
		}
		if !found {
			return ErrModel3DGLBSourceAuthorizationRequired
		}
	}
	return nil
}
