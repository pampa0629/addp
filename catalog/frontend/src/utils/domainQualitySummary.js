export const DOMAIN_QUALITY_SECTIONS = [
  { key: 'rules', permission: 'quality.rule.read', route: '/quality/rules' },
  { key: 'plans', permission: 'quality.plan.read', route: '/quality/plans' },
  { key: 'issues', permission: 'quality.issue.read', route: '/quality/issues' }
]

export function domainQualityQuery(domainId, key) {
  return {
    owner_domain_id: String(domainId),
    page: 1,
    page_size: 3,
    ...(key === 'issues' ? { status: 'open' } : {})
  }
}

export function domainQualityDisplay(item, key) {
  if (key === 'issues') {
    const target = [item?.table_name, item?.column_name].filter(Boolean).join('.')
    return { id: item?.id, name: target || item?.type || String(item?.id || ''), code: item?.type || '' }
  }
  return { id: item?.id, name: item?.name || item?.code || '', code: item?.code || '' }
}

export async function loadDomainQualitySection(section, { domainId, hasPermission, list }) {
  const version = ++section.requestVersion
  section.items = []
  section.total = 0
  if (!domainId || !hasPermission(section.permission)) {
    section.status = 'forbidden'
    return
  }
  section.status = 'loading'
  try {
    const response = await list(domainQualityQuery(domainId, section.key))
    if (version !== section.requestVersion) return
    section.items = (response.data || []).map(item => domainQualityDisplay(item, section.key))
    section.total = Number(response.total) || 0
    section.status = 'ready'
  } catch (error) {
    if (version !== section.requestVersion) return
    section.status = error?.response?.status === 403 ? 'forbidden' : 'unavailable'
  }
}
