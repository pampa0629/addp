package api

import (
	"database/sql"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/spatial"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func spatialPreviewContract(geometryColumn string, sourceSRID int, sourceCRS string, sourceCRSDefinition *datatype.CRSDefinition) gin.H {
	contract := gin.H{
		"geometry_column": geometryColumn,
		"source_srid":     sourceSRID,
		"target_srid":     nil,
	}
	if sourceSRID > 0 {
		if sourceCRS == "" {
			sourceCRS = datatype.EPSGCRSRef(sourceSRID)
		}
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
	contract["transform_message"] = "源坐标系未知，已跳过地图渲染"
	return contract
}

func postGISCRSDefinition(db *gorm.DB, sourceSRID int) (string, *datatype.CRSDefinition) {
	if db == nil || sourceSRID <= 0 {
		return "", nil
	}
	var authName sql.NullString
	var authSRID sql.NullInt64
	var srtext sql.NullString
	var proj4text sql.NullString
	err := db.Raw(`
		SELECT auth_name, auth_srid, srtext, proj4text
		FROM spatial_ref_sys
		WHERE srid = ?
		LIMIT 1
	`, sourceSRID).Row().Scan(&authName, &authSRID, &srtext, &proj4text)
	if err != nil {
		return "", nil
	}

	encoding := datatype.CRSDefinitionEncodingWKT
	definitionText := strings.TrimSpace(srtext.String)
	if definitionText == "" {
		encoding = datatype.CRSDefinitionEncodingProj4
		definitionText = strings.TrimSpace(proj4text.String)
	}
	if definitionText == "" {
		return "", nil
	}

	code := 0
	if authSRID.Valid {
		code = int(authSRID.Int64)
	}
	id := datatype.CRSRefFromAuthority(authName.String, code, definitionText)
	if id == "" {
		return "", nil
	}
	return id, &datatype.CRSDefinition{
		ID:                 id,
		DefinitionEncoding: encoding,
		Definition:         definitionText,
		Source:             datatype.CRSDefinitionSourcePostGISSpatialRefSys,
	}
}
