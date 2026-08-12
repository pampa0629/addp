package service

import (
	"errors"
	"fmt"

	commonapi "github.com/addp/common/api"
)

var (
	ErrInvalidCodeSetType        = errors.New("invalid code set type")
	ErrDomainParentCycle         = errors.New("domain parent cycle")
	ErrClassificationParentCycle = errors.New("classification parent cycle")
	ErrMetricCategoryParentCycle = errors.New("metric category parent cycle")
	ErrSystemCategoryImmutable   = fmt.Errorf("%w: system measurement category is immutable", commonapi.ErrConflict)
	ErrSystemUnitImmutable       = fmt.Errorf("%w: system unit is immutable", commonapi.ErrConflict)
	ErrSystemCodeSetImmutable    = fmt.Errorf("%w: system code set is immutable", commonapi.ErrConflict)
)
