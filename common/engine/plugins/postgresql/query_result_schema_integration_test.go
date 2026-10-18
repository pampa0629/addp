package postgresql

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestIntegrationPostgresCatalogPreciseReadOnlyOutputSchema(t *testing.T) {
	db, provider, info := openPostgresPrepareIntegration(t, false)
	t.Cleanup(func() { db.Close() })
	result, err := provider.ExecuteSQL(t.Context(), info, `SELECT 2::integer AS small, 2::bigint AS large, 1.25::numeric AS unbounded, 1.25::numeric(38,18) AS fixed`, plugin.QueryOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Fields) != 4 || result.Fields[0].Type != datatype.FieldTypeInt || result.Fields[1].Type != datatype.FieldTypeBigInt || result.Fields[2].Type != datatype.FieldTypeDecimal || result.Fields[2].Precision != 0 || result.Fields[2].Scale != 0 || result.Fields[3].Precision != 38 || result.Fields[3].Scale != 18 {
		t.Fatalf("native output schema: %+v", result.Fields)
	}
}
