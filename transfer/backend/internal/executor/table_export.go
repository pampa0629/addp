package executor

import (
	"context"
	"fmt"
	"sort"
	"strings"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/common/resource"
)

const defaultBatchSize = 1000

type TableExportPlan struct {
	SourceConnInfo engineplugin.ConnectionInfo
	SourcePath     engineplugin.CatalogPath
	SourceQuery    string

	TargetConnInfo engineplugin.ConnectionInfo
	TargetPath     engineplugin.CatalogPath
	TargetWrite    engineplugin.WriteOptions

	Format       format.FormatType
	BatchSize    int
	WriteOptions *format.WriteOptions
	ReadOptions  map[string]interface{}
}

type TableExportMetrics struct {
	RecordsRead    int64
	RecordsWritten int64
	Batches        int64
}

type TableExportExecutor struct {
	Reader               engineplugin.BatchReadableProvider
	TableSessionProvider engineplugin.TableReadSessionProvider
	Writer               engineplugin.ContentWritableProvider
	FormatProvider       format.TableWriterProvider
	ComponentProvider    format.ComponentTableWriterProvider
}

func NewTableExportExecutor(sourceEngineType, targetEngineType string, formatType format.FormatType) (*TableExportExecutor, error) {
	sourcePlugin, err := engineplugin.Get(sourceEngineType)
	if err != nil {
		return nil, fmt.Errorf("get source engine plugin %q: %w", sourceEngineType, err)
	}
	reader, ok := sourcePlugin.(engineplugin.BatchReadableProvider)
	if !ok {
		return nil, fmt.Errorf("source engine %q does not implement batch table read", sourceEngineType)
	}
	tableSessionProvider, _ := sourcePlugin.(engineplugin.TableReadSessionProvider)

	targetPlugin, err := engineplugin.Get(targetEngineType)
	if err != nil {
		return nil, fmt.Errorf("get target engine plugin %q: %w", targetEngineType, err)
	}
	writer, ok := targetPlugin.(engineplugin.ContentWritableProvider)
	if !ok {
		return nil, fmt.Errorf("target engine %q does not implement content write", targetEngineType)
	}

	formatProvider, tableWriterErr := format.GetTableWriterProvider(formatType)
	componentProvider, componentWriterErr := format.GetComponentTableWriterProvider(formatType)
	if tableWriterErr != nil && componentWriterErr != nil {
		return nil, fmt.Errorf("get table writer provider %q: %w", formatType, tableWriterErr)
	}

	return &TableExportExecutor{
		Reader:               reader,
		TableSessionProvider: tableSessionProvider,
		Writer:               writer,
		FormatProvider:       formatProvider,
		ComponentProvider:    componentProvider,
	}, nil
}

func (e *TableExportExecutor) Execute(ctx context.Context, plan TableExportPlan) (*TableExportMetrics, error) {
	if err := validateTableExportExecutor(e); err != nil {
		return nil, err
	}
	if err := validateTableExportPlan(plan); err != nil {
		return nil, err
	}
	if err := validateTableExportFormat(plan.Format, e); err != nil {
		return nil, err
	}

	metrics, err := (&TablePipeline{
		Source: &nativeTableBatchSource{
			reader:               e.Reader,
			tableSessionProvider: e.TableSessionProvider,
			connInfo:             plan.SourceConnInfo,
			path:                 plan.SourcePath,
			query:                plan.SourceQuery,
			readOptions:          plan.ReadOptions,
		},
		Target: &encodedContentTableTarget{
			writer:            e.Writer,
			formatProvider:    e.FormatProvider,
			componentProvider: e.ComponentProvider,
			connInfo:          plan.TargetConnInfo,
			path:              plan.TargetPath,
			writeOptions:      plan.TargetWrite,
			formatOptions:     plan.WriteOptions,
		},
		BatchSize: plan.BatchSize,
	}).Execute(ctx)
	return tableExportMetrics(metrics), err
}

func validateTableExportFormat(planFormat format.FormatType, e *TableExportExecutor) error {
	if planFormat == "" {
		return nil
	}
	providerFormat := format.FormatType("")
	if e.FormatProvider != nil {
		providerFormat = e.FormatProvider.Format()
	}
	if e.ComponentProvider != nil {
		providerFormat = e.ComponentProvider.Format()
	}
	if planFormat != providerFormat {
		return fmt.Errorf("table export format %q does not match table writer provider format %q", planFormat, providerFormat)
	}
	return nil
}

func validateTableExportExecutor(e *TableExportExecutor) error {
	if e == nil {
		return fmt.Errorf("table export executor cannot be nil")
	}
	if e.Reader == nil {
		return fmt.Errorf("table export executor requires source batch reader")
	}
	if e.Writer == nil {
		return fmt.Errorf("table export executor requires target content writer")
	}
	if e.FormatProvider == nil && e.ComponentProvider == nil {
		return fmt.Errorf("table export executor requires table writer provider")
	}
	if e.FormatProvider != nil && (e.FormatProvider.Format() == "" || e.FormatProvider.Format() == format.FormatUnknown) {
		return fmt.Errorf("table export executor requires concrete table writer format")
	}
	if e.ComponentProvider != nil && (e.ComponentProvider.Format() == "" || e.ComponentProvider.Format() == format.FormatUnknown) {
		return fmt.Errorf("table export executor requires concrete component table writer format")
	}
	return nil
}

func validateTableExportPlan(plan TableExportPlan) error {
	if plan.BatchSize < 0 {
		return fmt.Errorf("table export batch size cannot be negative")
	}
	if plan.Format != "" && plan.Format == format.FormatUnknown {
		return fmt.Errorf("table export format cannot be unknown")
	}
	return nil
}

func tableExportMetrics(metrics *TablePipelineMetrics) *TableExportMetrics {
	if metrics == nil {
		return nil
	}
	return &TableExportMetrics{
		RecordsRead:    metrics.RecordsRead,
		RecordsWritten: metrics.RecordsWritten,
		Batches:        metrics.Batches,
	}
}

func tableInfoFromBatch(batch *engineplugin.BatchData) *format.TableInfo {
	info := &format.TableInfo{}
	if batch == nil {
		return info
	}
	info.Fields = make([]format.FieldInfo, 0, len(batch.Fields))
	for _, field := range batch.Fields {
		name := field.Name
		if name == "" {
			continue
		}
		info.Fields = append(info.Fields, format.FieldInfo{
			Name:         name,
			Type:         format.FieldType(field.Type),
			OriginalType: field.NativeType,
			Nullable:     field.Nullable,
			IsPrimaryKey: field.PrimaryKey,
			Comment:      field.Comment,
		})
	}
	if len(info.Fields) == 0 && len(batch.Rows) > 0 {
		names := make([]string, 0, len(batch.Rows[0]))
		for name := range batch.Rows[0] {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			info.Fields = append(info.Fields, format.FieldInfo{Name: name, Type: format.FieldTypeUnknown})
		}
	}
	return info
}

func resourceRefFromCatalogPath(path engineplugin.CatalogPath) resource.ResourceRef {
	stringPath := path.StringPath()
	return resource.NewResourceRef(stringPath, resource.ResourceRoleMain)
}

func applySpatialInfoFromOptions(info *format.TableInfo, opts *format.WriteOptions) {
	if info == nil || opts == nil || opts.ExtraParams == nil {
		return
	}
	geometryField := optionString(opts.ExtraParams, "geometry_field")
	geometryType := optionString(opts.ExtraParams, "geometry_type")
	if geometryField == "" && geometryType == "" {
		return
	}
	if info.SpatialInfo == nil {
		info.SpatialInfo = &format.SpatialInfo{}
	}
	if geometryField != "" {
		info.SpatialInfo.GeometryColumn = geometryField
		for i := range info.Fields {
			if strings.EqualFold(info.Fields[i].Name, geometryField) {
				info.Fields[i].Type = format.FieldTypeGeometry
				break
			}
		}
	}
	if geometryType != "" {
		info.SpatialInfo.GeometryType = geometryType
	}
}

func optionString(values map[string]interface{}, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}
