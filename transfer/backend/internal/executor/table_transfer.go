package executor

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/contentadapter"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/common/resume"
)

type TableEndpointKind string

const (
	TableEndpointNative  TableEndpointKind = "native"
	TableEndpointEncoded TableEndpointKind = "encoded"
)

type TableSourcePlan struct {
	Kind         TableEndpointKind
	ConnInfo     engineplugin.ConnectionInfo
	Path         engineplugin.CatalogPath
	Query        string
	ReadOptions  map[string]interface{}
	ContentRead  engineplugin.ReadOptions
	Format       format.FormatType
	Layout       format.Layout
	ParseOptions *format.ParseOptions
	ResumeMarker *resume.Marker
	TableInfo    *datatype.TableInfo
	SpatialInfo  *datatype.SpatialInfo
	RelatedRefs  []format.RelatedRef
}

type TableTargetPlan struct {
	Kind              TableEndpointKind
	ConnInfo          engineplugin.ConnectionInfo
	Path              engineplugin.CatalogPath
	DeleteBeforeWrite bool
	ContentWrite      engineplugin.WriteOptions
	TablePrepare      engineplugin.TableWriteOptions
	TableWrite        engineplugin.BatchWriteOptions
	Format            format.FormatType
	FormatOptions     *format.WriteOptions
	ResumeMarker      *resume.Marker
}

type TableTransferPlan struct {
	Source           TableSourcePlan
	Target           TableTargetPlan
	Transforms       []TableTransformPlan
	BatchSize        int
	ProgressCallback TableProgressCallback
}

type GeometryBatchReprojectProvider interface {
	ReprojectGeometryBatch(ctx context.Context, geometries [][]byte, sourceCRS, targetCRS, geometryColumn string) ([][]byte, error)
}

type TableProgressCallback func(context.Context, TableProgressEvent) error

type TableProgressEvent struct {
	BatchIndex     int64
	SourceOffset   int64
	BatchRows      int64
	RecordsRead    int64
	RecordsWritten int64
	ResumeMarker   *resume.Marker
	CommitMarker   *resume.Marker
	Final          bool
}

type TableTransformPlan struct {
	Type             string
	FieldMapping     *FieldMappingTransformPlan
	SpatialReproject *SpatialReprojectTransformPlan
}

type SpatialReprojectTransformPlan struct {
	GeometryColumn string
	SourceCRS      string
	TargetCRS      string
	Reproject      bool
}

type FieldMappingMode string

const (
	FieldMappingModeProject     FieldMappingMode = "project"
	FieldMappingModePassthrough FieldMappingMode = "passthrough"
)

type FieldMappingTransformPlan struct {
	Mode   FieldMappingMode
	Fields []FieldMappingFieldPlan
}

type FieldMappingFieldPlan struct {
	Source     string
	Target     string
	TargetType string
	Precision  *int
	Scale      *int
	Nullable   bool
	Default    interface{}
	Format     string
}

type TableTransferExecutor struct {
	SourceNativeReader         engineplugin.BatchReadableProvider
	SourceTableSessionProvider engineplugin.TableReadSessionProvider
	SourceContentReader        engineplugin.ContentReadableProvider
	SourceTableReadProvider    format.TableReaderProvider
	SourceInfoProvider         format.TableInfoProvider
	SourceMultiReadProvider    format.MultiTableReaderProvider
	SourceScopeReadProvider    format.ScopeTableReaderProvider
	TargetContentWriter        engineplugin.ContentWritableProvider
	TargetTableWriterProvider  format.TableWriterProvider
	TargetMultiProvider        format.MultiTableWriterProvider
	TargetDeleteProvider       engineplugin.ResourceDeleteProvider
	TargetNativePreparer       engineplugin.TableWritePreparer
	TargetNativeWriter         engineplugin.BatchWritableProvider
	TargetTableSessionProvider engineplugin.TableWriteSessionProvider
	GeometryBatchReprojecter   GeometryBatchReprojectProvider
}

func NewTableTransferExecutor(sourceEngineType, targetEngineType string, sourceFormat, targetFormat format.FormatType) (*TableTransferExecutor, error) {
	sourcePlugin, err := engineplugin.Get(sourceEngineType)
	if err != nil {
		return nil, fmt.Errorf("get source engine plugin %q: %w", sourceEngineType, err)
	}
	targetPlugin, err := engineplugin.Get(targetEngineType)
	if err != nil {
		return nil, fmt.Errorf("get target engine plugin %q: %w", targetEngineType, err)
	}

	executor := &TableTransferExecutor{}
	if reader, ok := sourcePlugin.(engineplugin.BatchReadableProvider); ok {
		executor.SourceNativeReader = reader
	}
	executor.SourceTableSessionProvider, _ = sourcePlugin.(engineplugin.TableReadSessionProvider)
	if reader, ok := sourcePlugin.(engineplugin.ContentReadableProvider); ok {
		executor.SourceContentReader = reader
	}
	if sourceFormat != "" {
		executor.SourceInfoProvider, _ = format.GetTableInfoProvider(sourceFormat)
		executor.SourceTableReadProvider, _ = format.GetTableReaderProvider(sourceFormat)
		executor.SourceMultiReadProvider, _ = format.GetMultiTableReaderProvider(sourceFormat)
		executor.SourceScopeReadProvider, _ = format.GetScopeTableReaderProvider(sourceFormat)
	}

	if writer, ok := targetPlugin.(engineplugin.ContentWritableProvider); ok {
		executor.TargetContentWriter = writer
	}
	executor.TargetDeleteProvider, _ = targetPlugin.(engineplugin.ResourceDeleteProvider)
	if targetFormat != "" {
		executor.TargetTableWriterProvider, _ = format.GetTableWriterProvider(targetFormat)
		executor.TargetMultiProvider, _ = format.GetMultiTableWriterProvider(targetFormat)
		if executor.TargetTableWriterProvider != nil {
			executor.TargetMultiProvider = nil
		}
	}
	executor.TargetNativePreparer, _ = targetPlugin.(engineplugin.TableWritePreparer)
	if writer, ok := targetPlugin.(engineplugin.BatchWritableProvider); ok {
		executor.TargetNativeWriter = writer
	}
	executor.TargetTableSessionProvider, _ = targetPlugin.(engineplugin.TableWriteSessionProvider)

	return executor, nil
}

func (e *TableTransferExecutor) Execute(ctx context.Context, plan TableTransferPlan) (*TablePipelineMetrics, error) {
	if e == nil {
		return nil, fmt.Errorf("table transfer executor cannot be nil")
	}
	source, err := e.openSource(plan.Source)
	if err != nil {
		return nil, err
	}
	target, err := e.openTarget(plan.Target)
	if err != nil {
		return nil, err
	}
	return (&TablePipeline{
		Source:                   source,
		Target:                   target,
		Transforms:               plan.Transforms,
		BatchSize:                plan.BatchSize,
		ProgressCallback:         plan.ProgressCallback,
		GeometryBatchReprojecter: e.GeometryBatchReprojecter,
	}).Execute(ctx)
}

func (e *TableTransferExecutor) openSource(plan TableSourcePlan) (TableBatchSource, error) {
	switch plan.Kind {
	case TableEndpointNative:
		if e.SourceNativeReader == nil && e.SourceTableSessionProvider == nil {
			return nil, fmt.Errorf("native table source requires batch reader or table read session")
		}
		return &nativeTableBatchSource{
			reader:               e.SourceNativeReader,
			tableSessionProvider: e.SourceTableSessionProvider,
			connInfo:             plan.ConnInfo,
			path:                 plan.Path,
			query:                plan.Query,
			readOptions:          plan.ReadOptions,
			resumeMarker:         plan.ResumeMarker,
			tableInfo:            plan.TableInfo,
			spatialInfo:          plan.SpatialInfo,
		}, nil
	case TableEndpointEncoded:
		if e.SourceContentReader == nil {
			return nil, fmt.Errorf("encoded table source requires content reader")
		}
		if plan.Layout == format.LayoutWhole {
			if e.SourceScopeReadProvider == nil {
				return nil, fmt.Errorf("encoded whole scope table source requires scope table reader provider")
			}
			return &encodedContentTableSource{
				reader:              e.SourceContentReader,
				scopeReaderProvider: e.SourceScopeReadProvider,
				connInfo:            plan.ConnInfo,
				path:                plan.Path,
				readOptions:         plan.ContentRead,
				parseOptions:        plan.ParseOptions,
				resumeMarker:        plan.ResumeMarker,
				tableInfo:           plan.TableInfo,
				spatialInfo:         plan.SpatialInfo,
			}, nil
		}
		if e.SourceTableReadProvider == nil && e.SourceMultiReadProvider == nil {
			return nil, fmt.Errorf("encoded table source requires table reader provider")
		}
		return &encodedContentTableSource{
			reader:              e.SourceContentReader,
			tableProvider:       e.SourceTableReadProvider,
			multiReaderProvider: e.SourceMultiReadProvider,
			infoProvider:        e.SourceInfoProvider,
			connInfo:            plan.ConnInfo,
			path:                plan.Path,
			readOptions:         plan.ContentRead,
			parseOptions:        plan.ParseOptions,
			resumeMarker:        plan.ResumeMarker,
			tableInfo:           plan.TableInfo,
			spatialInfo:         plan.SpatialInfo,
			relatedRefs:         plan.RelatedRefs,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported table source kind %q", plan.Kind)
	}
}

func (e *TableTransferExecutor) openTarget(plan TableTargetPlan) (TableBatchTarget, error) {
	deleter, err := e.targetDeleter(plan)
	if err != nil {
		return nil, err
	}
	switch plan.Kind {
	case TableEndpointEncoded:
		if e.TargetContentWriter == nil {
			return nil, fmt.Errorf("encoded table target requires content writer")
		}
		if e.TargetTableWriterProvider == nil && e.TargetMultiProvider == nil {
			return nil, fmt.Errorf("encoded table target requires table writer provider")
		}
		refBasePath, refPathMapper := encodedTargetRelatedRefMapping(plan.Path)
		return &encodedContentTableTarget{
			writer:              e.TargetContentWriter,
			deleter:             deleter,
			tableWriterProvider: e.TargetTableWriterProvider,
			multiProvider:       e.TargetMultiProvider,
			connInfo:            plan.ConnInfo,
			path:                plan.Path,
			refBasePath:         refBasePath,
			refPathMapper:       refPathMapper,
			writeOptions:        plan.ContentWrite,
			formatOptions:       plan.FormatOptions,
			resumeMarker:        plan.ResumeMarker,
		}, nil
	case TableEndpointNative:
		if e.TargetNativeWriter == nil && e.TargetTableSessionProvider == nil {
			return nil, fmt.Errorf("native table target requires batch writer or table write session")
		}
		if e.TargetNativePreparer == nil {
			return nil, fmt.Errorf("target engine does not implement table write prepare")
		}
		return &nativeTableBatchTarget{
			deleter:              deleter,
			preparer:             e.TargetNativePreparer,
			writer:               e.TargetNativeWriter,
			tableSessionProvider: e.TargetTableSessionProvider,
			connInfo:             plan.ConnInfo,
			path:                 plan.Path,
			prepareOptions:       plan.TablePrepare,
			writeOptions:         plan.TableWrite,
			resumeMarker:         plan.ResumeMarker,
			replace:              plan.DeleteBeforeWrite,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported table target kind %q", plan.Kind)
	}
}

func encodedTargetRelatedRefMapping(path engineplugin.CatalogPath) (string, contentadapter.RefCatalogPathMapper) {
	bucket, objectPath := objectCatalogPathParts(path)
	if bucket == "" || objectPath == "" {
		return path.StringPath(), nil
	}
	return objectPath, contentadapter.SameObjectBucketPathMapper(path)
}

func objectCatalogPathParts(path engineplugin.CatalogPath) (string, string) {
	bucket := ""
	parts := make([]string, 0, len(path.Segments))
	for _, segment := range path.Segments {
		name := strings.Trim(segment.Name, "/")
		if name == "" {
			continue
		}
		if bucket == "" && (segment.Term == engineplugin.CatalogTermBucket || segment.Kind == engineplugin.CatalogKindBucket) {
			bucket = name
			continue
		}
		if bucket != "" {
			parts = append(parts, name)
		}
	}
	return bucket, strings.Join(parts, "/")
}

func (e *TableTransferExecutor) targetDeleter(plan TableTargetPlan) (*engineTargetResourceDeleter, error) {
	if !plan.DeleteBeforeWrite {
		return nil, nil
	}
	if e.TargetDeleteProvider == nil {
		return nil, fmt.Errorf("target engine does not implement resource delete")
	}
	return &engineTargetResourceDeleter{
		provider: e.TargetDeleteProvider,
		connInfo: plan.ConnInfo,
		path:     plan.Path,
	}, nil
}
