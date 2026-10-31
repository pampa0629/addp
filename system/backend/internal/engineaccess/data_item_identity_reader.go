package engineaccess

import (
	"context"
	"github.com/addp/common/dataitem/authorizationfacts"
)

type DataItemIdentityReader interface {
	QueryDataItemIdentities(context.Context, uint, authorizationfacts.IdentityQuery) (*authorizationfacts.IdentityPage, error)
}

func (s *Service) WithDataItemIdentityReader(reader DataItemIdentityReader) *Service {
	s.dataItemIdentities = reader
	return s
}
