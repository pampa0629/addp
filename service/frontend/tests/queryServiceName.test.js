import assert from 'node:assert/strict'
import test from 'node:test'
import { readFile } from 'node:fs/promises'

import { validateServiceName } from '../src/utils/serviceHelper.js'

test('query service names accept the documented URL-safe separators', () => {
  assert.equal(validateServiceName('commerce-order-analysis'), true)
  assert.equal(validateServiceName('commerce_order_analysis'), true)
  assert.equal(validateServiceName('commerce order analysis'), false)
  assert.equal(validateServiceName('CommerceOrderAnalysis'), false)
})

test('query service form and i18n describe the same service-name contract', async () => {
  const [source, zhCn, en] = await Promise.all([
    readFile(new URL('../src/views/QueryServiceForm.vue', import.meta.url), 'utf8'),
    readFile(new URL('../src/i18n/zh-cn.json', import.meta.url), 'utf8').then(JSON.parse),
    readFile(new URL('../src/i18n/en.json', import.meta.url), 'utf8').then(JSON.parse)
  ])

  assert.match(source, /SERVICE_NAME_PATTERN/)
  assert.match(zhCn.service.query.serviceNameHelp, /连字符/)
  assert.match(en.service.query.serviceNameHelp, /hyphens/)
})
