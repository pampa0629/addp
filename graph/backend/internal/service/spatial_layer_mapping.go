package service

import "github.com/addp/graph/internal/models"

type SpatialLayerMapping struct {
	EntityTypeName  string
	EntityTypeLabel string
	Config          *models.SpatialLayerConfig
	NodeLabels      []string
}

func buildSpatialLayerMappingsByEntityType(entityTypes []models.EntityType) map[string]SpatialLayerMapping {
	byID := entityTypeByID(entityTypes)
	result := make(map[string]SpatialLayerMapping)
	for i := range entityTypes {
		et := &entityTypes[i]
		cfg := effectiveSpatialLayerConfig(et, byID, 0)
		if cfg == nil {
			continue
		}
		result[et.Name] = SpatialLayerMapping{
			EntityTypeName:  et.Name,
			EntityTypeLabel: et.Label,
			Config:          cfg,
			NodeLabels:      effectiveNodeLabels(et, byID),
		}
	}
	return result
}

func buildSpatialLayerMappingsByLayerName(entityTypes []models.EntityType) map[string]SpatialLayerMapping {
	byEntityType := buildSpatialLayerMappingsByEntityType(entityTypes)
	result := make(map[string]SpatialLayerMapping, len(byEntityType))
	for _, mapping := range byEntityType {
		if mapping.Config == nil || mapping.Config.LayerName == "" {
			continue
		}
		result[mapping.Config.LayerName] = mapping
	}
	return result
}

func effectiveSpatialLayerConfig(et *models.EntityType, byID map[uint]*models.EntityType, depth int) *models.SpatialLayerConfig {
	if et == nil || depth > 16 {
		return nil
	}
	if et.IsSpatialLayer {
		return et.ParsedSpatialLayerConfig()
	}
	if et.ParentID == nil {
		return nil
	}
	parent, ok := byID[*et.ParentID]
	if !ok {
		return nil
	}
	parentCfg := effectiveSpatialLayerConfig(parent, byID, depth+1)
	if parentCfg == nil {
		return nil
	}
	cfg := *parentCfg
	cfg.LayerName = et.Name
	return &cfg
}

func spatialLayerInfoFromMapping(layerName string, mapping SpatialLayerMapping) models.SpatialLayerInfo {
	info := models.SpatialLayerInfo{Name: layerName}
	if mapping.Config != nil {
		cfg := *mapping.Config
		if cfg.LayerName == "" {
			cfg.LayerName = layerName
		}
		info.Config = &cfg
	}
	info.EntityType = mapping.EntityTypeName
	info.EntityTypeLabel = mapping.EntityTypeLabel
	info.NodeLabels = append([]string(nil), mapping.NodeLabels...)
	return info
}
