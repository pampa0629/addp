package dataitem

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func ResolveItems(input ResolveInput) (*ResolveResult, error) {
	result := &ResolveResult{
		Items:  []ResolvedItem{},
		Claims: map[string]bool{},
	}
	candidates, ignored := normalizeAndFilterCandidates(input)
	if input.Options.IncludeIgnored {
		result.Ignored = ignored
	}
	if input.Options.AllowWholeScope {
		resolveWholeScope(candidates, result, input, true)
		if result.Exclusive {
			return result, nil
		}
	}
	resolveMultiItems(candidates, result)
	if input.Options.AllowWholeScope {
		resolveWholeScope(candidates, result, input, false)
		if result.Exclusive {
			return result, nil
		}
	}
	resolveSingleItems(candidates, result, input)
	return result, nil
}

func ScanTargetsFromAttributes(attrs map[string]interface{}) []ScanTarget {
	if len(attrs) == 0 {
		return nil
	}
	itemAttrs := asMap(attrs["item"])
	storageAttrs := asMap(attrs["storage"])
	switch strings.ToLower(strings.TrimSpace(asString(itemAttrs["layout"]))) {
	case string(format.LayoutMulti):
		if target := normalizeTargetPath(asString(storageAttrs["physical_path"])); target != "" {
			return []ScanTarget{{Path: target}}
		}
		return nil
	case string(format.LayoutWhole):
		if target := normalizeTargetPath(asString(storageAttrs["physical_path"])); target != "" {
			return []ScanTarget{{Path: target}}
		}
		if target := normalizeTargetPath(asString(storageAttrs["path"])); target != "" {
			return []ScanTarget{{Path: target}}
		}
	case string(format.LayoutSingle):
		if target := normalizeTargetPath(asString(storageAttrs["physical_path"])); target != "" {
			return []ScanTarget{{Path: target}}
		}
		if target := normalizeTargetPath(asString(storageAttrs["path"])); target != "" {
			return []ScanTarget{{Path: target}}
		}
	}
	return nil
}

func DescriptorFromAttributes(attrs map[string]interface{}) ItemDescriptor {
	if len(attrs) == 0 {
		return ItemDescriptor{}
	}
	itemAttrs := asMap(attrs["item"])
	storageAttrs := asMap(attrs["storage"])
	layout := format.Layout(strings.ToLower(strings.TrimSpace(asString(itemAttrs["layout"]))))
	physicalPath := normalizeTargetPath(asString(storageAttrs["physical_path"]))
	storagePath := normalizeStoragePath(asString(storageAttrs["path"]))
	primaryContentPath := ""
	scopePath := ""
	switch layout {
	case format.LayoutWhole:
		scopePath = firstNonEmpty(physicalPath, storagePath)
	case format.LayoutSingle, format.LayoutMulti:
		primaryContentPath = physicalPath
	}
	return ItemDescriptor{
		Layout:             layout,
		DataType:           datatype.DataType(strings.ToLower(strings.TrimSpace(asString(itemAttrs["data_type"])))),
		Format:             string(format.NormalizeFormat(asString(itemAttrs["format"]))),
		PrimaryContentPath: primaryContentPath,
		ScopePath:          scopePath,
		PhysicalPath:       physicalPath,
		StoragePath:        storagePath,
		StorageName:        strings.Trim(strings.TrimSpace(asString(storageAttrs["name"])), "/"),
		StorageBucket:      strings.Trim(strings.TrimSpace(asString(storageAttrs["bucket"])), "/"),
		Refs:               refsFromAttributes(itemAttrs["refs"]),
		SizeBytes:          sizeBytesFromAttributes(storageAttrs),
	}
}

func normalizeAndFilterCandidates(input ResolveInput) ([]Candidate, []IgnoredCandidate) {
	policy := input.Options.IgnorePolicy
	if policy == nil {
		policy = DefaultIgnorePolicy{}
	}
	candidates := make([]Candidate, 0, len(input.Candidates))
	ignored := []IgnoredCandidate{}
	for _, candidate := range input.Candidates {
		candidate = NormalizeCandidate(candidate)
		if ignore, reason := policy.Ignore(candidate); ignore {
			ignored = append(ignored, IgnoredCandidate{Candidate: candidate, Reason: reason})
			continue
		}
		candidates = append(candidates, candidate)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Path < candidates[j].Path
	})
	return candidates, ignored
}

func refsFromAttributes(raw interface{}) []ItemRef {
	items := asSlice(raw)
	if len(items) == 0 {
		return nil
	}
	refs := make([]ItemRef, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		rawRef := asMap(item)
		path := normalizeTargetPath(asString(rawRef["path"]))
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		refs = append(refs, ItemRef{
			Role:      strings.TrimSpace(asString(rawRef["role"])),
			Path:      path,
			Required:  asBool(rawRef["required"]),
			Primary:   asBool(rawRef["primary"]),
			Extension: strings.ToLower(strings.TrimSpace(asString(rawRef["extension"]))),
		})
	}
	return refs
}

func sizeBytesFromAttributes(storageAttrs map[string]interface{}) *int64 {
	for _, key := range []string{"total_size", "size"} {
		if value, ok := asInt64(storageAttrs[key]); ok && value > 0 {
			return &value
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func resolveMultiItems(candidates []Candidate, result *ResolveResult) {
	for _, rule := range BuiltinMultiRules() {
		for _, item := range matchMultiRule(candidates, rule, result.Claims) {
			result.Items = append(result.Items, item)
			for _, ref := range item.RefList {
				result.Claims[ref.Path] = true
			}
		}
	}
}

func matchMultiRule(candidates []Candidate, rule FormatRule, claims map[string]bool) []ResolvedItem {
	specs := rule.RelatedRefSpecs
	if len(specs) == 0 {
		specs = refSpecsFromRule(rule)
	}
	if len(specs) == 0 {
		return nil
	}
	if err := format.ValidateRelatedRefSpecs(specs); err != nil {
		return nil
	}
	specs = expandPrimaryRelatedRefSpecs(specs, rule.Entry.Extensions)
	knownExts := map[string]format.RelatedRefSpec{}
	requiredExts := map[string]bool{}
	primaryExts := []string{}
	for _, spec := range specs {
		ext := format.NormalizeExtension(spec.Extension)
		if ext == "" {
			continue
		}
		knownExts[ext] = spec
		if spec.Required && !spec.Primary {
			requiredExts[ext] = true
		}
		if spec.Primary {
			primaryExts = append(primaryExts, ext)
		}
	}
	if len(primaryExts) == 0 {
		for _, spec := range specs {
			ext := format.NormalizeExtension(spec.Extension)
			if ext != "" && spec.Required {
				primaryExts = append(primaryExts, ext)
				break
			}
		}
	}
	if len(primaryExts) == 0 {
		return nil
	}

	groups := map[string]map[string]Candidate{}
	for _, candidate := range candidates {
		if claims[candidate.Path] {
			continue
		}
		matchedExt, spec, ok := matchRelatedRefSpec(candidate, knownExts)
		if !ok || spec.Extension == "" {
			continue
		}
		groupKey := multiGroupKey(candidate, matchedExt, primaryExts)
		if groupKey == "" {
			continue
		}
		if _, ok := groups[groupKey]; !ok {
			groups[groupKey] = map[string]Candidate{}
		}
		groups[groupKey][matchedExt] = candidate
	}

	groupKeys := make([]string, 0, len(groups))
	for key := range groups {
		groupKeys = append(groupKeys, key)
	}
	sort.Strings(groupKeys)

	items := []ResolvedItem{}
	for _, key := range groupKeys {
		group := groups[key]
		complete := true
		for ext := range requiredExts {
			if _, ok := group[ext]; !ok {
				complete = false
				break
			}
		}
		if !complete {
			continue
		}
		primaryExt := firstGroupPrimaryExt(group, primaryExts)
		if primaryExt == "" {
			continue
		}
		entry, ok := group[primaryExt]
		if !ok {
			continue
		}
		refList := make([]ItemRef, 0, len(group))
		refPaths := map[string]string{}
		var total int64
		exts := make([]string, 0, len(group))
		for ext := range group {
			exts = append(exts, ext)
		}
		sort.Strings(exts)
		for _, ext := range exts {
			candidate := group[ext]
			spec := knownExts[ext]
			role := strings.TrimSpace(spec.Role)
			if role == "" {
				role = strings.TrimPrefix(ext, ".")
			}
			if candidate.SizeBytes != nil {
				total += *candidate.SizeBytes
			}
			refPaths[role] = candidate.Path
			refList = append(refList, ItemRef{
				Role:      role,
				Path:      candidate.Path,
				Required:  spec.Required,
				Primary:   spec.Primary || ext == primaryExt,
				Extension: ext,
			})
		}
		size := total
		item := ResolvedItem{
			Name:               entry.Name,
			FullName:           entry.Path,
			Layout:             format.LayoutMulti,
			DataType:           rule.DataType,
			Format:             rule.Format,
			PrimaryContentPath: entry.Path,
			RefPaths:           refPaths,
			RefList:            refList,
			SizeBytes:          &size,
			DetectionReason:    "multi_refs",
			Properties: map[string]interface{}{
				"base_name": strings.TrimSuffix(entry.Name, filepath.Ext(entry.Name)),
			},
		}
		items = append(items, item)
	}
	return items
}

func expandPrimaryRelatedRefSpecs(specs []format.RelatedRefSpec, entryExtensions []string) []format.RelatedRefSpec {
	if len(specs) == 0 || len(entryExtensions) == 0 {
		return specs
	}
	primarySpec := format.RelatedRefSpec{}
	primaryExt := ""
	known := map[string]bool{}
	for _, spec := range specs {
		ext := format.NormalizeExtension(spec.Extension)
		if ext != "" {
			known[ext] = true
		}
		if spec.Primary {
			primarySpec = spec
			primaryExt = ext
		}
	}
	if primaryExt == "" {
		return specs
	}
	expanded := append([]format.RelatedRefSpec(nil), specs...)
	for _, ext := range entryExtensions {
		normalized := format.NormalizeExtension(ext)
		if normalized == "" || known[normalized] || normalized == primaryExt {
			continue
		}
		variant := primarySpec
		variant.Extension = normalized
		expanded = append(expanded, variant)
		known[normalized] = true
	}
	return expanded
}

func matchRelatedRefSpec(candidate Candidate, specs map[string]format.RelatedRefSpec) (string, format.RelatedRefSpec, bool) {
	exts := make([]string, 0, len(specs))
	for ext := range specs {
		exts = append(exts, ext)
	}
	sort.Slice(exts, func(i, j int) bool {
		return len(exts[i]) > len(exts[j])
	})
	name := strings.ToLower(strings.TrimSpace(candidate.Name))
	path := strings.ToLower(strings.TrimSpace(candidate.Path))
	for _, ext := range exts {
		if strings.HasSuffix(name, ext) || strings.HasSuffix(path, ext) {
			return ext, specs[ext], true
		}
	}
	return "", format.RelatedRefSpec{}, false
}

func firstGroupPrimaryExt(group map[string]Candidate, primaryExts []string) string {
	for _, ext := range primaryExts {
		if _, ok := group[ext]; ok {
			return ext
		}
	}
	return ""
}

func asMap(value interface{}) map[string]interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return v
	case map[string]string:
		result := map[string]interface{}{}
		for key, item := range v {
			result[key] = item
		}
		return result
	default:
		return map[string]interface{}{}
	}
}

func asSlice(value interface{}) []interface{} {
	switch v := value.(type) {
	case []interface{}:
		return v
	case []map[string]interface{}:
		result := make([]interface{}, 0, len(v))
		for _, item := range v {
			result = append(result, item)
		}
		return result
	case []map[string]string:
		result := make([]interface{}, 0, len(v))
		for _, item := range v {
			mapped := map[string]interface{}{}
			for key, val := range item {
				mapped[key] = val
			}
			result = append(result, mapped)
		}
		return result
	default:
		return nil
	}
}

func asBool(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

func asInt64(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case int32:
		return int64(v), true
	case uint:
		return int64(v), true
	case uint64:
		if v > uint64(^uint64(0)>>1) {
			return 0, false
		}
		return int64(v), true
	case float64:
		return int64(v), true
	case float32:
		return int64(v), true
	default:
		return 0, false
	}
}

func normalizeTargetPath(value string) string {
	return strings.Trim(strings.TrimSpace(value), "/")
}

func normalizeStoragePath(value string) string {
	hadTrailingSlash := strings.HasSuffix(strings.TrimSpace(value), "/")
	value = normalizeTargetPath(value)
	if value != "" && hadTrailingSlash {
		return value + "/"
	}
	return value
}

func asString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return ""
	}
}

func resolveWholeScope(candidates []Candidate, result *ResolveResult, input ResolveInput, claimAllOnly bool) {
	for _, rule := range BuiltinWholeScopeRules() {
		if rule.WholeScope == nil || rule.WholeScope.ClaimAllOnStrongHit != claimAllOnly {
			continue
		}
		item, ok := matchWholeScopeRule(candidates, rule, result.Claims, input)
		if !ok {
			continue
		}
		result.Items = append(result.Items, item)
		for _, ref := range item.RefList {
			result.Claims[ref.Path] = true
		}
		if rule.WholeScope != nil && rule.WholeScope.ExclusiveOnStrongHit {
			result.Exclusive = true
		}
		return
	}
}

func matchWholeScopeRule(candidates []Candidate, rule FormatRule, claims map[string]bool, input ResolveInput) (ResolvedItem, bool) {
	if rule.WholeScope == nil {
		return ResolvedItem{}, false
	}
	allowedExts := ruleExtensionSet(rule.Entry.Extensions)
	requiredNames := ruleFileNameSet(rule.WholeScope.RequiredFileNames)
	if len(allowedExts) == 0 && len(requiredNames) == 0 {
		return ResolvedItem{}, false
	}

	scopePath := strings.Trim(input.ScopePath, "/")
	dataCandidates := []Candidate{}
	requiredCandidates := []Candidate{}
	unmatchedCandidates := []Candidate{}
	auxiliaryCount := 0
	directDataCount := 0
	partLikeDataCount := 0
	partitionLikePath := false
	var total int64

	for _, candidate := range candidates {
		if claims[candidate.Path] {
			continue
		}
		candidateName := strings.ToLower(strings.TrimSpace(filepath.Base(candidate.Name)))
		if candidateName == "" {
			candidateName = strings.ToLower(strings.TrimSpace(filepath.Base(candidate.Path)))
		}
		if requiredNames[candidateName] {
			requiredCandidates = append(requiredCandidates, candidate)
			if candidate.SizeBytes != nil {
				total += *candidate.SizeBytes
			}
			continue
		}
		if allowedExts[candidate.Extension] {
			dataCandidates = append(dataCandidates, candidate)
			if candidate.SizeBytes != nil {
				total += *candidate.SizeBytes
			}
			if isDirectChildOfScope(scopePath, candidate.Path) {
				directDataCount++
			}
			if isPartLikeWholeScopeEntry(candidate.Name) {
				partLikeDataCount++
			}
			if hasPartitionLikePath(scopePath, candidate.Path) {
				partitionLikePath = true
			}
			continue
		}
		if isWholeScopeAuxiliaryCandidate(candidate, rule.WholeScope) {
			auxiliaryCount++
			continue
		}
		unmatchedCandidates = append(unmatchedCandidates, candidate)
		if rule.WholeScope.RequiresStrongMatch {
			if len(requiredNames) == 0 || !rule.WholeScope.ClaimAllOnStrongHit {
				return ResolvedItem{}, false
			}
		}
	}

	if len(dataCandidates) == 0 && len(requiredCandidates) == 0 {
		return ResolvedItem{}, false
	}
	requiredStrongHit := len(requiredNames) > 0 && requiredFileNamesSatisfied(requiredNames, requiredCandidates, scopePath)
	strongHit := requiredStrongHit || partitionLikePath || (directDataCount == len(dataCandidates) && (len(dataCandidates) > 1 && partLikeDataCount == len(dataCandidates) || auxiliaryCount > 0))
	if !strongHit && rule.WholeScope.RequiresStrongMatch {
		return ResolvedItem{}, false
	}

	if requiredStrongHit && rule.WholeScope.ClaimAllOnStrongHit {
		for _, candidate := range unmatchedCandidates {
			if candidate.SizeBytes != nil {
				total += *candidate.SizeBytes
			}
			dataCandidates = append(dataCandidates, candidate)
		}
	}

	refs := make([]ItemRef, 0, len(requiredCandidates)+len(dataCandidates))
	refPaths := map[string]string{}
	for _, candidate := range requiredCandidates {
		refs = append(refs, ItemRef{
			Role:      "manifest",
			Path:      candidate.Path,
			Required:  true,
			Primary:   len(refs) == 0,
			Extension: candidate.Extension,
		})
		refPaths[candidate.Path] = candidate.Path
	}
	for _, candidate := range dataCandidates {
		refs = append(refs, ItemRef{
			Role:      wholeScopeDataRole(candidate, requiredNames),
			Path:      candidate.Path,
			Required:  true,
			Primary:   len(refs) == 0,
			Extension: candidate.Extension,
		})
		refPaths[candidate.Path] = candidate.Path
	}

	size := total
	name := filepath.Base(scopePath)
	if name == "." || name == "" {
		name = scopePath
	}
	return ResolvedItem{
		Name:            name,
		FullName:        scopePath,
		Layout:          format.LayoutWhole,
		DataType:        rule.DataType,
		Format:          rule.Format,
		ScopePath:       scopePath,
		RefPaths:        refPaths,
		RefList:         refs,
		SizeBytes:       &size,
		DetectionReason: "whole_scope",
	}, true
}

func refSpecsFromRule(rule FormatRule) []format.RelatedRefSpec {
	if rule.Refs == nil {
		return nil
	}
	specs := make([]format.RelatedRefSpec, 0, len(rule.Refs.RequiredExtensions)+len(rule.Refs.OptionalExtensions))
	entryExt := format.NormalizeExtension(rule.Refs.EntryExtension)
	for _, ext := range rule.Refs.RequiredExtensions {
		normalized := format.NormalizeExtension(ext)
		specs = append(specs, format.RelatedRefSpec{
			Extension: normalized,
			Required:  true,
			Primary:   normalized == entryExt,
		})
	}
	for _, ext := range rule.Refs.OptionalExtensions {
		normalized := format.NormalizeExtension(ext)
		specs = append(specs, format.RelatedRefSpec{
			Extension: normalized,
			Required:  false,
			Primary:   normalized == entryExt,
		})
	}
	return specs
}

func resolveSingleItems(candidates []Candidate, result *ResolveResult, input ResolveInput) {
	for _, candidate := range candidates {
		if result.Claims[candidate.Path] {
			continue
		}
		formatName := DetectFormat(candidate)
		dataType := DefaultDataTypeForFormat(formatName)
		if dataType == datatype.Unknown && candidate.IsDirectory {
			dataType = datatype.Container
		}
		size := int64(0)
		var sizePtr *int64
		if candidate.SizeBytes != nil {
			size = *candidate.SizeBytes
			sizePtr = &size
		}
		item := ResolvedItem{
			Name:               candidate.Name,
			FullName:           candidate.Path,
			Layout:             format.LayoutSingle,
			DataType:           dataType,
			Format:             formatName,
			PrimaryContentPath: candidate.Path,
			SizeBytes:          sizePtr,
			DetectionReason:    "single_resource",
			Properties:         cloneMap(candidate.Properties),
		}
		result.Items = append(result.Items, item)
		result.Claims[candidate.Path] = true
		if input.Options.MaxItems > 0 && len(result.Items) >= input.Options.MaxItems {
			return
		}
	}
}

func multiGroupKey(candidate Candidate, matchedExt string, primaryExts []string) string {
	dir := filepath.Dir(strings.Trim(candidate.Path, "/"))
	if dir == "." {
		dir = ""
	}
	base := candidate.BaseName
	if matchedExt != "" {
		base = strings.TrimSuffix(candidate.Name, matchedExt)
		if base == candidate.Name {
			base = strings.TrimSuffix(candidate.Name, filepath.Ext(candidate.Name))
		}
		for _, primaryExt := range primaryExts {
			if strings.HasSuffix(strings.ToLower(base), primaryExt) {
				base = strings.TrimSuffix(base, base[len(base)-len(primaryExt):])
				break
			}
		}
	} else if base == "" {
		base = strings.TrimSuffix(candidate.Name, filepath.Ext(candidate.Name))
	}
	if base == "" {
		return ""
	}
	return dir + "\x00" + base
}

func ruleExtensionSet(extensions []string) map[string]bool {
	result := map[string]bool{}
	for _, ext := range extensions {
		normalized := format.NormalizeExtension(ext)
		if normalized != "" {
			result[normalized] = true
		}
	}
	return result
}

func ruleFileNameSet(fileNames []string) map[string]bool {
	result := map[string]bool{}
	for _, fileName := range fileNames {
		normalized := strings.ToLower(strings.TrimSpace(filepath.Base(fileName)))
		if normalized != "" {
			result[normalized] = true
		}
	}
	return result
}

func requiredFileNamesSatisfied(required map[string]bool, candidates []Candidate, scopePath string) bool {
	if len(required) == 0 {
		return false
	}
	found := map[string]bool{}
	for _, candidate := range candidates {
		name := strings.ToLower(strings.TrimSpace(filepath.Base(candidate.Name)))
		if name == "" {
			name = strings.ToLower(strings.TrimSpace(filepath.Base(candidate.Path)))
		}
		if required[name] && isDirectChildOfScope(scopePath, candidate.Path) {
			found[name] = true
		}
	}
	return len(found) == len(required)
}

func wholeScopeDataRole(candidate Candidate, requiredNames map[string]bool) string {
	name := strings.ToLower(strings.TrimSpace(filepath.Base(candidate.Name)))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(filepath.Base(candidate.Path)))
	}
	if requiredNames[name] {
		return "manifest"
	}
	return "data"
}

func isWholeScopeAuxiliaryCandidate(candidate Candidate, rule *WholeScopeRule) bool {
	name := strings.ToLower(strings.TrimSpace(filepath.Base(candidate.Name)))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(filepath.Base(candidate.Path)))
	}
	for _, allowed := range rule.IgnoredFileNames {
		if name == strings.ToLower(strings.TrimSpace(allowed)) {
			return true
		}
	}
	if strings.HasPrefix(name, ".") && strings.Contains(name, ".crc") {
		return true
	}
	return strings.HasSuffix(name, ".crc")
}

func isDirectChildOfScope(scopePath, candidatePath string) bool {
	parent := strings.Trim(filepath.ToSlash(filepath.Dir(strings.Trim(candidatePath, "/"))), "/")
	if parent == "." {
		parent = ""
	}
	return parent == scopePath
}

func hasPartitionLikePath(scopePath, candidatePath string) bool {
	trimmed := strings.Trim(candidatePath, "/")
	if scopePath != "" {
		prefix := scopePath + "/"
		if !strings.HasPrefix(trimmed, prefix) {
			return false
		}
		trimmed = strings.TrimPrefix(trimmed, prefix)
	}
	parts := strings.Split(trimmed, "/")
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		name := strings.TrimSpace(part)
		if strings.Contains(name, "=") || strings.HasPrefix(name, "_") {
			return true
		}
	}
	return false
}

func isPartLikeWholeScopeEntry(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(filepath.Base(name)))
	return strings.HasPrefix(normalized, "part-") || strings.HasPrefix(normalized, "part_")
}

func cloneMap(input map[string]interface{}) map[string]interface{} {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]interface{}, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func ContainerChildInfoFromResolvedItem(item ResolvedItem) datatype.ContainerChildInfo {
	kind := "file"
	if item.Layout == format.LayoutMulti {
		kind = "multi"
	}
	native := cloneMap(item.Properties)
	if native == nil {
		native = map[string]interface{}{}
	}
	delete(native, "refs")
	delete(native, "ref_paths")
	native["path"] = item.PrimaryContentPath
	return datatype.ContainerChildInfo{
		Name:      item.Name,
		ChildKind: kind,
		DataType:  item.DataType,
		Format:    item.Format,
		Refs:      containerChildRefs(item),
		Native:    native,
	}
}

func containerChildRefs(item ResolvedItem) []datatype.ContainerChildRef {
	if len(item.RefList) == 0 {
		return nil
	}
	refs := make([]datatype.ContainerChildRef, 0, len(item.RefList))
	for _, ref := range item.RefList {
		refs = append(refs, datatype.ContainerChildRef{
			Role:      ref.Role,
			Path:      ref.Path,
			Required:  ref.Required,
			Primary:   ref.Primary,
			Extension: ref.Extension,
		})
	}
	return refs
}
