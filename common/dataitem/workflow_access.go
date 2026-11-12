package dataitem

import (
	"fmt"
	"path"

	"github.com/addp/common/engine/workflowaccess"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

// WithTIFFComponents carries every companion registered on this source item.
func WithTIFFComponents(source workflowaccess.Source, locator *resourcetree.ResourceLocator, item *commonModels.MetaItem) (workflowaccess.Source, error) {
	if item == nil || item.EngineID != locator.EngineID || item.FullName != locator.FullName() {
		return workflowaccess.Source{}, fmt.Errorf("TIFF source metadata does not match its locator")
	}
	source.RelatedFiles = nil
	for _, ref := range DescriptorFromAttributes(item.Attributes).Refs {
		if ref.Primary {
			continue
		}
		if path.Dir(ref.Path) != path.Dir(item.FullName) {
			return workflowaccess.Source{}, fmt.Errorf("TIFF component must share its primary directory")
		}
		access := source.Access
		if access.Method == workflowaccess.MethodMountedPath {
			access.Path = path.Join(path.Dir(access.Path), path.Base(ref.Path))
		} else {
			access.Object = path.Join(path.Dir(access.Object), path.Base(ref.Path))
		}
		source.RelatedFiles = append(source.RelatedFiles, workflowaccess.RelatedFile{Role: ref.Role, Access: access})
	}
	return source, nil
}
