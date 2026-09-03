package service

import (
	"errors"
	"fmt"

	commonapi "github.com/addp/common/api"
)

var (
	ErrInvalidStandardScope          = fmt.Errorf("%w: invalid standard scope", commonapi.ErrBadRequest)
	ErrInvalidStandardRevision       = fmt.Errorf("%w: invalid standard revision", commonapi.ErrBadRequest)
	ErrInvalidRevisionTransition     = fmt.Errorf("%w: invalid standard revision transition", commonapi.ErrConflict)
	ErrEffectiveIntervalConflict     = fmt.Errorf("%w: standard revision effective interval conflicts with an existing published revision", commonapi.ErrConflict)
	ErrDraftRevisionExists           = fmt.Errorf("%w: a draft revision already exists", commonapi.ErrConflict)
	ErrPublishedRevisionRequired     = fmt.Errorf("%w: published standard revision required", commonapi.ErrBadRequest)
	ErrPlatformCodeSetImmutable      = fmt.Errorf("%w: platform code set is immutable", commonapi.ErrConflict)
	ErrDomainParentCycle             = errors.New("domain parent cycle")
	ErrDomainReferenced              = fmt.Errorf("%w: domain is referenced", commonapi.ErrConflict)
	ErrMetricCategoryParentCycle     = errors.New("metric category parent cycle")
	ErrMetricCategoryReferenced      = fmt.Errorf("%w: metric category is referenced", commonapi.ErrConflict)
	ErrMeasurementCategoryReferenced = fmt.Errorf("%w: measurement category is referenced", commonapi.ErrConflict)
	ErrUnitReferenced                = fmt.Errorf("%w: unit is referenced", commonapi.ErrConflict)
	ErrCodeSetReferenced             = fmt.Errorf("%w: code set is referenced", commonapi.ErrConflict)
	ErrCodeItemReferenced            = fmt.Errorf("%w: code item is referenced", commonapi.ErrConflict)
	ErrMetricReferenced              = fmt.Errorf("%w: metric is referenced", commonapi.ErrConflict)
	ErrSystemCategoryImmutable       = fmt.Errorf("%w: system measurement category is immutable", commonapi.ErrConflict)
	ErrSystemUnitImmutable           = fmt.Errorf("%w: system unit is immutable", commonapi.ErrConflict)
)

func mapDeleteConflict(err, referencedError error) error {
	if errors.Is(err, commonapi.ErrConflict) {
		return referencedError
	}
	return err
}
