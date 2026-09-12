export const TARGET_OVERRIDE_POLICY = 'existing_table_append'

export function targetOverrideEligible({ boundary, loadMode, representation, existingTarget, applyMode }) {
  return boundary === 'bounded' &&
    loadMode === 'snapshot' &&
    representation === 'native' &&
    existingTarget === true &&
    applyMode === 'append'
}

export function withTargetOverride(endpoint, enabled) {
  if (!enabled) return endpoint
  return { ...endpoint, override_policy: TARGET_OVERRIDE_POLICY }
}

export function targetOverrideAfterParentSelection({ parentChanged, existingTarget, overrideEnabled }) {
  if (!parentChanged) return { existingTarget, overrideEnabled }
  return { existingTarget: false, overrideEnabled: false }
}
