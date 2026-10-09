import { escapeHtml } from './formatters.js'

export function retrievalResultPath(item = {}, formatLocator) {
  const indexedPath = item.full_name || item.path || item.relative_path
  if (indexedPath) return indexedPath
  return formatLocator?.(item.locator) || ''
}

// Escape source text first; only the search service's bare mark tags are HTML.
const highlightHtml = value => escapeHtml(value)
  .replace(/&lt;mark&gt;/g, '<mark>').replace(/&lt;\/mark&gt;/g, '</mark>')

export function retrievalFieldText(field = {}, key) {
  const highlighted = field.highlights?.[key]
  return highlighted ? highlightHtml(highlighted) : escapeHtml(field[key] || '')
}

export function retrievalSnippet(item = {}) {
  for (const key of ['content', 'content_preview', 'metadata.summary', 'metadata.tags', 'description', 'tags', 'keywords', 'author', 'title', 'name', 'full_name', 'file_name']) {
    const fragments = item.highlights?.[key]
    if (Array.isArray(fragments) && fragments.length) return highlightHtml(fragments[0])
  }
  return item.content_preview ? escapeHtml(item.content_preview.slice(0, 200)) : ''
}
