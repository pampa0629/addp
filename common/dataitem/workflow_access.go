package dataitem

import (
	"fmt"

	"github.com/addp/common/engine/workflowaccess"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

// WithTIFFPAM carries only the optional companion registered on this source item.
func WithTIFFPAM(source workflowaccess.Source, locator *resourcetree.ResourceLocator, item *commonModels.MetaItem) (workflowaccess.Source, error) {
	if item == nil || item.EngineID != locator.EngineID || item.FullName != locator.FullName() {
		return workflowaccess.Source{}, fmt.Errorf("TIFF source metadata does not match its locator")
	}
	for _, ref := range DescriptorFromAttributes(item.Attributes).Refs {
		if ref.Role != "auxiliary_metadata" || ref.Path != item.FullName+".aux.xml" {
			continue
		}
		access := source.Access
		if access.Method == workflowaccess.MethodMountedPath {
			access.Path += ".aux.xml"
		} else {
			access.Object += ".aux.xml"
		}
		source.RelatedFiles = []workflowaccess.RelatedFile{{Role: "auxiliary_metadata", Access: access}}
		break
	}
	return source, nil
}
