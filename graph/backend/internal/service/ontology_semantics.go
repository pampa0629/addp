package service

import (
	"fmt"
	"sort"
	"strings"

	"github.com/addp/graph/internal/models"
)

type entityTypeSemantic struct {
	Name            string
	Label           string
	Labels          []string
	DisplayProperty string
}

type ontologySemantics struct {
	entityByShape map[string]entityTypeSemantic
	entityByName  map[string]entityTypeSemantic
	relationLabel map[string]string
	searchable    map[string]struct{}
}

func newOntologySemantics(ontology *models.Ontology) *ontologySemantics {
	semantics := &ontologySemantics{
		entityByShape: make(map[string]entityTypeSemantic),
		entityByName:  make(map[string]entityTypeSemantic),
		relationLabel: make(map[string]string),
		searchable:    make(map[string]struct{}),
	}
	if ontology == nil {
		return semantics
	}

	byID := entityTypeByID(ontology.EntityTypes)
	for index := range ontology.EntityTypes {
		entityType := &ontology.EntityTypes[index]
		label := entityType.Label
		if label == "" {
			label = entityType.Name
		}
		item := entityTypeSemantic{
			Name:            entityType.Name,
			Label:           label,
			Labels:          normalizedStringSet(effectiveNodeLabels(entityType, byID)),
			DisplayProperty: strings.TrimSpace(entityType.DisplayProperty),
		}
		semantics.entityByName[item.Name] = item
		if len(item.Labels) > 0 {
			semantics.entityByShape[nodeLabelsKey(item.Labels)] = item
		}
		properties := collectInheritedProperties(entityType, byID)
		for _, property := range properties {
			if property.Searchable && property.DataType == "string" && strings.TrimSpace(property.Name) != "" {
				semantics.searchable[property.Name] = struct{}{}
			}
		}
		if item.DisplayProperty != "" {
			semantics.searchable[item.DisplayProperty] = struct{}{}
		}
	}
	for _, relationType := range ontology.RelationTypes {
		label := relationType.Label
		if label == "" {
			label = relationType.Name
		}
		semantics.relationLabel[relationType.Name] = label
	}
	return semantics
}

func (s *ontologySemantics) resolveEntityType(labels []string) (string, string) {
	if len(labels) == 0 {
		return "", ""
	}
	if item, ok := s.entityByShape[nodeLabelsKey(labels)]; ok {
		return item.Name, item.Label
	}
	for _, label := range labels {
		if item, ok := s.entityByName[label]; ok {
			return item.Name, item.Label
		}
	}
	shapeName := endpointShapeName(labels)
	return shapeName, shapeName
}

func (s *ontologySemantics) entityType(name string) (entityTypeSemantic, bool) {
	item, ok := s.entityByName[name]
	return item, ok
}

func (s *ontologySemantics) displayName(labels []string, properties map[string]interface{}, fallback string) string {
	item, ok := s.entityByShape[nodeLabelsKey(labels)]
	if !ok {
		for _, label := range labels {
			if candidate, exists := s.entityByName[label]; exists {
				item = candidate
				ok = true
				break
			}
		}
	}
	if ok && item.DisplayProperty != "" {
		if value, exists := properties[item.DisplayProperty]; exists && value != nil {
			name := strings.TrimSpace(fmt.Sprintf("%v", value))
			if name != "" {
				return name
			}
		}
	}
	return fallback
}

func (s *ontologySemantics) searchableProperties() []string {
	properties := make([]string, 0, len(s.searchable))
	for property := range s.searchable {
		properties = append(properties, property)
	}
	sort.Strings(properties)
	return properties
}

type searchIndexDefinition struct {
	Name       string
	EntityType string
	Labels     []string
	Properties []string
}

func buildSearchIndexDefinitions(graphID uint, ontology *models.Ontology) []searchIndexDefinition {
	if ontology == nil {
		return nil
	}
	byID := entityTypeByID(ontology.EntityTypes)
	definitions := make([]searchIndexDefinition, 0, len(ontology.EntityTypes))
	for index := range ontology.EntityTypes {
		entityType := &ontology.EntityTypes[index]
		labels := normalizedStringSet(effectiveNodeLabels(entityType, byID))
		if len(labels) == 0 {
			continue
		}
		properties := collectInheritedProperties(entityType, byID)
		searchable := make([]string, 0, len(properties))
		for _, property := range properties {
			if property.Searchable && property.DataType == "string" && strings.TrimSpace(property.Name) != "" {
				searchable = append(searchable, property.Name)
			}
		}
		if displayProperty := strings.TrimSpace(entityType.DisplayProperty); displayProperty != "" {
			searchable = append(searchable, displayProperty)
		}
		searchable = normalizedStringSet(searchable)
		if len(searchable) == 0 {
			continue
		}
		definitions = append(definitions, searchIndexDefinition{
			Name:       searchIndexName(graphID, entityType.Name),
			EntityType: entityType.Name,
			Labels:     labels,
			Properties: searchable,
		})
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].EntityType < definitions[j].EntityType
	})
	return definitions
}
