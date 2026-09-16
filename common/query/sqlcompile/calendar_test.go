package sqlcompile

import (
	"errors"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
	"strings"
	"testing"
)

func TestCalendarRejectsInvalidInputsAndExpansion(t *testing.T) {
	for _, input := range []CheckedExpression{{Type: datatype.FieldTypeString}, {SQL: strings.Repeat("s", plan.MaxBytes), Type: datatype.FieldTypeString}} {
		if _, err := CalendarDate(input, expressionTestDialect{}); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := CalendarDate(CheckedExpression{SQL: "1", Type: datatype.FieldTypeInt}, expressionTestDialect{}); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
		t.Fatal(err)
	}
	if _, err := CalendarMonthStart(CheckedExpression{SQL: "s", Type: datatype.FieldTypeString}, expressionTestDialect{}); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
		t.Fatal(err)
	}
	if _, err := CalendarDate(CheckedExpression{SQL: "s", Type: datatype.FieldTypeString}, nil); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
}

func TestCalendarAddMonthsRejectsInvalidTypesAndExpansion(t *testing.T) {
	date := CheckedExpression{SQL: "d", Type: datatype.FieldTypeDate}
	months := CheckedExpression{SQL: "m", Type: datatype.FieldTypeBigInt}
	if _, err := CalendarAddMonths(date, months, nil); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
	bad := months
	bad.Type = datatype.FieldTypeDecimal
	if _, err := CalendarAddMonths(date, bad, expressionTestDialect{}); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
		t.Fatal(err)
	}
	date.SQL = strings.Repeat("d", plan.MaxBytes/10)
	if _, err := CalendarAddMonths(date, months, expressionTestDialect{}); !errors.Is(err, plugin.ErrAnalyticalInvalid) {
		t.Fatal(err)
	}
}
