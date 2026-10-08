export function maxTextureDeclarations(rows) {
  const references = new Set()
  for (const { reference, path } of rows) {
    if (!reference.trim() || !path.trim()) return { error: 'incomplete' }
    if (references.has(reference)) return { error: 'duplicate' }
    references.add(reference)
    if (path.startsWith('/') || /[\\:\u0000]/.test(path) || path.split('/').includes('..') || path.split('/').every(part => !part || part === '.')) {
      return { error: 'outsideDirectory' }
    }
  }
  return { mapping: Object.fromEntries(rows.map(({ reference, path }) => [reference, path])) }
}
