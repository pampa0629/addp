import { describe, expect, it } from 'vitest'
import { captureIndependentGrant } from '../src/utils/independentGrant'
import zh from '../src/i18n/zh-cn.json'
import en from '../src/i18n/en.json'

const path = { engine_id: '2', version: 'catalog.path/v1', segments: [
  { term: 'server', kind: 'server', name: '' }, { term: 'schema', kind: 'namespace', name: 'outdoor' },
  { term: 'table', kind: 'table', name: 'a"b/中文' }
] }
const binding = { item_fingerprint: 'a'.repeat(64), boundary_hash: 'b'.repeat(64), native_target: path }
const requirement = { binding, engine_id: '2', catalog_path: path, state: 'allowed', source: 'explicit', version: '1' }
const candidates = [{ id: '9007199254740993', name: 'Outdoor reader' }]
const form = { recipientType: 'user', recipientID: candidates[0].id, expiryMode: 'until_revoked', expiresAt: null, reason: ' Explicit read ' }
describe('independent read-grant command capture', () => {
  it('captures one whole file DataItem, lists its constituents and never submits caller composition', () => {
    const file = name => ({ engine_id: '33', version: 'catalog.path/v1', segments: [{ term: 'root', kind: 'root', name: '' }, { term: 'directory', kind: 'directory', name: 'Models' }, { term: 'file', kind: 'file', name }] })
    const primary = file('Model.3DS'), texture = file('TEST.PNG')
    const binding = { item_fingerprint: 'a'.repeat(64), boundary_hash: 'b'.repeat(64), file_composition: { format: '3ds', refs: [
      { target: primary, role: 'model', required: true, primary: true }, { target: texture, role: 'texture', required: true, primary: false }
    ] } }
    const requirement = { engine_id: '33', catalog_path: primary, state: 'allowed', source: 'deployment_default', version: '7', binding }
    const captured = captureIndependentGrant('33', requirement, form, candidates)
    expect(captured.compositionPaths).toEqual(['Models / Model.3DS', 'Models / TEST.PNG'])
    expect(Object.isFrozen(captured.compositionPaths)).toBe(true)
    expect(JSON.parse(captured.payload)).toMatchObject({ catalog_path: { ...primary, engine_id: 33 }, item_fingerprint: binding.item_fingerprint, expected_boundary_hash: binding.boundary_hash, direct_permission_source: 'deployment_default', direct_permission_version: '7' })
    expect(captured.payload).not.toContain('TEST.PNG')
    expect(captured.payload).not.toContain('file_composition')
    expect(() => captureIndependentGrant('33', { ...requirement, catalog_path: texture }, form, candidates)).toThrow()
    expect(() => captureIndependentGrant('33', { ...requirement, binding: { ...binding, file_composition: undefined, native_target: primary } }, form, candidates)).toThrow()
    expect(zh.system.engine.sourceGrants.confirmComposition).toContain('{files}')
    expect(en.system.engine.sourceGrants.confirmComposition).toContain('{files}')
  })
  it('captures a FileGDB scope without accepting a caller-defined boundary or an ordinary directory', () => {
    const root = { engine_id: '2', version: 'catalog.path/v1', segments: [{term:'root',kind:'root',name:''},{term:'directory',kind:'directory',name:'test.gdb'}] }
    const scope = { format:'filegdb', rule_version:1, root }
    const whole = { ...requirement, catalog_path:root, binding:{item_fingerprint:binding.item_fingerprint,boundary_hash:binding.boundary_hash,scope} }
    const captured = captureIndependentGrant('2', whole, form, candidates)
    expect(captured.wholeScope).toBe(true)
    expect(captured.compositionPaths).toEqual([])
    expect(JSON.parse(captured.payload).catalog_path).toEqual({...root,engine_id:2})
    expect(JSON.parse(captured.payload)).not.toHaveProperty('scope')
    expect(() => captureIndependentGrant('2',{...whole,binding:{...whole.binding,scope:undefined}},form,candidates)).toThrow()
    expect(() => captureIndependentGrant('2',{...whole,binding:{...whole.binding,scope:{...scope,rule_version:2}}},form,candidates)).toThrow()
    expect(zh.system.engine.sourceGrants.confirmWhole).toContain('以后新增')
    expect(en.system.engine.sourceGrants.confirmWhole).toContain('future')
  })
  it('labels production as its own authorization source in both locales', () => {
    expect(zh.system.engine.dataAuthorization.modes.task_production).toBe('任务产出初始授权')
    expect(en.system.engine.dataAuthorization.modes.task_production).toBe('Initial task output grant')
  })
  it('captures immutable lossless identifiers, explicit validity and one exact target', () => {
    const captured = captureIndependentGrant('2', requirement, form, candidates)
    const body = JSON.parse(captured.payload)
    expect(body).toEqual({ item_fingerprint: binding.item_fingerprint, expected_boundary_hash: binding.boundary_hash, request_id: captured.requestID, direct_permission_version: '1', direct_permission_source: 'explicit', recipient_type: 'user', recipient_id: candidates[0].id,
      action: 'read', expiry_mode: 'until_revoked', expires_at: null, reason: 'Explicit read', catalog_path: { ...path, engine_id: 2 } })
    expect(Object.isFrozen(captured)).toBe(true)
    expect(captured.recipientLabel).toBe(candidates[0].name)
    expect(captured.recipientType).toBe('user')
    expect(captured.recipientID).toBe('9007199254740993')
    expect(captured.targetLabel).toBe('outdoor / a"b/中文')
    expect(body).not.toHaveProperty('initialize_approval')
    expect(body).not.toHaveProperty('requirement_version')
    expect(captureIndependentGrant('2', requirement, { ...form, recipientType: 'department' }, candidates).payload).toContain('"recipient_type":"department"')
  })
  it('requires current candidates, exact independent target, valid version and explicit expiry', () => {
    for (const changed of [{ recipientID: '1' }, { recipientType: 'service_principal' }, { expiryMode: '' }, { reason: ' ' }, { reason: '字'.repeat(2001) }, { expiryMode: 'at_time', expiresAt: new Date(1) }]) {
      expect(() => captureIndependentGrant('2', requirement, { ...form, ...changed }, candidates)).toThrow()
    }
    for (const changed of [{ binding: null }, { binding: { ...binding, boundary_hash: '' } }, { state: 'denied' }, { state: 'unknown' }, { source: 'deployment_unknown' }, { engine_id: '3' }, { version: 0 }, { version: 9007199254740993 },
      { catalog_path: { ...path, segments: [...path.segments.slice(0, -1), { term: 'table', kind: 'view', name: 'view' }] } }]) {
      expect(() => captureIndependentGrant('2', { ...requirement, ...changed }, form, candidates)).toThrow()
    }
    expect(() => captureIndependentGrant('2', requirement, form, [])).toThrow()
    const future = captureIndependentGrant('2', requirement, { ...form, expiryMode: 'at_time', expiresAt: new Date(2000) }, candidates, 1000)
    expect(JSON.parse(future.payload).expires_at).toBe('1970-01-01T00:00:02.000Z')
  })
})
