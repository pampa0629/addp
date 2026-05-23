package jsonformat

import "sort"

type metadataBuilder struct {
	geometryTypes map[string]struct{}
	propertySet   map[string]struct{}
	bounds        geometryBounds
}

func newMetadataBuilder() *metadataBuilder {
	return &metadataBuilder{
		geometryTypes: make(map[string]struct{}),
		propertySet:   make(map[string]struct{}),
	}
}

func (b *metadataBuilder) AddFeature(feature *Feature) {
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

func (b *metadataBuilder) Build() map[string]interface{} {
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

func (b *metadataBuilder) HasGeometry() bool {
	return len(b.geometryTypes) > 0
}

func (b *metadataBuilder) BoundingBox() ([4]float64, bool) {
	return b.bounds.BoundingBox()
}
