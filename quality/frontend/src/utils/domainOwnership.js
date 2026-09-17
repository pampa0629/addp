export const normalizeDomainFilter = value => {
  if (Array.isArray(value) || value == null || !/^\d+$/.test(String(value))) return null
  const id = Number(value)
  return Number.isSafeInteger(id) && id >= 0 ? id : null
}

export const domainOwnershipLabel = (id, t, domains = []) => {
  if (id == null) return t('quality.domain.public')
  const domain = domains.find(item => item.id === id)
  return domain ? `${domain.name} · ${domain.code}` : t('quality.domain.identifier', { id })
}

export const executionDomainLabel = (config, t) => {
  if (!config || !Object.hasOwn(config, 'owner_domain_id')) return t('quality.domain.unrecorded')
  return domainOwnershipLabel(config.owner_domain_id, t)
}
