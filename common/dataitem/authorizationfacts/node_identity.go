package authorizationfacts

import (
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/engine/plugin"
)

const MaxNodeIdentityBytes = 16 << 10

// NodeIdentityQuery identifies one owner node. It carries no ACL or source connection.
type NodeIdentityQuery struct {
	Target plugin.EngineCatalogPath `json:"target"`
}

// ValidNodeIdentityTarget admits the first supported node profile: PG root/schema.
// Names remain opaque; a dot or slash inside a quoted schema is not a hierarchy.
func ValidNodeIdentityTarget(path plugin.EngineCatalogPath) bool {
	if path.EngineID == 0 || uint64(path.EngineID) > 1<<32-1 || len(path.Segments) < 1 || len(path.Segments) > 2 {
		return false
	}
	model := plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema)
	want := plugin.EngineCatalogRootPath(model, path.EngineID)
	if len(path.Segments) == 2 {
		name := path.Segments[1].Name
		if name == "" || !utf8.ValidString(name) || len(name) > 512 || strings.ContainsRune(name, 0) {
			return false
		}
		want = plugin.TabularNamespacePath(path.EngineID, plugin.EngineCatalogTermSchema, name)
	}
	return reflect.DeepEqual(path, want)
}

func (q NodeIdentityQuery) Validate() error {
	if !ValidNodeIdentityTarget(q.Target) {
		return errors.New("invalid node identity target")
	}
	return nil
}

// NodeIdentity is a committed existence observation, never an access decision.
type NodeIdentity struct {
	TenantID uint                     `json:"tenant_id"`
	Target   plugin.EngineCatalogPath `json:"target"`
	Exists   bool                     `json:"exists"`
}

func (f NodeIdentity) Validate(tenantID uint, query NodeIdentityQuery) error {
	if tenantID == 0 || f.TenantID != tenantID || query.Validate() != nil || !reflect.DeepEqual(f.Target, query.Target) {
		return errors.New("invalid node identity response")
	}
	return nil
}
