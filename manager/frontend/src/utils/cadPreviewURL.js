export function resolveCADTileURL(template, z, x, y, manifestURL, origin) {
  const value = String(template || '')
    .replace('{z}', String(z))
    .replace('{x}', String(x))
    .replace('{y}', String(y))
  const baseOrigin = origin || (typeof window !== 'undefined' ? window.location.origin : '')
  const manifest = String(manifestURL || '')
  const resolvedManifestURL = baseOrigin ? new URL(manifest, baseOrigin) : new URL(manifest)
  return new URL(value, resolvedManifestURL).href
}
