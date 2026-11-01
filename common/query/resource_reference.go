package query

// ResourceQueryReference is an owner-verified identifier path. Parts preserve
// physical names exactly; rendering belongs to the target query language.
// It carries neither a statement nor authorization or connection information.
type ResourceQueryReference struct {
	Parts []string `json:"parts"`
}
