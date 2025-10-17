package scanner

import (
	"context"

	sdk "github.com/addp/meta-extractor-sdk"
)

// SDKExtractorAdapter adapts SDK extractors to internal scanner interface
// This allows third-party plugins built with the SDK to work with ADDP's internal types
type SDKExtractorAdapter struct {
	sdkExtractor sdk.MetadataExtractor
}

// NewSDKExtractorAdapter creates an adapter for an SDK-based extractor
func NewSDKExtractorAdapter(sdkExtractor sdk.MetadataExtractor) MetadataExtractor {
	return &SDKExtractorAdapter{
		sdkExtractor: sdkExtractor,
	}
}

// SupportedTypes delegates to SDK extractor
func (a *SDKExtractorAdapter) SupportedTypes() []string {
	return a.sdkExtractor.SupportedTypes()
}

// Priority delegates to SDK extractor
func (a *SDKExtractorAdapter) Priority() int {
	return a.sdkExtractor.Priority()
}

// Extract adapts SDK input/output to internal types
func (a *SDKExtractorAdapter) Extract(ctx context.Context, input ExtractInput) (*Metadata, error) {
	// Convert internal ExtractInput to SDK ExtractInput
	sdkInput := sdk.ExtractInput{
		ResourceID:   input.ResourceID,
		ObjectKey:    input.ObjectKey,
		ContentType:  input.ContentType,
		Size:         input.Size,
		Reader:       input.Reader,
		Metadata:     input.Metadata,
		LastModified: input.LastModified,
		ETag:         input.ETag,
	}

	// Call SDK extractor
	sdkMetadata, err := a.sdkExtractor.Extract(ctx, sdkInput)
	if err != nil {
		return nil, err
	}

	// Convert SDK Metadata to internal Metadata
	metadata := &Metadata{
		BasicInfo: BasicMetadata{
			FileName:     sdkMetadata.BasicInfo.FileName,
			FileType:     sdkMetadata.BasicInfo.FileType,
			Size:         sdkMetadata.BasicInfo.Size,
			ContentType:  sdkMetadata.BasicInfo.ContentType,
			Encoding:     sdkMetadata.BasicInfo.Encoding,
			LastModified: sdkMetadata.BasicInfo.LastModified,
			Checksum:     sdkMetadata.BasicInfo.Checksum,
			ETag:         sdkMetadata.BasicInfo.ETag,
		},
		CustomAttrs: sdkMetadata.CustomAttrs,
		PreviewData: sdkMetadata.PreviewData,
	}

	// Convert SchemaInfo if present
	if sdkMetadata.SchemaInfo != nil {
		metadata.SchemaInfo = &SchemaMetadata{
			Columns:    convertSDKColumns(sdkMetadata.SchemaInfo.Columns),
			RowCount:   sdkMetadata.SchemaInfo.RowCount,
			SampleData: sdkMetadata.SchemaInfo.SampleData,
			Extra:      sdkMetadata.SchemaInfo.Extra,
		}
	}

	return metadata, nil
}

// convertSDKColumns converts SDK ColumnInfo to internal ColumnInfo
func convertSDKColumns(sdkColumns []sdk.ColumnInfo) []ColumnInfo {
	columns := make([]ColumnInfo, len(sdkColumns))
	for i, col := range sdkColumns {
		columns[i] = ColumnInfo{
			Name:        col.Name,
			Type:        col.Type,
			Nullable:    col.Nullable,
			Description: col.Description,
			Example:     col.Example,
		}
	}
	return columns
}

// RegisterSDKExtractor registers an SDK-based extractor with the internal registry
// This is the bridge function that third-party plugins use
func RegisterSDKExtractor(sdkExtractor sdk.MetadataExtractor) {
	adapter := NewSDKExtractorAdapter(sdkExtractor)
	Register(adapter)
}

// Helper types to maintain backward compatibility

// GeoMetadata is deprecated - use SDK types instead
// Kept for backward compatibility with existing internal extractors
type GeoMetadata = sdk.GeoSpatialMetadata

// ImageMetadata is deprecated - use SDK types instead
type ImageMetadata = sdk.ImageMetadata

// DocumentMetadata is deprecated - use SDK types instead
type DocumentMetadata = sdk.DocumentMetadata
