package authorizationfacts

import (
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/engine/plugin"
)

const MaxNodeIdentityBytes = 16 << 10
const MaxNodeIdentityPageBytes = 128 << 10

// NodeIdentityQuery identifies one owner node. It carries no ACL or source connection.
type NodeIdentityQuery struct {
	Target plugin.EngineCatalogPath `json:"target"`
}

// ValidNodeIdentityTarget admits PG, NFS and MinIO navigation identities.
// Names remain opaque; a dot or slash inside a quoted schema is not a hierarchy.
func ValidNodeIdentityTarget(path plugin.EngineCatalogPath) bool {
	if path.EngineID == 0 || uint64(path.EngineID) > 1<<63-1 || len(path.Segments) < 1 || len(path.Segments) > 64 {
		return false
	}
	if path.Segments[0].Term == plugin.EngineCatalogTermService {
		if path.Segments[0] != (plugin.EngineCatalogSegment{Term: "service", Kind: "service", Name: ""}) {
			return false
		}
		for i, part := range path.Segments[1:] {
			term := plugin.EngineCatalogTermPrefix
			kind := plugin.EngineCatalogKindPrefix
			if i == 0 {
				term = plugin.EngineCatalogTermBucket
				kind = plugin.EngineCatalogKindBucket
			}
			if part.Term != term || part.Kind != kind || part.Name == "" || len(part.Name) > 4096 || !utf8.ValidString(part.Name) || strings.ContainsAny(part.Name, "/\\\x00") || part.Name == "." || part.Name == ".." {
				return false
			}
		}
		if len(path.Segments) == 1 {
			return reflect.DeepEqual(path, plugin.ObjectRootPath(path.EngineID))
		}
		names := []string{}
		for _, part := range path.Segments[2:] {
			names = append(names, part.Name)
		}
		return reflect.DeepEqual(path, plugin.ObjectDirectoryPath(path.EngineID, path.Segments[1].Name, strings.Join(names, "/")))
	}
	if path.Segments[0].Term == plugin.EngineCatalogTermRoot {
		for i, part := range path.Segments {
			if i == 0 {
				if part != (plugin.EngineCatalogSegment{Term: "root", Kind: "root", Name: ""}) {
					return false
				}
				continue
			}
			if part.Term != "directory" || part.Kind != "directory" || part.Name == "" || len(part.Name) > 4096 || !utf8.ValidString(part.Name) || strings.ContainsAny(part.Name, "/\\\x00") || part.Name == "." || part.Name == ".." {
				return false
			}
		}
		return reflect.DeepEqual(path, plugin.FileDirectoryPath(path.EngineID, path.StringPath()))
	}
	if len(path.Segments) > 2 {
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

// NodeIdentityPageQuery lists only immediate owner nodes, never child items.
type NodeIdentityPageQuery struct {
	Target   plugin.EngineCatalogPath `json:"target"`
	Page     int                      `json:"page"`
	PageSize int                      `json:"page_size"`
}

func (q NodeIdentityPageQuery) Validate() error {
	if !ValidNodeIdentityTarget(q.Target) || q.Page < 1 || q.PageSize < 1 || q.PageSize > 100 || q.Page > int(^uint(0)>>1)/q.PageSize {
		return errors.New("invalid node identity page query")
	}
	return nil
}

type NodeIdentityPage struct {
	Parent   NodeIdentity   `json:"parent"`
	Nodes    []NodeIdentity `json:"nodes"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

func (p NodeIdentityPage) Validate(tenantID uint, q NodeIdentityPageQuery) error {
	if q.Validate() != nil || p.Parent.Validate(tenantID, NodeIdentityQuery{Target: q.Target}) != nil || p.Page != q.Page || p.PageSize != q.PageSize || p.Total < 0 || p.Nodes == nil {
		return errors.New("invalid node identity page")
	}
	remaining := p.Total - (q.Page-1)*q.PageSize
	if remaining < 0 {
		remaining = 0
	}
	if remaining > q.PageSize {
		remaining = q.PageSize
	}
	if len(p.Nodes) != remaining || (!p.Parent.Exists && p.Total != 0) || (q.Target.Segments[0].Term == "server" && len(q.Target.Segments) == 2 && p.Total != 0) {
		return errors.New("invalid node identity page size")
	}
	seen := make(map[string]bool, len(p.Nodes))
	for _, node := range p.Nodes {
		if !node.Exists || node.TenantID != tenantID || !ValidNodeIdentityTarget(node.Target) || node.Target.EngineID != q.Target.EngineID || len(node.Target.Segments) != len(q.Target.Segments)+1 {
			return errors.New("invalid child node identity")
		}
		for i, segment := range q.Target.Segments {
			if node.Target.Segments[i] != segment {
				return errors.New("invalid child node scope")
			}
		}
		name := node.Target.Segments[len(node.Target.Segments)-1].Name
		if seen[name] {
			return errors.New("duplicate child node identity")
		}
		seen[name] = true
	}
	return nil
}

// NodeIdentityMatchesEngine keeps admission aligned across discovery and signing.
func NodeIdentityMatchesEngine(engineType string, path plugin.EngineCatalogPath) bool {
	if !ValidNodeIdentityTarget(path) {
		return false
	}
	term := path.Segments[0].Term
	return (engineType == "postgresql" && term == "server") || (engineType == "nfs" && term == "root") || (engineType == "minio" && term == "service")
}
