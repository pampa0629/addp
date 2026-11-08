import { describe, expect, it } from 'vitest'
import zh from '../src/i18n/zh-cn.json'
import en from '../src/i18n/en.json'
import { buildEngineConfigurationNote } from '../src/utils/engineConfigurationNote'

const descriptor = { type: 'fixture_engine', connection_spec: { fields: [
  { key: 'host', sensitive: false }, { key: 'port', sensitive: false, default: 9999 },
  { key: 'password', sensitive: true }, { key: 'token', sensitive: true },
  { key: 'private_key', sensitive: true }, { key: 'enabled', sensitive: false },
  { key: 'empty', sensitive: false }, { key: 'nullable', sensitive: false },
  { key: 'object', sensitive: false }, { key: 'absent', sensitive: false, default: 'not-saved' }
] } }
const engine = { id: '9007199254740993', name: 'Example', engine_type: 'fixture_engine', version: 7,
  lifecycle_state: 'active', connection_info: {
    host: 'fixture-host', port: 0, password: 'password-sentinel', token: 'token-sentinel',
    private_key: 'private-key-sentinel', enabled: false, empty: '', nullable: null,
    object: { password: 'nested-sentinel' }, unknown: 'undeclared-sentinel'
  }, capabilities: { custom: 'raw-capability-sentinel' }, scan_config: { secret: 'scan-sentinel' },
  check_message: 'error-sentinel', deletion_error: 'deletion-sentinel' }
const translator = messages => path => path.split('.').reduce((value, part) => value?.[part], messages) || path
const note = (changes = {}, messages = zh) => buildEngineConfigurationNote({
  engine, descriptor, generatedAt: '2026-11-08T06:00:00Z', summary: [], sections: [],
  t: translator(messages), ...changes
})

describe('engine configuration note', () => {
  it('exports saved declared scalars in plugin order without secrets, unknown fields or defaults', () => {
    const { fileName, content } = note()
    expect(fileName).toBe('engine-9007199254740993-configuration.md')
    expect(content).toContain('| host | fixture\\-host |')
    expect(content).toContain('| port | 0 |')
    expect(content).toContain('| enabled | false |')
    expect(content).toContain('| empty |  |')
    expect(content).toContain('| nullable |  |')
    expect(content.indexOf('| host |')).toBeLessThan(content.indexOf('| port |'))
    for (const value of ['password-sentinel', 'token-sentinel', 'private-key-sentinel', 'nested-sentinel',
      'undeclared-sentinel', 'not-saved', 'raw-capability-sentinel', 'scan-sentinel', 'error-sentinel', 'deletion-sentinel', '9999']) {
      expect(content).not.toContain(value)
    }
    expect(content).toContain('| 配置版本 | 7 |')
    expect(content).toContain('不是可导入的配置备份')
  })
  it('does not use the descriptor of a different engine type or export without field declarations', () => {
    for (const invalid of [undefined, { ...descriptor, type: 'other' }, { type: 'fixture_engine' }]) {
      expect(() => note({ descriptor: invalid })).toThrow('matching connection field descriptor')
    }
  })
  it('escapes user text so pipes, links, markup and multiline values remain document text', () => {
    const { content } = note({ engine: { ...engine, name: 'a|b\n![img](https://example.test)\n<script>x</script>`',
      connection_info: { host: 'a\r\nb\rc' } } })
    expect(content).toContain('a\\|b<br>\\!\\[img\\]\\(https://example\\.test\\)<br>\\<script\\>x\\</script\\>\\`')
    expect(content).toContain('| host | a<br>b<br>c |')
    expect(content).not.toContain('\n<script>')
  })
  it.each([[zh, '引擎配置说明'], [en, 'Engine configuration note']])('uses the selected language and supplied capability display facts', (messages, title) => {
    const { content } = note({ summary: ['SQL'], sections: [{ title: 'Read', status: 'Unavailable',
      description: 'Declared only', rows: [['Table', 'Unsupported']] }] }, messages)
    expect(content).toContain(`# ${title}`)
    expect(content).toContain('### Read\n\nUnavailable')
    expect(content).toContain('| Table | Unsupported |')
    expect(content).not.toContain('system.engine.')
  })
})
