package service

import "github.com/addp/model/internal/models"

func deriveStructuralConstraints(tableID int64, fields []models.LogicalField, relations []models.TableRelationDetail) models.LogicalTableStructuralConstraints {
	constraints := models.LogicalTableStructuralConstraints{
		PrimaryKey:     make([]models.LogicalConstraintField, 0),
		RequiredFields: make([]models.LogicalConstraintField, 0),
		ForeignKeys:    make([]models.TableRelationDetail, 0),
	}
	for _, field := range fields {
		reference := models.LogicalConstraintField{FieldID: field.ID, ColumnName: field.ColumnName}
		if field.IsPK {
			constraints.PrimaryKey = append(constraints.PrimaryKey, reference)
		}
		if field.IsPK || !field.Nullable {
			constraints.RequiredFields = append(constraints.RequiredFields, reference)
		}
	}
	for _, relation := range relations {
		if relation.SourceTable == tableID && relation.RelationType == "fk" {
			constraints.ForeignKeys = append(constraints.ForeignKeys, relation)
		}
	}
	return constraints
}
