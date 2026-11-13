package skp

// Local XML/resource facts only: no XSD validator, resolver, Provider or
// complete-composition receipt. Unresolved dependencies and opaque bytes are
// explicit and can never be promoted to admission by this layer.
import (
	"context"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

const (
	vffClassificationNS = "http://www.sketchup.com/schemas/sketchup/1.0/classification"
	vffDocumentNS       = "http://www.sketchup.com/schemas/1.0/documentproperties"
	vffReferencesNS     = "http://www.sketchup.com/schemas/1.0/references"
	vffXSDNS            = "http://www.w3.org/2001/XMLSchema"
	vffXSINS            = "http://www.w3.org/2001/XMLSchema-instance"
)

type vffClassificationDocumentLimits struct {
	packages     vffClassificationPackageLimits
	xml          vffXMLLimits
	images       vffImageLimits
	filters      vffSKCFilterLimits
	dependencies int
}

type vffSKCMemberFact struct {
	member string
	sha256 [32]byte
}

type vffSKCSchemaHint struct{ namespace, location string }
type vffSKCXMLFact struct {
	vffSKCMemberFact
	hints []vffSKCSchemaHint
}

type vffSKCDependency struct {
	kind, location, namespace string
	namespaceDeclared         bool
	begin, end                int
	// Empty member means unresolved. No filesystem/network lookup is attempted.
	member string
	sha256 [32]byte
}

type vffSKCOpaqueXML struct {
	begin, end int
	sha256     [32]byte
}

type vffSKCSchemaFact struct {
	vffSKCMemberFact
	targetNamespace   string
	namespaceDeclared bool
	dependencies      []vffSKCDependency
	opaque            []vffSKCOpaqueXML
	resources         []vffSKCResourceDeclaration
}

type vffSKCResourceDeclaration struct {
	kind, attribute, value string
	begin, end             int
	sha256                 [32]byte
}

type vffSKCDocumentFact struct {
	vffSKCMemberFact // Outer SKC identity.
	documents        []vffSKCXMLFact
	rootSchema       string
	title            string
	thumbnail        *vffImageFact
	schemas          []vffSKCSchemaFact
	filters          []vffSKCFilterFact
	unexplained      []vffSKCMemberFact
	links            []vffSKCSchemaLink
}

type vffClassificationDocumentFacts struct {
	packages  *vffClassificationPackageFacts
	documents []vffSKCDocumentFact
}

func validateVFFClassificationDocumentLimits(limits vffClassificationDocumentLimits) error {
	if limits.xml.bytes <= 0 || limits.xml.nodes <= 0 || limits.xml.depth <= 0 || limits.xml.attributes <= 0 || limits.xml.text <= 0 || limits.dependencies <= 0 || limits.images.images <= 0 || limits.images.bytes <= 0 || limits.images.pixels == 0 {
		return fmt.Errorf("SKC documents require explicit cumulative XML, dependency and image budgets")
	}
	if limits.filters.bytes <= 0 || limits.filters.tokens <= 0 || limits.filters.rules <= 0 || limits.filters.attributes <= 0 {
		return fmt.Errorf("SKC documents require explicit cumulative filter budgets")
	}
	return nil
}

func readVFFClassificationDocuments(ctx context.Context, c *vffContainer, limits vffClassificationDocumentLimits) (*vffClassificationDocumentFacts, error) {
	if err := validateVFFClassificationDocumentLimits(limits); err != nil {
		return nil, err
	}
	packages, err := readVFFClassificationPackages(ctx, c, limits.packages)
	if err != nil {
		return nil, err
	}
	result := &vffClassificationDocumentFacts{packages: packages}
	budget := &vffXMLBudget{limits: limits.xml}
	filterBudget := &vffSKCFilterBudget{limits: limits.filters}
	remainingImages, remainingDependencies := limits.images, limits.dependencies
	for _, item := range packages.packages {
		fact, err := readVFFSKCDocument(ctx, item, budget, filterBudget, &remainingImages, &remainingDependencies)
		if err != nil {
			return nil, err
		}
		result.documents = append(result.documents, *fact)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func readVFFSKCDocument(ctx context.Context, item vffClassificationPackage, budget *vffXMLBudget, filterBudget *vffSKCFilterBudget, images *vffImageLimits, dependencies *int) (*vffSKCDocumentFact, error) {
	result := &vffSKCDocumentFact{vffSKCMemberFact: vffSKCMemberFact{item.member, item.sha256}}
	claimed := map[string]bool{}
	names := []string{"document.xml", "documentProperties.xml", "references.xml"}
	namespaces := []string{vffClassificationNS, vffDocumentNS, vffReferencesNS}
	roots := make([]*vffXMLNode, 3)
	for i, name := range names {
		raw, ok := item.files[name]
		if !ok {
			return nil, fmt.Errorf("missing exact SKC document")
		}
		qualified := map[xml.Name]bool{{Space: vffXSINS, Local: "schemaLocation"}: true}
		if i == 1 {
			qualified[xml.Name{Space: vffDocumentNS, Local: "name"}] = true
			qualified[xml.Name{Space: vffDocumentNS, Local: "version"}] = true
		}
		node, err := readVFFXML(ctx, raw, budget, map[string]bool{namespaces[i]: true}, qualified)
		if err != nil {
			return nil, err
		}
		hints, err := vffSKCHints(node)
		if err != nil {
			return nil, err
		}
		roots[i] = node
		claimed[name] = true
		result.documents = append(result.documents, vffSKCXMLFact{vffSKCMemberFact{name, sha256.Sum256(raw)}, hints})
	}
	root := roots[0]
	if root.name != (xml.Name{Space: vffClassificationNS, Local: "classificationDocument"}) || len(root.children) != 1 {
		return nil, fmt.Errorf("unsupported SKC classification document")
	}
	if err := vffXMLShape(root, "classificationDocument", vffXMLAttributeKey(vffXSINS, "schemaLocation"), "Classification", false); err != nil {
		return nil, err
	}
	declaration := root.children[0]
	if err := vffXMLShape(declaration, "Classification", "xsdFile", "", false); err != nil {
		return nil, err
	}
	result.rootSchema = declaration.attrs["xsdFile"]
	if err := vffSKCExactMember(item.files, result.rootSchema, ".xsd"); err != nil {
		return nil, err
	}
	title, thumbnail, err := vffSKCProperties(roots[1])
	if err != nil {
		return nil, err
	}
	result.title = title
	// Properties require the element; only its exact empty text means no image.
	if thumbnail != "" {
		if err := vffSKCExactMember(item.files, thumbnail, ".png .jpg .jpeg"); err != nil {
			return nil, err
		}
		// Decode through the existing one image implementation. Each exact SKC is
		// visited once; different package identities consume aggregate budgets.
		imageFacts, err := readVFFImages(ctx, &vffContainer{members: item.files}, []vffImageReference{{thumbnail, sha256.Sum256(item.files[thumbnail])}}, *images)
		if err != nil {
			return nil, err
		}
		result.thumbnail = &imageFacts[0]
		images.images--
		images.bytes -= len(item.files[thumbnail])
		images.pixels -= uint64(result.thumbnail.width) * uint64(result.thumbnail.height)
		claimed[thumbnail] = true
	}
	if roots[2].name != (xml.Name{Space: vffReferencesNS, Local: "references"}) {
		return nil, fmt.Errorf("unsupported SKC references root")
	}
	if err := vffXMLShape(roots[2], "references", vffXMLAttributeKey(vffXSINS, "schemaLocation"), "", false); err != nil {
		return nil, err
	}
	queue := []string{result.rootSchema}
	for i := 0; i < len(queue); i++ {
		name := queue[i]
		if claimed[name] {
			continue
		}
		claimed[name] = true
		schema, err := readVFFSKCSchema(ctx, name, item.files, budget, dependencies)
		if err != nil {
			return nil, err
		}
		result.schemas = append(result.schemas, *schema)
		for _, dependency := range schema.dependencies {
			if dependency.member != "" && !claimed[dependency.member] {
				queue = append(queue, dependency.member)
			}
		}
		filter := name + ".filter"
		if raw, ok := item.files[filter]; ok {
			fact, err := readVFFSKCFilter(ctx, filter, raw, filterBudget)
			if err != nil {
				return nil, err
			}
			claimed[filter] = true
			result.filters = append(result.filters, *fact)
		}
	}
	links, err := bindVFFSKCSchemaLinks(ctx, item, result.schemas)
	if err != nil {
		return nil, err
	}
	result.links = links
	var unclaimed []string
	for name := range item.files {
		if !claimed[name] {
			unclaimed = append(unclaimed, name)
		}
	}
	sort.Strings(unclaimed)
	for _, name := range unclaimed {
		result.unexplained = append(result.unexplained, vffSKCMemberFact{name, sha256.Sum256(item.files[name])})
	}
	return result, nil
}

func vffSKCHints(root *vffXMLNode) ([]vffSKCSchemaHint, error) {
	value, exists := root.attrs[vffXMLAttributeKey(vffXSINS, "schemaLocation")]
	if !exists {
		return nil, nil
	}
	parts := strings.Fields(value)
	if len(parts) == 0 || len(parts)%2 != 0 {
		return nil, fmt.Errorf("invalid SKC schema location hint pairs")
	}
	var result []vffSKCSchemaHint
	seen := map[string]bool{}
	for i := 0; i < len(parts); i += 2 {
		if seen[parts[i]] {
			return nil, fmt.Errorf("ambiguous SKC schema location hints")
		}
		seen[parts[i]] = true
		result = append(result, vffSKCSchemaHint{parts[i], parts[i+1]})
	}
	return result, nil
}

func vffSKCExactMember(files map[string][]byte, member, extensions string) error {
	if err := vffMemberName([]byte(member), 0x800); err != nil {
		return err
	}
	allowed := false
	for _, extension := range strings.Fields(extensions) {
		allowed = allowed || strings.ToLower(path.Ext(member)) == extension
	}
	if !allowed || len(files[member]) == 0 {
		return fmt.Errorf("missing or unsupported exact SKC resource member")
	}
	return nil
}

func vffSKCProperties(root *vffXMLNode) (string, string, error) {
	fields := "title description creator keywords lastModifiedBy revision created modified thumbnail generator"
	if root.name != (xml.Name{Space: vffDocumentNS, Local: "documentProperties"}) {
		return "", "", fmt.Errorf("unsupported SKC properties root")
	}
	if err := vffXMLShape(root, "documentProperties", vffXMLAttributeKey(vffXSINS, "schemaLocation"), fields, false); err != nil {
		return "", "", err
	}
	values := map[string]string{}
	for _, child := range root.children {
		if child.name.Local == "generator" {
			name, version := vffXMLAttributeKey(vffDocumentNS, "name"), vffXMLAttributeKey(vffDocumentNS, "version")
			if err := vffXMLShape(child, "generator", name+" "+version, "", false); err != nil {
				return "", "", err
			}
			if strings.TrimSpace(child.attrs[name]) == "" || strings.TrimSpace(child.attrs[version]) == "" {
				return "", "", fmt.Errorf("incomplete SKC generator")
			}
		} else if err := vffXMLShape(child, child.name.Local, "", "", true); err != nil {
			return "", "", err
		}
		values[child.name.Local] = child.text
	}
	for _, field := range strings.Fields(fields) {
		if _, ok := values[field]; !ok {
			return "", "", fmt.Errorf("missing SKC property")
		}
	}
	if strings.TrimSpace(values["revision"]) == "" {
		return "", "", fmt.Errorf("empty SKC revision")
	}
	for _, field := range []string{"created", "modified"} {
		parsed, err := time.Parse("2006-01-02T15:04:05", values[field])
		if err != nil || parsed.Format("2006-01-02T15:04:05") != values[field] {
			return "", "", fmt.Errorf("unsupported SKC timestamp")
		}
	}
	return values["title"], values["thumbnail"], nil
}

func readVFFSKCSchema(ctx context.Context, member string, files map[string][]byte, budget *vffXMLBudget, dependencies *int) (*vffSKCSchemaFact, error) {
	raw := files[member]
	root, err := readVFFXML(ctx, raw, budget, map[string]bool{vffXSDNS: true}, nil)
	if err != nil {
		return nil, err
	}
	if root.name != (xml.Name{Space: vffXSDNS, Local: "schema"}) || strings.TrimSpace(root.text) != "" {
		return nil, fmt.Errorf("unsupported SKC schema root")
	}
	for key := range root.attrs {
		if !strings.Contains(" targetNamespace elementFormDefault attributeFormDefault version id blockDefault finalDefault ", " "+key+" ") {
			return nil, fmt.Errorf("unsupported SKC schema root attribute")
		}
	}
	result := &vffSKCSchemaFact{vffSKCMemberFact: vffSKCMemberFact{member, sha256.Sum256(raw)}, targetNamespace: root.attrs["targetNamespace"]}
	_, result.namespaceDeclared = root.attrs["targetNamespace"]
	resources, err := vffSKCSchemaResources(ctx, root, raw, dependencies)
	if err != nil {
		return nil, err
	}
	result.resources = resources
	for _, child := range root.children {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		kind := child.name.Local
		if kind != "include" && kind != "import" && kind != "redefine" {
			result.opaque = append(result.opaque, vffSKCOpaqueXML{child.begin, child.end, sha256.Sum256(raw[child.begin:child.end])})
			continue
		}
		if *dependencies <= 0 {
			return nil, fmt.Errorf("SKC cumulative dependency count exceeded")
		}
		*dependencies -= 1
		allowed := " schemaLocation id "
		if kind == "import" {
			allowed += "namespace "
		}
		for key := range child.attrs {
			if !strings.Contains(allowed, " "+key+" ") {
				return nil, fmt.Errorf("unsupported SKC dependency attribute")
			}
		}
		if strings.TrimSpace(child.text) != "" {
			return nil, fmt.Errorf("mixed SKC dependency content")
		}
		// Nested annotation/redefinition semantics are explicitly opaque. A
		// dependency inside another opaque subtree is not silently discovered.
		for _, nested := range child.children {
			result.opaque = append(result.opaque, vffSKCOpaqueXML{nested.begin, nested.end, sha256.Sum256(raw[nested.begin:nested.end])})
		}
		location := child.attrs["schemaLocation"]
		if kind != "import" && location == "" {
			return nil, fmt.Errorf("missing required SKC dependency location")
		}
		dependency := vffSKCDependency{kind: kind, location: location, namespace: child.attrs["namespace"], begin: child.begin, end: child.end}
		_, dependency.namespaceDeclared = child.attrs["namespace"]
		// Relative locations and the byte-pinned IFC2X3 import both resolve
		// within this SKC. Other locations remain unresolved.
		if location != "" && vffMemberName([]byte(location), 0x800) == nil && !strings.ContainsAny(location, "%?# \t\r\n") {
			target := path.Join(path.Dir(member), location)
			if err := vffSKCExactMember(files, target, ".xsd"); err != nil {
				return nil, err
			}
			dependency.member, dependency.sha256 = target, sha256.Sum256(files[target])
		} else {
			bindVFFSKCIFC2X3Import(result.vffSKCMemberFact, files, &dependency)
		}
		result.dependencies = append(result.dependencies, dependency)
	}
	return result, nil
}

// buildingSMART Standards-builds IFC2x3 TC1 XML at 9e87befeccde8e876b0f06325779617dedd44aa5.
func bindVFFSKCIFC2X3Import(source vffSKCMemberFact, files map[string][]byte, dependency *vffSKCDependency) {
	const sourceDigest = "e67112920fa6750827d370a2bc42d8dc2d40f18776f91a87e8629947ba051533"
	const targetDigest = "c9a726b71f870f6426ad396e0e91737ef5d5354e9dde23994298a8bcaa2e418f"
	const target = "Schemas/ex.xsd"
	if dependency.kind != "import" || !dependency.namespaceDeclared ||
		dependency.location != "http://www.iai-tech.org/ifcXML/IFC2x3/FINAL/ex.xsd" ||
		dependency.namespace != "urn:iso.org:standard:10303:part(28):version(2):xmlschema:common" ||
		source.member != "Schemas/IFC2X3.xsd" || fmt.Sprintf("%x", source.sha256) != sourceDigest {
		return
	}
	raw, exists := files[target]
	if !exists {
		return
	}
	digest := sha256.Sum256(raw)
	if fmt.Sprintf("%x", digest) == targetDigest {
		dependency.member, dependency.sha256 = target, digest
	}
}
