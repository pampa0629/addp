export function tenantUserBrowserPreferenceKey(authContext, preference, item) {
  const { principal, context } = authContext || {}
  if (principal?.type !== 'user' || context?.type !== 'tenant' ||
      !/^[1-9]\d*$/.test(principal.id) || !/^[1-9]\d*$/.test(context.tenant_id)) return ''
  const scope = `addp:${preference}:v1:${context.tenant_id}:${principal.id}`
  return item === undefined ? scope : `${scope}:${encodeURIComponent(item)}`
}

export function readBrowserPreference(key) {
  if (!key) return null
  try { return JSON.parse(globalThis.localStorage.getItem(key)) }
  catch { return null }
}

export function writeBrowserPreference(key, value) {
  if (!key) return
  try { globalThis.localStorage.setItem(key, JSON.stringify(value)) }
  catch { /* Storage is optional. */ }
}
