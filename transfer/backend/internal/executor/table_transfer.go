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
	TableEndpointQuery   TableEndpointKind = "query"
)

type TableSourcePlan struct {
	Kind         TableEndpointKind
	ConnInfo     engineplugin.ConnectionInfo
	Path         engineplugin.EngineCatalogPath
	Query        string
	RuntimeQuery *engineplugin.QueryRequest
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
	Path              engineplugin.EngineCatalogPath
	DeleteBeforeWrite bool
	ManagedExisting   bool
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

type CRSDefinitionConverter interface {
	ConvertCRSDefinition(ctx context.Context, crsRef string, source *datatype.CRSDefinition, targetEncoding string) (*datatype.CRSDefinition, error)
}

type TableProgressCallback func(context.Context, TableProgressEvent) error

// TableSourceProtector is the Transfer-owned hook that binds an exact native
// source or the same immutable PreparedQuery to a local protection projection.
// The executor owns placement of the returned protector before transforms and
// target writes; Security policy state never enters this package.
type TableSourceProtector interface {
	PrepareCatalogTableProtection(context.Context, engineplugin.EngineCatalogPath, []datatype.FieldInfo) (func(*engineplugin.QueryResult) error, error)
	PrepareQueryProtection(context.Context, engineplugin.PreparedQuery) (func(*engineplugin.QueryResult) error, error)
}

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
	SourceQuerySessionProvider engineplugin.QueryReadSessionProvider
	SourceContentReader        engineplugin.ContentReadableProvider
	SourceTableReadProvider    format.TableReaderProvider
	SourceInfoProvider         format.TableInfoProvider
	SourceMultiReadProvider    format.MultiTableReaderProvider
	SourceScopeReadProvider    format.ScopeTableReaderProvider
	TargetContentWriter        engineplugin.ContentWritableProvider
	TargetTableWriterProvider  format.TableWriterProvider
	TargetMultiProvider        format.MultiTableWriterProvider
	TargetScopeWriterProvider  format.ScopeTableWriterProvider
	TargetDeleteProvider       engineplugin.ResourceDeleteProvider
	TargetNativePreparer       engineplugin.TableWritePreparer
	TargetNativeWriter         engineplugin.BatchWritableProvider
	TargetTableSessionProvider engineplugin.TableWriteSessionProvider
	GeometryBatchReprojecter   GeometryBatchReprojectProvider
	CRSDefinitionConverter     CRSDefinitionConverter
	TargetCRSRequirements      format.CRSDefinitionWriteRequirementProvider
	SourceProtector            TableSourceProtector
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
	executor.SourceQuerySessionProvider, _ = sourcePlugin.(engineplugin.QueryReadSessionProvider)
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
		executor.TargetScopeWriterProvider, _ = format.GetScopeTableWriterProvider(targetFormat)
		executor.TargetCRSRequirements, _ = format.GetCRSDefinitionWriteRequirementProvider(targetFormat)
		if executor.TargetTableWriterProvider != nil {
			executor.TargetMultiProvider = nil
			executor.TargetScopeWriterProvider = nil
		} else if executor.TargetMultiProvider != nil {
			executor.TargetScopeWriterProvider = nil
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
	case TableEndpointQuery:
		if e.SourceQuerySessionProvider == nil {
			return nil, fmt.Errorf("query source requires query read session provider")
		}
		if plan.RuntimeQuery == nil {
			return nil, fmt.Errorf("query source requires runtime query request")
		}
		return &queryTableBatchSource{
			provider: e.SourceQuerySessionProvider, protector: e.SourceProtector,
			connInfo: plan.ConnInfo, request: *plan.RuntimeQuery, tableInfo: plan.TableInfo,
		}, nil
	case TableEndpointNative:
		if e.SourceNativeReader == nil && e.SourceTableSessionProvider == nil {
			return nil, fmt.Errorf("native table source requires batch reader or table read session")
		}
		return &nativeTableBatchSource{
			reader:               e.SourceNativeReader,
			tableSessionProvider: e.SourceTableSessionProvider,
			protector:            e.SourceProtector,
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
		if e.SourceScopeReadProvider != nil {
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
		if plan.Layout == format.LayoutWhole {
			return nil, fmt.Errorf("encoded whole scope table source requires scope table reader provider")
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
	var deleter *engineTargetResourceDeleter
	if plan.Kind != TableEndpointEncoded || e.TargetScopeWriterProvider == nil {
		var err error
		deleter, err = e.targetDeleter(plan)
		if err != nil {
			return nil, err
		}
	}
	switch plan.Kind {
	case TableEndpointEncoded:
		if e.TargetContentWriter == nil && e.TargetScopeWriterProvider == nil {
			return nil, fmt.Errorf("encoded table target requires content writer")
		}
		if e.TargetTableWriterProvider == nil && e.TargetMultiProvider == nil && e.TargetScopeWriterProvider == nil {
			return nil, fmt.Errorf("encoded table target requires table writer provider")
		}
		refBasePath, refPathMapper := encodedTargetRelatedRefMapping(plan.Path)
		return &encodedContentTableTarget{
			writer:              e.TargetContentWriter,
			deleter:             deleter,
			tableWriterProvider: e.TargetTableWriterProvider,
			multiProvider:       e.TargetMultiProvider,
			scopeWriterProvider: e.TargetScopeWriterProvider,
			connInfo:            plan.ConnInfo,
			path:                plan.Path,
			refBasePath:         refBasePath,
			refPathMapper:       refPathMapper,
			writeOptions:        plan.ContentWrite,
			formatOptions:       plan.FormatOptions,
			resumeMarker:        plan.ResumeMarker,
			crsRequirements:     e.TargetCRSRequirements,
			crsConverter:        e.CRSDefinitionConverter,
		}, nil
	case TableEndpointNative:
		if e.TargetNativeWriter == nil && e.TargetTableSessionProvider == nil {
			return nil, fmt.Errorf("native table target requires batch writer or table write session")
		}
		if plan.ManagedExisting && e.TargetTableSessionProvider == nil {
			return nil, fmt.Errorf("managed existing table target requires table write session provider")
		}
		if !plan.ManagedExisting && e.TargetNativePreparer == nil {
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
			managedExisting:      plan.ManagedExisting,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported table target kind %q", plan.Kind)
	}
}

func encodedTargetRelatedRefMapping(path engineplugin.EngineCatalogPath) (string, contentadapter.RefCatalogPathMapper) {
	bucket, objectPath := objectCatalogPathParts(path)
	if bucket == "" || objectPath == "" {
		return path.StringPath(), nil
	}
	return objectPath, contentadapter.SameObjectBucketPathMapper(path)
}

func objectCatalogPathParts(path engineplugin.EngineCatalogPath) (string, string) {
	bucket := ""
	parts := make([]string, 0, len(path.Segments))
	for _, segment := range path.Segments {
		name := strings.Trim(segment.Name, "/")
		if name == "" {
			continue
		}
		if bucket == "" && (segment.Term == engineplugin.EngineCatalogTermBucket || segment.Kind == engineplugin.EngineCatalogKindBucket) {
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
