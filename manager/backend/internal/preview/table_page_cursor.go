package preview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/opaquetoken"
	commonquery "github.com/addp/common/query"
)

var ErrTablePageCursor = plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, errors.New("invalid table page cursor"))

var ErrTablePageKeyUnavailable = errors.New("table page cursor key is not configured")

type TablePageRequest struct {
	Codec   *opaquetoken.Codec
	Cursor  string
	Subject dataprotection.SubjectReference
	Rules   []dataprotection.Rule
}

type tableSeek struct {
	request *TablePageRequest
	keys    []string
	after   []string
	purpose string
	page    int
}

type tablePosition struct {
	Page  int
	After []string
}

func (r *TablePageRequest) readable(keys []string) bool {
	now := time.Now().UTC()
	for _, rule := range r.Rules {
		if len(rule.Component.Path) == 0 || rule.Action != "preview" {
			continue
		}
		for _, key := range keys {
			if rule.Component.Path[0].Name == key && rule.EffectiveDecision(r.Subject, now).Effect != dataprotection.EffectAllow {
				return false
			}
		}
	}
	return true
}

// Use constraint order, including composite keys declared in a different
// order from the physical fields. Nullable uniqueness cannot order every row.
func (r *TablePageRequest) resolve(req *PreviewRequest, facts *plugin.EngineCatalogFacts, page, size int) (*tableSeek, error) {
	var keys, types []string
	if facts != nil && facts.Table != nil && facts.Table.Native["relkind"] == "r" && facts.Table.Native["relhassubclass"] == false {
		fields := make(map[string]datatype.FieldInfo, len(facts.Table.Fields))
		for _, field := range facts.Table.Fields {
			fields[field.Name] = field
		}
		for _, constraint := range facts.Constraints {
			if constraint.ConstraintType != plugin.ConstraintTypePrimaryKey && constraint.ConstraintType != plugin.ConstraintTypeUnique {
				continue
			}
			if len(constraint.Fields) == 0 || !r.readable(constraint.Fields) {
				continue
			}
			var native []string
			for _, key := range constraint.Fields {
				field := fields[key]
				if field.Nullable || field.Type != datatype.FieldTypeInt && field.Type != datatype.FieldTypeBigInt && field.Type != datatype.FieldTypeString && field.Type != datatype.FieldTypeUUID {
					break
				}
				native = append(native, field.NativeType)
			}
			if len(native) == len(constraint.Fields) && (keys == nil || constraint.ConstraintType == plugin.ConstraintTypePrimaryKey) {
				keys, types = constraint.Fields, native
				if constraint.ConstraintType == plugin.ConstraintTypePrimaryKey {
					break
				}
			}
		}
	}
	if len(keys) == 0 {
		if r.Cursor != "" {
			return nil, ErrTablePageCursor
		}
		return nil, nil
	}
	if r.Codec == nil {
		return nil, ErrTablePageKeyUnavailable
	}
	binding, err := json.Marshal([]any{r.Subject, req.TenantID, req.ItemFingerprint, req.Engine.ID, req.Engine.Version, req.ProviderPath, req.DataScope, keys, types, size})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(binding)
	seek := &tableSeek{request: r, keys: keys, purpose: hex.EncodeToString(sum[:]), page: page}
	if r.Cursor == "" {
		if page != 1 {
			return nil, ErrTablePageCursor
		}
		return seek, nil
	}
	var pos tablePosition
	if page <= 1 || r.Codec.Decode(seek.purpose, r.Cursor, &pos) != nil || pos.Page != page || len(pos.After) != len(keys) {
		return nil, ErrTablePageCursor
	}
	seek.after = pos.After
	return seek, nil
}

func (s *tableSeek) predicate(dialect commonquery.Dialect, where string, args []interface{}) (string, []interface{}) {
	if len(s.after) == 0 {
		return where, args
	}
	var params []string
	for _, value := range s.after {
		args = append(args, value)
		params = append(params, dialect.Placeholder(len(args)))
	}
	seek := "(" + databasePreviewOrderByClause(dialect, "", s.keys) + ") > (" + strings.Join(params, ", ") + ")"
	if where != "" {
		seek = "(" + where + ") AND " + seek
	}
	return seek, args
}

// Called at the HTTP response boundary on the last delivered, protected row.
func (s *tableSeek) next(row map[string]interface{}) (string, error) {
	if !s.request.readable(s.keys) {
		return "", ErrTablePageCursor
	}
	pos := tablePosition{Page: s.page + 1}
	for _, key := range s.keys {
		var value string
		switch v := row[key].(type) {
		case string:
			value = v
		case []byte:
			value = string(v)
		case int64:
			value = strconv.FormatInt(v, 10)
		case int32:
			value = strconv.FormatInt(int64(v), 10)
		case int16:
			value = strconv.FormatInt(int64(v), 10)
		case int:
			value = strconv.Itoa(v)
		default:
			return "", fmt.Errorf("table cursor key has unsupported representation %T", v)
		}
		pos.After = append(pos.After, value)
	}
	return s.request.Codec.Encode(s.purpose, pos)
}
