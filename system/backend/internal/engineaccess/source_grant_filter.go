package engineaccess

import (
	"strings"
	"unicode"
	"unicode/utf8"

	commonapi "github.com/addp/common/api"
)

// SourceGrantFilter narrows the management list, not the effective-access verdict.
// AccountID matches a personal Grant only; organization Grants remain independent.
type SourceGrantFilter struct {
	TableSearch string
	AccountID   int64
}

func (f SourceGrantFilter) Validate() error {
	if f.AccountID < 0 || !utf8.ValidString(f.TableSearch) || utf8.RuneCountInString(f.TableSearch) > 200 || strings.TrimSpace(f.TableSearch) != f.TableSearch {
		return commonapi.ErrBadRequest
	}
	for _, character := range f.TableSearch {
		if unicode.IsControl(character) {
			return commonapi.ErrBadRequest
		}
	}
	return nil
}

// Both current relations and history use identical parameter-bound predicates.
// strpos keeps SQL wildcard characters literal, including % and _ in table names.
func (f SourceGrantFilter) predicate() (string, []any) {
	clauses := []string{"TRUE"}
	args := make([]any, 0, 2)
	if f.TableSearch != "" {
		clauses = append(clauses, `strpos(lower(array_to_string(ARRAY(
 SELECT segment->>'name' FROM jsonb_array_elements(catalog_path->'segments') WITH ORDINALITY AS parts(segment, position)
 ORDER BY position), ' / ')), lower(?)) > 0`)
		args = append(args, f.TableSearch)
	}
	if f.AccountID != 0 {
		clauses = append(clauses, "recipient_type = 'user' AND recipient_id = ?")
		args = append(args, f.AccountID)
	}
	return strings.Join(clauses, " AND "), args
}
