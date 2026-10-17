import { describe, expect, it } from 'vitest'
import zhCn from '../src/i18n/zh-cn.json'
import en from '../src/i18n/en.json'
import { isCurrentSharingAccount, sharingRecipientLabel, sharingRecipientOptionLabel } from '../src/utils/sharingRecipientPresentation'

const auth = { principal: { type: 'user', id: '9007199254740993' } }
const translate = messages => key => key.split('.').reduce((value, part) => value[part], messages)

describe('sharing recipient display', () => {
  it('shows the resolved account name and login with an exact current-account marker in both languages', () => {
    for (const messages of [zhCn, en]) {
      const t = translate(messages)
      const label = sharingRecipientOptionLabel({ id: auth.principal.id, name: '开发管理员', code: 'addp-owner' }, 'user', auth, t)
      expect(label).toContain('开发管理员 · addp-owner')
      expect(label).toContain(t('catalog.sharing.currentAccount'))
      expect(label).not.toContain(auth.principal.id)
    }
  })
  it('does not mark a different account, same-ID project group, service or rounded number as the current account', () => {
    expect(isCurrentSharingAccount('user', '9007199254740992', auth)).toBe(false)
    expect(isCurrentSharingAccount('project_group', auth.principal.id, auth)).toBe(false)
    expect(isCurrentSharingAccount('user', 9007199254740993, auth)).toBe(false)
    expect(isCurrentSharingAccount('user', auth.principal.id, { principal: { type: 'service', id: auth.principal.id } })).toBe(false)
  })
  it('keeps unresolved historical recipients explicit without substituting their IDs or stale names', () => {
    const t = translate(zhCn)
    for (const status of ['not_found', 'unavailable']) {
      const label = sharingRecipientLabel({ recipient_type: 'user', recipient_id: '33', recipient_name: 'Stale name', recipient_label_status: status }, auth, t)
      expect(label).not.toContain('33')
      expect(label).not.toContain('Stale name')
      expect(label).toContain(t(status === 'not_found' ? 'catalog.sharing.recipientMissing' : 'catalog.sharing.recipientUnavailable'))
    }
  })
})
