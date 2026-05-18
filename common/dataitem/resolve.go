package dataitem

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/addp/common/contentio"
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
	resolveMultiItems(candidates, result)
	if input.Options.AllowWholeScope {
		resolveWholeScope(candidates, result, input)
		if result.Exclusive {
			return result, nil
		}
	}
	resolveSingleItems(candidates, result, input)
	return result, nil
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
	knownExts := map[string]contentio.RelatedRefSpec{}
	requiredExts := map[string]bool{}
	primaryExt := ""
	for _, spec := range specs {
		ext := contentio.NormalizeExtension(spec.Extension)
		if ext == "" {
			continue
		}
		knownExts[ext] = spec
		if spec.Required {
			requiredExts[ext] = true
		}
		if spec.Primary {
			primaryExt = ext
		}
	}
	if primaryExt == "" {
		for _, spec := range specs {
			ext := contentio.NormalizeExtension(spec.Extension)
			if ext != "" && spec.Required {
				primaryExt = ext
				break
			}
		}
	}
	if primaryExt == "" {
		return nil
	}

	groups := map[string]map[string]Candidate{}
	for _, candidate := range candidates {
		if claims[candidate.Path] {
			continue
		}
		spec, ok := knownExts[candidate.Extension]
		if !ok || spec.Extension == "" {
			continue
		}
		groupKey := multiGroupKey(candidate)
		if groupKey == "" {
			continue
		}
		if _, ok := groups[groupKey]; !ok {
			groups[groupKey] = map[string]Candidate{}
		}
		groups[groupKey][candidate.Extension] = candidate
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
			Name:            entry.Name,
			FullName:        entry.Path,
			Organization:    OrganizationMulti,
			DataType:        rule.DataType,
			Format:          rule.Format,
			EntryPath:       entry.Path,
			RefPaths:        refPaths,
			RefList:         refList,
			SizeBytes:       &size,
			DetectionReason: "multi_refs",
			Properties: map[string]interface{}{
				"base_name": strings.TrimSuffix(entry.Name, filepath.Ext(entry.Name)),
			},
		}
		items = append(items, item)
	}
	return items
}

func resolveWholeScope(candidates []Candidate, result *ResolveResult, input ResolveInput) {
	for _, rule := range BuiltinWholeScopeRules() {
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
	if len(allowedExts) == 0 {
		return ResolvedItem{}, false
	}

	scopePath := strings.Trim(input.ScopePath, "/")
	dataCandidates := []Candidate{}
	auxiliaryCount := 0
	directDataCount := 0
	partLikeDataCount := 0
	partitionLikePath := false
	var total int64

	for _, candidate := range candidates {
		if claims[candidate.Path] {
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
		if rule.WholeScope.RequiresStrongMatch {
			return ResolvedItem{}, false
		}
	}

	if len(dataCandidates) == 0 {
		return ResolvedItem{}, false
	}
	strongHit := partitionLikePath || (directDataCount == len(dataCandidates) && (len(dataCandidates) > 1 && partLikeDataCount == len(dataCandidates) || auxiliaryCount > 0))
	if !strongHit && rule.WholeScope.RequiresStrongMatch {
		return ResolvedItem{}, false
	}

	refs := make([]ItemRef, 0, len(dataCandidates))
	refPaths := map[string]string{}
	for _, candidate := range dataCandidates {
		refs = append(refs, ItemRef{
			Role:      "data",
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
		Organization:    OrganizationWhole,
		DataType:        rule.DataType,
		Format:          rule.Format,
		EntryPath:       scopePath,
		RefPaths:        refPaths,
		RefList:         refs,
		SizeBytes:       &size,
		DetectionReason: "whole_scope",
	}, true
}

func refSpecsFromRule(rule FormatRule) []contentio.RelatedRefSpec {
	if rule.Refs == nil {
		return nil
	}
	specs := make([]contentio.RelatedRefSpec, 0, len(rule.Refs.RequiredExtensions)+len(rule.Refs.OptionalExtensions))
	entryExt := contentio.NormalizeExtension(rule.Refs.EntryExtension)
	for _, ext := range rule.Refs.RequiredExtensions {
		normalized := contentio.NormalizeExtension(ext)
		specs = append(specs, contentio.RelatedRefSpec{
			Extension: normalized,
			Required:  true,
			Primary:   normalized == entryExt,
		})
	}
	for _, ext := range rule.Refs.OptionalExtensions {
		normalized := contentio.NormalizeExtension(ext)
		specs = append(specs, contentio.RelatedRefSpec{
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
		dataType := DetectDataType(formatName)
		if dataType == DataTypeUnknown && candidate.IsDirectory {
			dataType = DataTypeContainer
		}
		size := int64(0)
		var sizePtr *int64
		if candidate.SizeBytes != nil {
			size = *candidate.SizeBytes
			sizePtr = &size
		}
		item := ResolvedItem{
			Name:            candidate.Name,
			FullName:        candidate.Path,
			Organization:    OrganizationSingle,
			DataType:        dataType,
			Format:          formatName,
			EntryPath:       candidate.Path,
			SizeBytes:       sizePtr,
			DetectionReason: "single_resource",
			Properties:      cloneMap(candidate.Properties),
		}
		result.Items = append(result.Items, item)
		result.Claims[candidate.Path] = true
		if input.Options.MaxItems > 0 && len(result.Items) >= input.Options.MaxItems {
			return
		}
	}
}

func multiGroupKey(candidate Candidate) string {
	dir := filepath.Dir(strings.Trim(candidate.Path, "/"))
	if dir == "." {
		dir = ""
	}
	base := candidate.BaseName
	if base == "" {
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
		normalized := contentio.NormalizeExtension(ext)
		if normalized != "" {
			result[normalized] = true
		}
	}
	return result
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

func ContainerChildInfoFromResolvedItem(item ResolvedItem) format.ContainerChildInfo {
	kind := "file"
	if item.Organization == OrganizationMulti {
		kind = "multi"
	}
	properties := cloneMap(item.Properties)
	if properties == nil {
		properties = map[string]interface{}{}
	}
	properties["path"] = item.EntryPath
	properties["format"] = item.Format
	properties["organization"] = string(item.Organization)
	if len(item.RefList) > 0 {
		refs := make([]map[string]interface{}, 0, len(item.RefList))
		refPaths := map[string]interface{}{}
		for _, ref := range item.RefList {
			refs = append(refs, map[string]interface{}{
				"role":      ref.Role,
				"path":      ref.Path,
				"required":  ref.Required,
				"primary":   ref.Primary,
				"extension": ref.Extension,
			})
			refPaths[ref.Role] = ref.Path
		}
		properties["refs"] = refs
		properties["ref_paths"] = refPaths
	}
	return format.ContainerChildInfo{
		Name:         item.Name,
		Kind:         kind,
		DataType:     string(item.DataType),
		Format:       format.FormatType(item.Format),
		Organization: string(item.Organization),
		Refs:         containerChildRefs(item),
		Properties:   properties,
	}
}

func containerChildRefs(item ResolvedItem) []format.ContainerChildRef {
	if len(item.RefList) == 0 {
		return nil
	}
	refs := make([]format.ContainerChildRef, 0, len(item.RefList))
	for _, ref := range item.RefList {
		refs = append(refs, format.ContainerChildRef{
			Role:      ref.Role,
			Path:      ref.Path,
			Required:  ref.Required,
			Primary:   ref.Primary,
			Extension: ref.Extension,
		})
	}
	return refs
}
