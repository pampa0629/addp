package dataitem

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/addp/common/format"
	"github.com/addp/common/resource"
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
			for _, component := range item.ComponentList {
				result.Claims[component.Path] = true
			}
		}
	}
}

func matchMultiRule(candidates []Candidate, rule FormatRule, claims map[string]bool) []ResolvedItem {
	specs := rule.ComponentSpecs
	if len(specs) == 0 {
		specs = componentSpecsFromRule(rule)
	}
	if len(specs) == 0 {
		return nil
	}
	knownExts := map[string]resource.ComponentSpec{}
	requiredExts := map[string]bool{}
	primaryExt := ""
	for _, spec := range specs {
		ext := resource.NormalizeExtension(spec.Extension)
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
			ext := resource.NormalizeExtension(spec.Extension)
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
		componentList := make([]ComponentRef, 0, len(group))
		componentPaths := map[string]string{}
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
			componentPaths[role] = candidate.Path
			componentList = append(componentList, ComponentRef{
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
			ComponentPaths:  componentPaths,
			ComponentList:   componentList,
			SizeBytes:       &size,
			DetectionReason: "multi_components",
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
		for _, component := range item.ComponentList {
			result.Claims[component.Path] = true
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

	components := make([]ComponentRef, 0, len(dataCandidates))
	componentPaths := map[string]string{}
	for _, candidate := range dataCandidates {
		components = append(components, ComponentRef{
			Role:      "data",
			Path:      candidate.Path,
			Required:  true,
			Primary:   len(components) == 0,
			Extension: candidate.Extension,
		})
		componentPaths[candidate.Path] = candidate.Path
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
		ComponentPaths:  componentPaths,
		ComponentList:   components,
		SizeBytes:       &size,
		DetectionReason: "whole_scope",
	}, true
}

func componentSpecsFromRule(rule FormatRule) []resource.ComponentSpec {
	if rule.Components == nil {
		return nil
	}
	specs := make([]resource.ComponentSpec, 0, len(rule.Components.RequiredExtensions)+len(rule.Components.OptionalExtensions))
	entryExt := resource.NormalizeExtension(rule.Components.EntryExtension)
	for _, ext := range rule.Components.RequiredExtensions {
		normalized := resource.NormalizeExtension(ext)
		specs = append(specs, resource.ComponentSpec{
			Extension: normalized,
			Required:  true,
			Primary:   normalized == entryExt,
		})
	}
	for _, ext := range rule.Components.OptionalExtensions {
		normalized := resource.NormalizeExtension(ext)
		specs = append(specs, resource.ComponentSpec{
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
		normalized := resource.NormalizeExtension(ext)
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
	if len(item.ComponentList) > 0 {
		components := make([]map[string]interface{}, 0, len(item.ComponentList))
		componentPaths := map[string]interface{}{}
		for _, component := range item.ComponentList {
			components = append(components, map[string]interface{}{
				"role":      component.Role,
				"path":      component.Path,
				"required":  component.Required,
				"primary":   component.Primary,
				"extension": component.Extension,
			})
			componentPaths[component.Role] = component.Path
		}
		properties["components"] = components
		properties["component_paths"] = componentPaths
	}
	return format.ContainerChildInfo{
		Name:         item.Name,
		Kind:         kind,
		DataType:     string(item.DataType),
		Format:       format.FormatType(item.Format),
		Organization: string(item.Organization),
		Components:   containerChildComponents(item),
		Properties:   properties,
	}
}

func containerChildComponents(item ResolvedItem) []format.ContainerChildComponent {
	if len(item.ComponentList) == 0 {
		return nil
	}
	components := make([]format.ContainerChildComponent, 0, len(item.ComponentList))
	for _, component := range item.ComponentList {
		components = append(components, format.ContainerChildComponent{
			Role:      component.Role,
			Path:      component.Path,
			Required:  component.Required,
			Primary:   component.Primary,
			Extension: component.Extension,
		})
	}
	return components
}
