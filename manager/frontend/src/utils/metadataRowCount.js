export const optionalCount = (value) => {
  if (value === null || value === undefined || value === '') return null
  const count = Number(value)
  return Number.isFinite(count) && count >= 0 ? count : null
}

export const pickNestedCount = (source, paths) => {
  for (const path of paths) {
    let current = source
    for (const segment of path) {
      current = current?.[segment]
    }
    const value = optionalCount(current)
    if (value !== null) return value
  }
  return null
}
