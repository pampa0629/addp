export const DOMAIN_QUALITY_SECTIONS = [
  { key: 'rules', permission: 'quality.rule.read', route: '/quality/rules' },
  { key: 'plans', permission: 'quality.plan.read', route: '/quality/plans' },
  { key: 'issues', permission: 'quality.issue.read', route: '/quality/issues' }
]

export function domainQualityQuery(domainId, key) {
  return {
    owner_domain_id: String(domainId),
    page: 1,
    page_size: 1,
    ...(key === 'issues' ? { status: 'open' } : {})
  }
}

export async function loadDomainQualitySection(section, { domainId, hasPermission, list }) {
  const version = ++section.requestVersion
  section.total = 0
  if (!domainId || !hasPermission(section.permission)) {
    section.status = 'forbidden'
    return
  }
  section.status = 'loading'
  try {
    const response = await list(domainQualityQuery(domainId, section.key))
    if (version !== section.requestVersion) return
    if (!Number.isSafeInteger(response?.total) || response.total < 0) throw new Error('Invalid Quality total')
    section.total = response.total
    section.status = 'ready'
  } catch (error) {
    if (version !== section.requestVersion) return
    section.status = error?.response?.status === 403 ? 'forbidden' : 'unavailable'
  }
}
