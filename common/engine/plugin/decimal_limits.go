package plugin

import (
	"fmt"

	"github.com/addp/common/datatype"
)

// ApplyExplicitDecimalTableWriteLimits declares the bounded decimal definition
// that a TableWritePreparer guarantees to validate and write.
func ApplyExplicitDecimalTableWriteLimits(capabilities *EngineCapabilities, maxPrecision, maxScale int) {
	if capabilities == nil {
		return
	}
	if capabilities.Limits == nil {
		capabilities.Limits = &EngineLimits{}
	}
	if capabilities.Limits.TableWrite == nil {
		capabilities.Limits.TableWrite = &TableWriteLimits{}
	}
	capabilities.Limits.TableWrite.Decimal = &DecimalFieldLimits{
		RequiresExplicitPrecisionScale: true,
		MaxPrecision:                   Int(maxPrecision),
		MaxScale:                       Int(maxScale),
	}
}

// ValidateExplicitDecimalFieldDefinition applies the same contract exposed by
// limits.table_write.decimal to one canonical decimal field definition.
func ValidateExplicitDecimalFieldDefinition(engineType string, field datatype.FieldInfo, maxPrecision, maxScale int) error {
	if field.Precision <= 0 {
		return fmt.Errorf("%s decimal field %q requires explicit precision and scale", engineType, field.Name)
	}
	if field.Precision > maxPrecision {
		return fmt.Errorf("%s decimal field %q precision %d exceeds maximum %d", engineType, field.Name, field.Precision, maxPrecision)
	}
	if field.Scale < 0 || field.Scale > maxScale {
		return fmt.Errorf("%s decimal field %q scale %d must be between 0 and %d", engineType, field.Name, field.Scale, maxScale)
	}
	if field.Scale > field.Precision {
		return fmt.Errorf("%s decimal field %q scale %d exceeds precision %d", engineType, field.Name, field.Scale, field.Precision)
	}
	return nil
}
