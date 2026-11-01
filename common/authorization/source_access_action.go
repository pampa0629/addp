package authorization

// Source access actions are resource rules, not function Permissions. Content
// reading includes metadata only while that content Grant is currently usable.
const (
	SourceAccessRead         = "read"
	SourceAccessMetadataRead = "metadata_read"
)

func ValidSourceAccessAction(action string) bool {
	return action == SourceAccessRead || action == SourceAccessMetadataRead
}
