const documentTypeTagTypes = Object.freeze({
  national: 'danger',
  industry: 'warning',
  internal: 'primary',
  reference: 'info'
})

export const getDocumentTypeTagType = type => documentTypeTagTypes[type] || 'info'
