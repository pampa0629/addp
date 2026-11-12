package production

import (
	"errors"
	"regexp"
)

var ownerName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
var executionPermission = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*\.execute$`)

// Owner is server composition, never request data. Permission keys remain in
// each native authorization manifest. One owner serves all its operators and
// scheduled executions through one minimal receipt route.
type Owner struct {
	Module              string
	ClientID            string
	ReceiptPermission   string
	ExecutionPermission string
}

type Registry struct{ owners map[string]Owner }

func NewRegistry(owners ...Owner) (*Registry, error) {
	r := &Registry{owners: make(map[string]Owner, len(owners))}
	for _, owner := range owners {
		if !ownerName.MatchString(owner.Module) || owner.ClientID != "addp-"+owner.Module ||
			owner.ReceiptPermission != owner.Module+".production_receipt.read" || !executionPermission.MatchString(owner.ExecutionPermission) {
			return nil, errors.New("invalid producer registration")
		}
		if _, exists := r.owners[owner.Module]; exists {
			return nil, errors.New("duplicate producer registration")
		}
		r.owners[owner.Module] = owner
	}
	return r, nil
}

// Resolve requires both the registered owner and its fixed calling service.
func (r *Registry) Resolve(module, clientID string) (Owner, bool) {
	if r == nil {
		return Owner{}, false
	}
	owner, ok := r.owners[module]
	return owner, ok && owner.ClientID == clientID
}
