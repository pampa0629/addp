package engineaccess

import (
	"context"
	"strconv"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
)

type searchRevisionFixture struct{}

func (searchRevisionFixture) ReadAuthorizationBasisRevision(context.Context, uint) (*authorizationfacts.Revision, error) {
	return &authorizationfacts.Revision{SchemaVersion: authorizationfacts.RevisionSchemaVersion, Generation: "03c40e08-fcbf-468c-932e-d54da85ca8e7", Version: "1"}, nil
}
func configureSearchObservationFixture(t *testing.T, s *Service) {
	t.Helper()
	if err := s.ConfigureSearchObservation([]byte("0123456789abcdef0123456789abcdef"), searchRevisionFixture{}); err != nil {
		t.Fatal(err)
	}
}

type searchChangingRevisionFixture struct{ calls *int }

func (f searchChangingRevisionFixture) ReadAuthorizationBasisRevision(context.Context, uint) (*authorizationfacts.Revision, error) {
	*f.calls++
	return &authorizationfacts.Revision{SchemaVersion: authorizationfacts.RevisionSchemaVersion, Generation: "03c40e08-fcbf-468c-932e-d54da85ca8e7", Version: strconv.Itoa(*f.calls)}, nil
}
