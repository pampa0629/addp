package engineaccess

import (
	"strings"
	"unicode"
	"unicode/utf8"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
)

// SourceGrantFilter narrows the management list, not the effective-access verdict.
// Recipient type and ID match a direct Grant, without expanding organization members.
type SourceGrantFilter struct {
	TableSearch   string
	CatalogPath   *engineplugin.EngineCatalogPath
	RecipientType string
	RecipientID   int64
	State         string
}

func (f SourceGrantFilter) Validate() error {
	switch f.State {
	case "", "active", "revoked", "expired":
	default:
		return commonapi.ErrBadRequest
	}
	if f.CatalogPath != nil {
		encoded, err := authorization.EncodeSharingTarget(*f.CatalogPath)
		if err != nil || len(encoded) > 4096 || f.TableSearch != "" {
			return commonapi.ErrBadRequest
		}
	}
	if f.RecipientID < 0 || (f.RecipientID != 0 && f.RecipientType == "") || !utf8.ValidString(f.TableSearch) || utf8.RuneCountInString(f.TableSearch) > 200 || strings.TrimSpace(f.TableSearch) != f.TableSearch {
		return commonapi.ErrBadRequest
	}
	switch f.RecipientType {
	case "", "user", "department", "project_group":
	default:
		return commonapi.ErrBadRequest
	}
	for _, character := range f.TableSearch {
		if unicode.IsControl(character) {
			return commonapi.ErrBadRequest
		}
	}
	return nil
}

func (f SourceGrantFilter) ValidateForEngine(engineID int64) error {
	if err := f.Validate(); err != nil {
		return err
	}
	if f.CatalogPath != nil && int64(f.CatalogPath.EngineID) != engineID {
		return commonapi.ErrBadRequest
	}
	return nil
}

// Both current relations and history use identical parameter-bound predicates.
// strpos keeps SQL wildcard characters literal, including % and _ in table names.
func (f SourceGrantFilter) predicate() (string, []any) {
	target, args := f.targetPredicate()
	clauses := []string{target}
	if f.RecipientType != "" {
		clauses = append(clauses, "recipient_type = ?")
		args = append(args, f.RecipientType)
	}
	if f.RecipientID != 0 {
		clauses = append(clauses, "recipient_id = ?")
		args = append(args, f.RecipientID)
	}
	return strings.Join(clauses, " AND "), args
}

// Apply the same target filter before account-source expansion and pagination.
func (f SourceGrantFilter) targetPredicate() (string, []any) {
	clauses := []string{"TRUE"}
	args := make([]any, 0, 1)
	if f.CatalogPath != nil {
		encoded, _ := authorization.EncodeSharingTarget(*f.CatalogPath) // validated at service boundary
		clauses = append(clauses, "catalog_path = ?::jsonb")
		args = append(args, string(encoded))
	}
	if f.TableSearch != "" {
		clauses = append(clauses, `strpos(lower(array_to_string(ARRAY(
 SELECT segment->>'name' FROM jsonb_array_elements(catalog_path->'segments') WITH ORDINALITY AS parts(segment, position)
 ORDER BY position), ' / ')), lower(?)) > 0`)
		args = append(args, f.TableSearch)
	}
	return strings.Join(clauses, " AND "), args
}
