package api

import (
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/spatial"
	"github.com/gin-gonic/gin"
)

func spatialPreviewContract(geometryColumn string, sourceSRID int, sourceCRS string, sourceCRSDefinition *datatype.CRSDefinition) gin.H {
	sourceCRS = strings.TrimSpace(sourceCRS)
	contract := gin.H{
		"geometry_column": geometryColumn,
		"source_srid":     sourceSRID,
	}
	if sourceCRS == "" && sourceSRID > 0 {
		sourceCRS = datatype.EPSGCRSRef(sourceSRID)
	}
	if sourceCRS != "" {
		contract["source_crs"] = sourceCRS
		if sourceCRSDefinition != nil {
			contract["source_crs_definition"] = sourceCRSDefinition
		}
		contract["transform_status"] = "not_transformed"
		if sourceSRID == spatial.SRIDWGS84 {
			contract["preview_hint"] = "direct_renderable"
		} else {
			contract["preview_hint"] = "frontend_transform_required"
		}
		return contract
	}

	contract["transform_status"] = "unknown_crs"
	contract["preview_hint"] = "unknown_crs"
	return contract
}
