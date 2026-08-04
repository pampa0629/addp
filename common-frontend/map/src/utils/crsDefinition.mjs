const WKT_PARAMETER_NAMES = new Map([
  ['latitude of natural origin', 'latitude_of_origin'],
  ['longitude of natural origin', 'central_meridian'],
  ['scale factor at natural origin', 'scale_factor'],
  ['false easting', 'false_easting'],
  ['false northing', 'false_northing']
])

export function normalizeCRSDefinition(definition, encoding) {
  const normalizedEncoding = String(encoding || '').trim().toLowerCase()
  let normalized = String(definition || '').trim()
  if (!normalized || !['wkt', 'esri_wkt'].includes(normalizedEncoding)) return normalized

  // MySQL WKT nests AUTHORITY inside projection parameters. proj4 then parses
  // numeric values as objects and produces non-finite transformed coordinates.
  normalized = normalized.replace(/,AUTHORITY\["[^"]+","[^"]+"\]/gi, '')
  normalized = normalized.replace(/PROJECTION\[\s*"(?:Gauss_Kruger|Transverse Mercator)"\s*\]/gi, 'PROJECTION["Transverse_Mercator"]')

  for (const [source, target] of WKT_PARAMETER_NAMES) {
    const escaped = source.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    normalized = normalized.replace(new RegExp(`PARAMETER\\[\\s*"${escaped}"`, 'gi'), `PARAMETER["${target}"`)
  }
  return normalized
}
