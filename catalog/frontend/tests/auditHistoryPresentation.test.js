import { describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import zh from '../src/i18n/zh-cn.json'
import en from '../src/i18n/en.json'
import { auditEventSummary, auditEventTitle } from '../src/utils/auditHistoryPresentation'

const eventTypes = [
  'source_discovered', 'catalog.entry.updated', 'catalog.entry.curation_withdrawn',
  'catalog.entry.certified', 'catalog.entry.certification_withdrawn', 'catalog.entry.deprecated',
  'catalog.entry.deprecation_updated', 'catalog.entry.deprecation_withdrawn',
  'catalog.entry.responsibilities_transferred', 'catalog.entry.batch_governance_applied',
  'catalog.entry.merged', 'catalog.source.rebound', 'catalog.responsibility.reference_state_changed',
  'catalog.sharing_decision.created', 'catalog.standard_mapping.proposed',
  'catalog.standard_mapping.updated', 'catalog.standard_mapping.deleted',
  'catalog.standard_mapping.approved', 'catalog.standard_mapping.rejected', 'catalog.standard_mapping.withdrawn'
]

describe.each(['zh-cn', 'en'])('Catalog audit presentation in %s', locale => {
  const missing = vi.fn()
  const { t } = createI18n({ legacy: false, locale, fallbackLocale: false, messages: { 'zh-cn': zh, en }, missing }).global
  const summary = (details, event_type = 'catalog.entry.updated') => auditEventSummary({ event_type, details }, t)
  const value = (rows, key) => rows.find(row => row.label === t(key))?.value

  it('translates every canonical event without confusing approval with grant issuance', () => {
    for (const event_type of eventTypes) {
      const title = auditEventTitle({ event_type }, t)
      expect(title).not.toBe(event_type)
      expect(title).not.toContain('catalog.history.')
    }
    expect(auditEventTitle({ event_type: 'catalog.sharing_decision.created' }, t)).toBe(locale === 'en' ? 'Data use approved' : '批准数据使用')
    expect(missing).not.toHaveBeenCalled()
  })

  it('shows recorded state/version transitions, zero counts and responsibility roles', () => {
    const rows = summary({ previous_governance_status: 'discovered', governance_status: 'curated', visibility: 'tenant',
      previous_version: 1, version: 2, domain_count: 1, glossary_count: 0, responsibility_count: 2,
      responsibilities: [{ role: 'business_owner', subject_id: '9007199254740993' }, { role: 'business_owner' }, { role: 'data_steward' }] })
    expect(value(rows, 'catalog.entries.governanceStatus')).toBe(`${t('catalog.status.governance.discovered')} → ${t('catalog.status.governance.curated')}`)
    expect(value(rows, 'catalog.entries.visibility')).toBe(t('catalog.status.visibility.tenant'))
    expect(value(rows, 'catalog.history.fields.entryVersion')).toBe('1 → 2')
    expect(value(rows, 'catalog.history.fields.glossaryCount')).toBe('0')
    expect(value(rows, 'catalog.edit.responsibilityRole')).toBe([t('catalog.edit.role.business_owner'), t('catalog.edit.role.data_steward')].join(t('catalog.edit.listSeparator')))
    expect(JSON.stringify(rows)).not.toContain('9007199254740993')
    expect(missing).not.toHaveBeenCalled()
  })

  it('distinguishes mapping versions and frozen certification evidence', () => {
    const mapping = summary({ version: '9007199254740993', review_status: 'approved', opinion: 'Reviewed' }, 'catalog.standard_mapping.approved')
    expect(value(mapping, 'catalog.history.fields.mappingVersion')).toBe('9007199254740993')
    expect(value(mapping, 'catalog.mapping.status')).toBe(t('catalog.mapping.states.approved'))
    expect(value(mapping, 'catalog.history.fields.entryVersion')).toBeUndefined()
    expect(value(summary({ approved_standard_mappings: [{ id: '1', version: 2 }] }, 'catalog.entry.certified'), 'catalog.history.fields.certifiedMappingCount')).toBe('1')
    expect(value(summary({ entry_version: '3' }, 'catalog.sharing_decision.created'), 'catalog.history.fields.entryVersion')).toBe('3')
    expect(missing).not.toHaveBeenCalled()
  })

  it('keeps source, manual evidence and batch operations readable', () => {
    expect(value(summary({ source_module: 'meta' }, 'source_discovered'), 'catalog.entry.sourceModule')).toBe(t('catalog.history.modules.meta'))
    expect(value(summary({ evidence: 'Manual verification' }, 'catalog.source.rebound'), 'catalog.rebind.evidence')).toBe('Manual verification')
    for (const [operation, key] of [['assign_primary_domain', 'assignPrimaryDomain'], ['assign_accountable_department', 'assignAccountableDepartment']]) {
      expect(value(summary({ operation }, 'catalog.entry.batch_governance_applied'), 'catalog.entries.batchGovernance.operation')).toBe(t(`catalog.entries.batchGovernance.${key}`))
    }
    expect(missing).not.toHaveBeenCalled()
  })

  it('shows each recorded responsibility state transition and reason in both languages', () => {
    const changes = [
      { role: 'data_steward', subject_id: '9007199254740993', previous_status: 'active', status: 'needs_transfer', reason: 'subject_not_found' },
      { role: 'data_steward', subject_id: '9007199254740994', previous_status: 'active', status: 'needs_transfer', reason: 'subject_not_referenceable' },
      { role: 'business_owner', previous_status: 'needs_transfer', status: 'active', reason: 'reference_restored' }
    ]
    const before = JSON.stringify(changes)
    const rows = summary({ changes }, 'catalog.responsibility.reference_state_changed')
    const changeRows = rows.slice(-3)
    expect(changeRows.map(row => row.value)).toEqual(locale === 'en' ? [
      'Active → Transfer required; Reason: Subject not found',
      'Active → Transfer required; Reason: Subject is not currently referenceable',
      'Transfer required → Active; Reason: Subject is referenceable again'
    ] : [
      '有效 → 待移交；原因：主体不存在',
      '有效 → 待移交；原因：主体当前不可引用',
      '待移交 → 有效；原因：主体已恢复为可引用'
    ])
    expect(new Set(changeRows.map(row => row.label)).size).toBe(3)
    expect(changeRows[0].label).toContain(t('catalog.edit.role.data_steward'))
    expect(JSON.stringify(rows)).not.toContain('9007199254740993')
    expect(JSON.stringify(changes)).toBe(before)
    expect(missing).not.toHaveBeenCalled()
  })

  it('does not infer missing or unknown responsibility states and reasons', () => {
    const changes = [null, [], 'bad', { role: {}, previous_status: {}, status: 'future', reason: { text: 'nested' } },
      { role: 'data_steward', status: 'active', reason: 'future_reason' }]
    const before = JSON.stringify(changes)
    const rows = summary({ changes }, 'catalog.responsibility.reference_state_changed')
    const changeRows = rows.slice(-2)
    expect(changeRows.map(row => row.value)).toEqual(locale === 'en' ? [
      'State not recorded or unrecognized → State not recorded or unrecognized; Reason: Not recorded or unrecognized',
      'State not recorded or unrecognized → Active; Reason: Not recorded or unrecognized'
    ] : [
      '状态未记录或无法识别 → 状态未记录或无法识别；原因：未记录或无法识别',
      '状态未记录或无法识别 → 有效；原因：未记录或无法识别'
    ])
    expect(JSON.stringify(rows)).not.toContain('[object Object]')
    expect(JSON.stringify(rows)).not.toContain('future_reason')
    expect(JSON.stringify(changes)).toBe(before)
    expect(summary({ changes }, 'catalog.entry.updated')).toHaveLength(2)
    expect(missing).not.toHaveBeenCalled()
  })

  it('does not coerce malformed objects, guess unknown semantics or mutate history', () => {
    const details = { reason: { text: 'nested' }, evidence: [{ nested: true }], version: Number.MAX_SAFE_INTEGER + 1,
      glossary_count: -1, visibility: {}, governance_status: 'future', review_status: 'future', source_module: 'future',
      responsibilities: [null, { role: {} }, { role: 'future' }] }
    const before = JSON.stringify(details)
    expect(summary(details)).toEqual([])
    expect(JSON.stringify(details)).toBe(before)
    for (const event_type of ['future.event', 'toString', '__proto__']) {
      expect(auditEventTitle({ event_type }, t)).toBe(t('catalog.history.events.other'))
      expect(summary({ version: 2 }, event_type)).toEqual([])
    }
    for (const empty of [null, [], 'text']) expect(summary(empty)).toEqual([])
    expect(missing).not.toHaveBeenCalled()
  })
})
