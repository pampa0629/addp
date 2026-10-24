package service

import (
	"fmt"
	"io/fs"
	"path"
	"strings"

	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
)

var ErrInvalidDownloadBundle = fmt.Errorf("%w: invalid bundle composition", ErrDownloadNotSupported)

// Validate before DescriptorFromAttributes normalizes or deduplicates paths.
func validateDownloadItemRefPaths(item *commonModels.MetaItem) error {
	if item == nil {
		return nil
	}
	raw := commonJSON.Section(item.Attributes, "item")["refs"]
	if raw == nil {
		return nil
	}
	refs := commonJSON.InterfaceSlice(raw)
	if refs == nil {
		return ErrInvalidDownloadBundle
	}
	seen := map[string]bool{}
	for _, value := range refs {
		ref, ok := value.(map[string]interface{})
		if !ok {
			return ErrInvalidDownloadBundle
		}
		name, ok := ref["path"].(string)
		if !ok || !validDownloadBundlePath(name) || seen[name] {
			return ErrInvalidDownloadBundle
		}
		seen[name] = true
	}
	return nil
}

func validDownloadBundlePath(name string) bool {
	if name == "." || !fs.ValidPath(name) || strings.ContainsAny(name, "\\\x00") {
		return false
	}
	return !(len(name) >= 2 && name[1] == ':' && ((name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= 'a' && name[0] <= 'z')))
}

// The full confirmed list determines the root, including unavailable optional
// members. Opening a subset must not change any remaining relative reference.
func downloadBundleNames(refs []models.DownloadRef) ([]string, error) {
	if len(refs) == 0 {
		return nil, ErrInvalidDownloadBundle
	}
	var root []string
	seen := map[string]bool{}
	for index, ref := range refs {
		if !validDownloadBundlePath(ref.StorageRef) || seen[ref.StorageRef] {
			return nil, ErrInvalidDownloadBundle
		}
		seen[ref.StorageRef] = true
		parent := path.Dir(ref.StorageRef)
		var parts []string
		if parent != "." {
			parts = strings.Split(parent, "/")
		}
		if index == 0 {
			root = parts
			continue
		}
		length := 0
		for length < len(root) && length < len(parts) && root[length] == parts[length] {
			length++
		}
		root = root[:length]
	}
	prefix := ""
	if len(root) != 0 {
		prefix = strings.Join(root, "/") + "/"
	}
	names := make([]string, len(refs))
	members := map[string]bool{}
	for index, ref := range refs {
		name := strings.TrimPrefix(ref.StorageRef, prefix)
		if !validDownloadBundlePath(name) || members[name] {
			return nil, ErrInvalidDownloadBundle
		}
		members[name] = true
		names[index] = name
	}
	for name := range members {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if members[parent] {
				return nil, ErrInvalidDownloadBundle
			}
		}
	}
	return names, nil
}
