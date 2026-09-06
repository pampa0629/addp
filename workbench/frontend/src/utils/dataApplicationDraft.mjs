export function normalizedApplicationSnapshot(snapshot) {
  const normalized = JSON.parse(JSON.stringify(snapshot))
  const used = new Set(normalized.parameter_bindings.map((binding) => binding.application_parameter_key))
  normalized.parameters = normalized.parameters.filter((parameter) => used.has(parameter.key))
  return normalized
}

export function buildDataApplicationPreview(application) {
  return {
    name: application.name.trim(),
    description: application.description.trim(),
    revision_number: 0,
    snapshot: normalizedApplicationSnapshot(application.snapshot),
  }
}

export function dataApplicationEditorRouteContext(routeName, applicationID = '') {
  if (routeName === 'DataApplicationCreate') return 'create'
  return `edit:${String(applicationID || '').trim()}`
}

export function dataApplicationEditorMutationContext(routeName, applicationID, action) {
  return `${dataApplicationEditorRouteContext(routeName, applicationID)}:${String(action || '').trim()}`
}

export function dataApplicationListPageContext(page) {
  const normalized = Number(page)
  return `page:${Number.isInteger(normalized) && normalized > 0 ? normalized : 1}`
}

export function dataApplicationDeletionContext(applicationID, version) {
  return `delete:${String(applicationID || '').trim()}:${String(version ?? '').trim()}`
}

export function commitLatestDataApplicationRequest(requests, request, currentContext, commit) {
  if (!requests.isCurrent(request, currentContext)) return false
  commit()
  return true
}

export async function confirmDataApplicationAction(confirm, message) {
  try {
    await confirm(message)
    return true
  } catch (error) {
    if (error === 'cancel' || error === 'close') return false
    throw error
  }
}
