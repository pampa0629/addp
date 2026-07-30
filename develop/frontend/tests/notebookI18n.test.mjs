import assert from 'node:assert/strict'
import { createI18n } from 'vue-i18n'
import enMessages from '../src/i18n/en.json' with { type: 'json' }
import zhCnMessages from '../src/i18n/zh-cn.json' with { type: 'json' }

const cases = [
  {
    locale: 'zh-cn',
    messages: zhCnMessages,
    expected: '请输入 JSON 格式的参数，例如：{"city_name": "北京"}'
  },
  {
    locale: 'en',
    messages: enMessages,
    expected: 'Enter JSON parameters, for example: {"city_name": "Beijing"}'
  }
]

for (const testCase of cases) {
  const compilationErrors = []
  const originalConsoleError = console.error
  console.error = (...args) => compilationErrors.push(args.join(' '))

  try {
    const i18n = createI18n({
      legacy: false,
      locale: testCase.locale,
      messages: {
        [testCase.locale]: testCase.messages
      }
    })

    assert.equal(
      i18n.global.t('develop.notebook.parametersPlaceholder'),
      testCase.expected
    )
  } finally {
    console.error = originalConsoleError
  }

  assert.deepEqual(compilationErrors, [])
}

console.log('notebook i18n tests passed')
