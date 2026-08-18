function findMarkupEnd(source, start, terminator = '>') {
  let quote = ''
  let bracketDepth = 0

  for (let index = start; index < source.length; index += 1) {
    const char = source[index]
    if (quote) {
      if (char === quote) quote = ''
      continue
    }
    if (char === '"' || char === "'") {
      quote = char
      continue
    }
    if (terminator === '>' && char === '[') {
      bracketDepth += 1
      continue
    }
    if (terminator === '>' && char === ']') {
      bracketDepth = Math.max(0, bracketDepth - 1)
      continue
    }
    if (bracketDepth === 0 && source.startsWith(terminator, index)) {
      return index + terminator.length
    }
  }

  return -1
}

function tokenizeXML(source) {
  const tokens = []
  let index = 0

  while (index < source.length) {
    if (source[index] !== '<') {
      const nextTag = source.indexOf('<', index)
      const end = nextTag === -1 ? source.length : nextTag
      tokens.push({ kind: 'text', raw: source.slice(index, end) })
      index = end
      continue
    }

    let end = -1
    let kind = 'declaration'
    if (source.startsWith('<!--', index)) {
      end = findMarkupEnd(source, index + 4, '-->')
      kind = 'comment'
    } else if (source.startsWith('<![CDATA[', index)) {
      end = findMarkupEnd(source, index + 9, ']]>')
      kind = 'cdata'
    } else if (source.startsWith('<?', index)) {
      end = findMarkupEnd(source, index + 2, '?>')
      kind = 'processing'
    } else {
      end = findMarkupEnd(source, index + 1)
    }
    if (end === -1) return null

    const raw = source.slice(index, end)
    if (kind === 'declaration' && /^<\s*\//.test(raw)) {
      const match = raw.match(/^<\s*\/\s*([^\s>]+)\s*>$/)
      if (!match) return null
      tokens.push({ kind: 'close', name: match[1], raw })
    } else if (kind === 'declaration' && /^<\s*[^!?]/.test(raw)) {
      const match = raw.match(/^<\s*([^\s/>]+)/)
      if (!match) return null
      tokens.push({
        kind: /\/\s*>$/.test(raw) ? 'self-closing' : 'open',
        name: match[1],
        raw
      })
    } else {
      tokens.push({ kind, raw })
    }
    index = end
  }

  return tokens
}

function parseXMLTree(source) {
  const tokens = tokenizeXML(source)
  if (!tokens) return null

  const root = { kind: 'root', children: [] }
  const stack = [root]
  let rootElementCount = 0

  for (const token of tokens) {
    const parent = stack[stack.length - 1]
    if (token.kind === 'text') {
      if (stack.length === 1 && token.raw.trim()) return null
      parent.children.push(token)
      continue
    }
    if (token.kind === 'open') {
      const element = { ...token, children: [], closeRaw: '' }
      parent.children.push(element)
      stack.push(element)
      if (stack.length === 2) rootElementCount += 1
      continue
    }
    if (token.kind === 'self-closing') {
      parent.children.push(token)
      if (stack.length === 1) rootElementCount += 1
      continue
    }
    if (token.kind === 'close') {
      if (stack.length === 1 || parent.name !== token.name) return null
      parent.closeRaw = token.raw
      stack.pop()
      continue
    }
    parent.children.push(token)
  }

  return stack.length === 1 && rootElementCount === 1 ? root : null
}

function serializeOriginal(node) {
  if (node.kind === 'open') {
    return `${node.raw}${node.children.map(serializeOriginal).join('')}${node.closeRaw}`
  }
  return node.raw || ''
}

function renderNode(node, depth) {
  const indent = '  '.repeat(depth)
  if (node.kind !== 'open') {
    if (node.kind === 'text' && !node.raw.trim()) return ''
    return `${indent}${node.raw.trim()}`
  }

  const meaningfulChildren = node.children.filter(child => child.kind !== 'text' || child.raw.trim())
  if (meaningfulChildren.length === 0) {
    return `${indent}${node.raw}${node.closeRaw}`
  }

  const hasDirectText = meaningfulChildren.some(child => child.kind === 'text' && child.raw.trim())
  if (hasDirectText) {
    return `${indent}${serializeOriginal(node).trim()}`
  }

  if (meaningfulChildren.length === 1) {
    const child = meaningfulChildren[0]
    if ((child.kind === 'cdata' || child.kind === 'text') && !child.raw.includes('\n')) {
      return `${indent}${node.raw}${child.raw}${node.closeRaw}`
    }
  }

  const body = meaningfulChildren
    .map(child => renderNode(child, depth + 1))
    .filter(Boolean)
    .join('\n')
  return `${indent}${node.raw}\n${body}\n${indent}${node.closeRaw}`
}

function browserCanParseXML(source) {
  if (typeof DOMParser === 'undefined') return true
  const document = new DOMParser().parseFromString(source, 'application/xml')
  return document.getElementsByTagName('parsererror').length === 0
}

export function formatXMLForDisplay(value) {
  const source = String(value || '')
  if (!source.trim() || !browserCanParseXML(source)) return source

  const tree = parseXMLTree(source)
  if (!tree) return source

  return tree.children
    .map(child => renderNode(child, 0))
    .filter(Boolean)
    .join('\n')
}

export function isXMLTextPreview(data) {
  const object = data?.object || {}
  const content = object.content || {}
  const format = String(
    content.metadata?.format ||
    object.attributes?.item?.format ||
    data?.metadata?.format ||
    data?.format ||
    ''
  ).trim().toLowerCase()

  return format === 'xml' && String(content.kind || '').trim().toLowerCase() === 'text'
}

export function withFormattedXMLPreview(data, rawMode = false) {
  if (rawMode || !isXMLTextPreview(data)) return data

  const content = data.object.content
  const formattedText = formatXMLForDisplay(content.text)
  if (formattedText === content.text) return data

  return {
    ...data,
    object: {
      ...data.object,
      content: {
        ...content,
        text: formattedText
      }
    }
  }
}
