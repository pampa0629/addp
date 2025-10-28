package format

import (
	"testing"
)

func TestSchemaValidate(t *testing.T) {
	tests := []struct {
		name    string
		schema  *Schema
		wantErr bool
	}{
		{
			name: "Valid schema",
			schema: &Schema{
				Fields: []Field{
					{Name: "id", Type: FieldTypeInt},
					{Name: "name", Type: FieldTypeString},
				},
				PrimaryKey: []string{"id"},
			},
			wantErr: false,
		},
		{
			name: "Empty fields",
			schema: &Schema{
				Fields: []Field{},
			},
			wantErr: true,
		},
		{
			name: "Duplicate field names",
			schema: &Schema{
				Fields: []Field{
					{Name: "id", Type: FieldTypeInt},
					{Name: "id", Type: FieldTypeString}, // duplicate
				},
			},
			wantErr: true,
		},
		{
			name: "Empty field name",
			schema: &Schema{
				Fields: []Field{
					{Name: "", Type: FieldTypeInt},
				},
			},
			wantErr: true,
		},
		{
			name: "Primary key field not found",
			schema: &Schema{
				Fields: []Field{
					{Name: "id", Type: FieldTypeInt},
				},
				PrimaryKey: []string{"nonexistent"},
			},
			wantErr: true,
		},
		{
			name: "Geometry field not found",
			schema: &Schema{
				Fields: []Field{
					{Name: "id", Type: FieldTypeInt},
				},
				GeometryField: stringPtr("geom"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.schema.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Schema.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSchemaGetField(t *testing.T) {
	schema := &Schema{
		Fields: []Field{
			{Name: "id", Type: FieldTypeInt},
			{Name: "name", Type: FieldTypeString},
			{Name: "geom", Type: FieldTypePoint},
		},
	}

	tests := []struct {
		name      string
		fieldName string
		wantFound bool
		wantType  FieldType
	}{
		{"Existing field", "name", true, FieldTypeString},
		{"Another existing field", "geom", true, FieldTypePoint},
		{"Non-existing field", "age", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := schema.GetField(tt.fieldName)
			if (field != nil) != tt.wantFound {
				t.Errorf("GetField(%q) found = %v, want %v", tt.fieldName, field != nil, tt.wantFound)
			}
			if field != nil && field.Type != tt.wantType {
				t.Errorf("GetField(%q).Type = %v, want %v", tt.fieldName, field.Type, tt.wantType)
			}
		})
	}
}

func TestSchemaIsPrimaryKey(t *testing.T) {
	schema := &Schema{
		Fields: []Field{
			{Name: "id", Type: FieldTypeInt},
			{Name: "name", Type: FieldTypeString},
		},
		PrimaryKey: []string{"id"},
	}

	tests := []struct {
		fieldName string
		want      bool
	}{
		{"id", true},
		{"name", false},
		{"nonexistent", false},
	}

	for _, tt := range tests {
		t.Run(tt.fieldName, func(t *testing.T) {
			got := schema.IsPrimaryKey(tt.fieldName)
			if got != tt.want {
				t.Errorf("IsPrimaryKey(%q) = %v, want %v", tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestSchemaIsGeospatial(t *testing.T) {
	tests := []struct {
		name   string
		schema *Schema
		want   bool
	}{
		{
			name: "Geospatial schema",
			schema: &Schema{
				Fields:        []Field{{Name: "geom", Type: FieldTypePoint}},
				GeometryField: stringPtr("geom"),
			},
			want: true,
		},
		{
			name: "Non-geospatial schema",
			schema: &Schema{
				Fields: []Field{{Name: "id", Type: FieldTypeInt}},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.schema.IsGeospatial()
			if got != tt.want {
				t.Errorf("IsGeospatial() = %v, want %v", got, tt.want)
			}
		})
	}
}

