export function buildTilesetSource(url, origin = window.location.origin) {
  const parsed = parseStorageStreamURL(url, origin)
  if (!parsed) {
    return { rootURL: withAuthToken(url, origin), engineID: '', storageRef: '', virtual: false }
  }
  return {
    rootURL: virtualTileURL(parsed.storageRef, origin),
    engineID: parsed.engineID,
    storageRef: parsed.storageRef,
    virtual: true
  }
}

export function parseStorageStreamURL(url, origin = window.location.origin) {
  if (!url || typeof url !== 'string') return null
  let parsed
  try {
    parsed = new URL(url, origin)
  } catch {
    return null
  }
  if (!parsed.pathname.endsWith('/api/v1/manager/storage-stream')) return null
  const engineID = parsed.searchParams.get('engine_id') || ''
  const storageRef = parsed.searchParams.get('storage_ref') || ''
  if (!engineID || !storageRef) return null
  return { engineID, storageRef }
}

export function virtualTileURL(storageRef, origin = window.location.origin) {
  const encoded = String(storageRef || '')
    .split('/')
    .filter(Boolean)
    .map((part) => encodeURIComponent(part))
    .join('/')
  return `${origin}/__addp_3dtiles__/${encoded}`
}

export function resolveTileResourceURL(resourceURL, source, origin = window.location.origin) {
  if (!source?.virtual) return withAuthToken(resourceURL, origin)
  let parsed
  try {
    parsed = new URL(resourceURL, origin)
  } catch {
    return resourceURL
  }
  const prefix = '/__addp_3dtiles__/'
  if (!parsed.pathname.startsWith(prefix)) return withAuthToken(resourceURL, origin)
  const encodedPath = parsed.pathname.slice(prefix.length)
  const storageRef = encodedPath
    .split('/')
    .filter(Boolean)
    .map((part) => decodeURIComponent(part))
    .join('/')
  const params = new URLSearchParams()
  params.set('engine_id', source.engineID)
  params.set('storage_ref', storageRef || source.storageRef)
  appendAuthToken(params)
  return `/api/v1/manager/storage-stream?${params.toString()}`
}

export function withAuthToken(url, origin = window.location.origin) {
  if (!url || typeof url !== 'string') return ''
  if (!url.startsWith('/api/') && !url.startsWith('/manager/')) return url
  const parsed = new URL(url, origin)
  appendAuthToken(parsed.searchParams)
  return `${parsed.pathname}?${parsed.searchParams.toString()}`
}

function appendAuthToken(params) {
  const storage = globalThis.localStorage
  const token = storage?.getItem?.('token')
  if (token && !params.has('token')) {
    params.set('token', token)
  }
}
