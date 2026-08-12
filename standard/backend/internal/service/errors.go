package service

import (
	"errors"
	"fmt"

	commonapi "github.com/addp/common/api"
)

var (
	ErrInvalidCodeSetType            = errors.New("invalid code set type")
	ErrDomainParentCycle             = errors.New("domain parent cycle")
	ErrDomainReferenced              = fmt.Errorf("%w: domain is referenced", commonapi.ErrConflict)
	ErrClassificationParentCycle     = errors.New("classification parent cycle")
	ErrClassificationReferenced      = fmt.Errorf("%w: classification is referenced", commonapi.ErrConflict)
	ErrMetricCategoryParentCycle     = errors.New("metric category parent cycle")
	ErrMetricCategoryReferenced      = fmt.Errorf("%w: metric category is referenced", commonapi.ErrConflict)
	ErrMeasurementCategoryReferenced = fmt.Errorf("%w: measurement category is referenced", commonapi.ErrConflict)
	ErrUnitReferenced                = fmt.Errorf("%w: unit is referenced", commonapi.ErrConflict)
	ErrCodeSetReferenced             = fmt.Errorf("%w: code set is referenced", commonapi.ErrConflict)
	ErrCodeItemReferenced            = fmt.Errorf("%w: code item is referenced", commonapi.ErrConflict)
	ErrMetricReferenced              = fmt.Errorf("%w: metric is referenced", commonapi.ErrConflict)
	ErrSystemCategoryImmutable       = fmt.Errorf("%w: system measurement category is immutable", commonapi.ErrConflict)
	ErrSystemUnitImmutable           = fmt.Errorf("%w: system unit is immutable", commonapi.ErrConflict)
	ErrSystemCodeSetImmutable        = fmt.Errorf("%w: system code set is immutable", commonapi.ErrConflict)
)

func mapDeleteConflict(err, referencedError error) error {
	if errors.Is(err, commonapi.ErrConflict) {
		return referencedError
	}
	return err
}
