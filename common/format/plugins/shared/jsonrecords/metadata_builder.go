package jsonrecords

import "sort"

type MetadataBuilder struct {
	geometryTypes map[string]struct{}
	propertySet   map[string]struct{}
	bounds        geometryBounds
}

func NewMetadataBuilder() *MetadataBuilder {
	return &MetadataBuilder{
		geometryTypes: make(map[string]struct{}),
		propertySet:   make(map[string]struct{}),
	}
}

func (b *MetadataBuilder) AddFeature(feature *Feature) {
	if feature == nil {
		return
	}
	if gt := feature.GeometryType(); gt != "" {
		b.geometryTypes[gt] = struct{}{}
		b.bounds.AddGeometry(feature.Geometry)
	}
	for key := range feature.Properties {
		if key == "" {
			continue
		}
		b.propertySet[key] = struct{}{}
	}
}

func (b *MetadataBuilder) Build() map[string]interface{} {
	meta := map[string]interface{}{}
	if len(b.propertySet) > 0 {
		props := make([]string, 0, len(b.propertySet))
		for name := range b.propertySet {
			props = append(props, name)
		}
		sort.Strings(props)
		meta["properties"] = props
	}
	return meta
}

func (b *MetadataBuilder) HasGeometry() bool {
	return len(b.geometryTypes) > 0
}

func (b *MetadataBuilder) BoundingBox() ([4]float64, bool) {
	return b.bounds.BoundingBox()
}
