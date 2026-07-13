package metaattr

import (
	"github.com/addp/common/datatype"
	"github.com/addp/meta/internal/models"
)

func CADInfoAttributes(cadInfo *datatype.CADInfo) models.JSONMap {
	attrs := models.JSONMap{}
	if payload := datatype.CADInfoPayload(cadInfo); len(payload) > 0 {
		UpsertNested(attrs, "type_info", "cad", payload)
	}
	return attrs
}
