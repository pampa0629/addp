function text(value) {
  return String(value || '').trim()
}

function containerInfo(attributes = {}) {
  return attributes?.type_info?.container || {}
}

export function containerTableChildren(attributes = {}) {
  const children = Array.isArray(containerInfo(attributes).children)
    ? containerInfo(attributes).children
    : []
  return children.filter(child => text(child?.name) && text(child?.data_type).toLowerCase() === 'table')
}

export function resolveContainerTableChild(attributes = {}, preferredName = '') {
  const children = containerTableChildren(attributes)
  if (children.length === 0) return null

  const preferred = text(preferredName)
  const defaultName = text(containerInfo(attributes).default_child)
  return children.find(child => text(child.name) === preferred) ||
    children.find(child => text(child.name) === defaultName) ||
    children[0]
}

export function isTransferableTableContainer({ dataType, representation, format, attributes, readableFormats }) {
  return text(dataType).toLowerCase() === 'container' &&
    text(representation).toLowerCase() === 'encoded' &&
    readableFormats?.has(text(format).toLowerCase()) === true &&
    containerTableChildren(attributes).length > 0
}
