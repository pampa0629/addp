package sdk

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// MetadataTypeRegistry manages typed metadata schemas
type MetadataTypeRegistry struct {
	types map[string]TypedMetadata // key: type name
	mu    sync.RWMutex
}

var defaultMetadataRegistry = &MetadataTypeRegistry{
	types: make(map[string]TypedMetadata),
}

func init() {
	// Register built-in typed metadata
	RegisterMetadataType(&GeoSpatialMetadata{})
	RegisterMetadataType(&ImageMetadata{})
	RegisterMetadataType(&DocumentMetadata{})
}

// RegisterMetadataType registers a typed metadata structure
// This should be called in the init() function of your extractor package
func RegisterMetadataType(metadata TypedMetadata) {
	defaultMetadataRegistry.Register(metadata)
}

// Register registers a typed metadata to this registry
func (r *MetadataTypeRegistry) Register(metadata TypedMetadata) {
	r.mu.Lock()
	defer r.mu.Unlock()

	typeName := metadata.TypeName()
	r.types[typeName] = metadata
}

// Get retrieves a typed metadata by name
func (r *MetadataTypeRegistry) Get(typeName string) (TypedMetadata, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	meta, ok := r.types[typeName]
	return meta, ok
}

// GetMetadataType retrieves a typed metadata by name from the default registry
func GetMetadataType(typeName string) (TypedMetadata, bool) {
	return defaultMetadataRegistry.Get(typeName)
}

// SerializeTypedMetadata converts typed metadata to a JSON-serializable map
// This includes the type information and schema validation
func SerializeTypedMetadata(metadata TypedMetadata) map[string]interface{} {
	return map[string]interface{}{
		"_type":   metadata.TypeName(),
		"_schema": metadata.Schema(),
		"data":    metadata.ToMap(),
	}
}

// DeserializeTypedMetadata reconstructs typed metadata from a map
func DeserializeTypedMetadata(m map[string]interface{}) (TypedMetadata, error) {
	typeName, ok := m["_type"].(string)
	if !ok {
		return nil, fmt.Errorf("missing _type field")
	}

	metadata, ok := GetMetadataType(typeName)
	if !ok {
		return nil, fmt.Errorf("unknown metadata type: %s", typeName)
	}

	data, ok := m["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("missing data field")
	}

	if err := metadata.FromMap(data); err != nil {
		return nil, fmt.Errorf("failed to deserialize: %w", err)
	}

	return metadata, nil
}

// ValidateMetadata validates typed metadata against its JSON schema
func ValidateMetadata(metadata TypedMetadata) error {
	// Simple validation: check if ToMap and FromMap round-trip successfully
	data := metadata.ToMap()
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal: %w", err)
	}

	var reconstructed map[string]interface{}
	if err := json.Unmarshal(jsonData, &reconstructed); err != nil {
		return fmt.Errorf("failed to unmarshal: %w", err)
	}

	// For more rigorous validation, integrate with a JSON Schema validator library
	// For now, we just check round-trip consistency
	return nil
}

// Helper functions for building metadata

// NewMetadata creates a new Metadata structure with commonly used fields
func NewMetadata(fileName, fileType string, size int64) *Metadata {
	return &Metadata{
		BasicInfo: BasicMetadata{
			FileName: fileName,
			FileType: fileType,
			Size:     size,
		},
		CustomAttrs: make(map[string]interface{}),
	}
}

// AddTypedMetadata adds typed metadata to the Metadata structure
// The typed metadata will be serialized with type information
func (m *Metadata) AddTypedMetadata(key string, typedMeta TypedMetadata) {
	if m.CustomAttrs == nil {
		m.CustomAttrs = make(map[string]interface{})
	}
	m.CustomAttrs[key] = SerializeTypedMetadata(typedMeta)
}

// GetTypedMetadata retrieves typed metadata from the Metadata structure
func (m *Metadata) GetTypedMetadata(key string) (TypedMetadata, error) {
	if m.CustomAttrs == nil {
		return nil, fmt.Errorf("no custom attributes")
	}

	data, ok := m.CustomAttrs[key]
	if !ok {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid data format")
	}

	return DeserializeTypedMetadata(dataMap)
}

// Common utility functions for extractors

// InferDataType infers the data type from a value
func InferDataType(value interface{}) string {
	if value == nil {
		return "null"
	}

	switch value.(type) {
	case bool:
		return "boolean"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "integer"
	case float32, float64:
		return "number"
	case string:
		return "string"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		return "unknown"
	}
}

// CalculateChecksum calculates MD5 checksum from a reader
// Note: This consumes the reader, so use with caution
func CalculateChecksum(reader io.Reader) (string, error) {
	// Placeholder - implement if needed in SDK
	// For now, extractors can implement their own checksum logic
	return "", fmt.Errorf("not implemented in SDK - implement in your extractor if needed")
}
