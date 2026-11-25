package service

import (
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// Uploads and task results share one logical ownership test. Being absent as a
// primary item is insufficient when another item owns the member or whole root.
func logicalItemAlreadyKnown(tx *gorm.DB, tenant uint, target plugin.EngineCatalogPath) (string, bool, error) {
	fingerprint, err := authorizationfacts.PrimaryCandidateFingerprint(target)
	if err != nil {
		return "", false, err
	}
	query := tx.Model(&models.MetaItem{}).Where("tenant_id=? AND engine_id=?", tenant, target.EngineID)
	if target.Segments[0].Term == plugin.FileCatalogModel().RootTerm {
		path := target.StringPath()
		switch tx.Dialector.Name() {
		case "postgres":
			query = query.Where(`fingerprint=? OR (attributes->'item'->>'layout'='whole' AND left(?,length(full_name)+1)=full_name||'/') OR EXISTS(SELECT 1 FROM jsonb_array_elements(COALESCE(attributes->'item'->'refs','[]'::jsonb)) ref WHERE ref->>'path'=?)`, fingerprint, path, path)
		case "mysql":
			query = query.Where(`fingerprint=? OR (JSON_UNQUOTE(JSON_EXTRACT(attributes,'$.item.layout'))='whole'
				AND LEFT(?,CHAR_LENGTH(full_name)+1)=CONCAT(full_name,'/'))
				OR JSON_CONTAINS(attributes,JSON_OBJECT('path',?),'$.item.refs')`, fingerprint, path, path)
		default:
			query = query.Where(`fingerprint=? OR (json_extract(attributes,'$.item.layout')='whole' AND substr(?,1,length(full_name)+1)=full_name||'/') OR EXISTS(SELECT 1 FROM json_each(COALESCE(json_extract(attributes,'$.item.refs'),'[]')) ref WHERE json_extract(ref.value,'$.path')=?)`, fingerprint, path, path)
		}
	} else {
		query = query.Where("fingerprint=?", fingerprint)
	}
	var known int64
	err = query.Count(&known).Error
	return fingerprint, known > 0, err
}
