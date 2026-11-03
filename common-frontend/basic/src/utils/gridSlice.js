// Sizes remain decimal text; a UI number is only allowed where it is exact.
export function gridWindowLimit(size, start = 0) {
  if (!/^\d+$/.test(String(size)) || !Number.isSafeInteger(start) || start < 0) return 0
  const remaining = BigInt(size) - BigInt(start)
  return remaining > 128n ? 128 : remaining > 0n ? Number(remaining) : 0
}
export function validateGridSelection(variable, selection) {
  const dimensions = variable?.dimensions
  if (!variable?.slice_supported || !Array.isArray(dimensions) || dimensions.length < 2 || dimensions.length > 8 || selection?.variable !== variable.name) return false
  if (!Array.isArray(selection.indices) || selection.indices.length !== dimensions.length - 2) return false
  if (!selection.indices.every((value, index) => Number.isSafeInteger(value) && value >= 0 && BigInt(value) < BigInt(dimensions[index].size))) return false
  if (![selection.x, selection.y, selection.width, selection.height].every(Number.isSafeInteger)) return false
  return selection.x >= 0 && selection.y >= 0 && selection.width > 0 && selection.height > 0 && selection.width <= gridWindowLimit(dimensions.at(-1).size, selection.x) && selection.height <= gridWindowLimit(dimensions.at(-2).size, selection.y) && selection.width * selection.height <= 4096
}
export function gridColorValues(values, valid) {
  return values.flatMap((row, y) => row.map((text, x) => {
    if (!valid[y]?.[x]) return null
    if (/^-?\d+$/.test(text) && !Number.isSafeInteger(Number(text))) return null
    const number = Number(text)
    return Number.isFinite(number) ? number : null
  })).filter(number => number !== null)
}

export function gridValueModeKey(mode) {
  return mode === 'raw_storage' ? 'rawValues' : mode === 'decoded_native_units' ? 'decodedValues' : ''
}
