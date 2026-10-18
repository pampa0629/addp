package plugin

import "context"

// ReadSessionEvidence is an optional proof on the actual opened read session,
// not a second read path or an authorization. The returned set describes this
// session's bound source closure, not a separately prepared query. Implementors
// return a deep copy or ErrQueryReadSetUnresolved, including after Close.
type ReadSessionEvidence interface {
	ReadSet(context.Context) (*QueryReadSet, error)
}
