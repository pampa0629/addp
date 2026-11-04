import { describe, expect, it } from 'vitest'
import { captureIndependentGrant } from '../src/utils/independentGrant'
import zh from '../src/i18n/zh-cn.json'
import en from '../src/i18n/en.json'

const path = { engine_id: '2', version: 'catalog.path/v1', segments: [
  { term: 'server', kind: 'server', name: '' }, { term: 'schema', kind: 'namespace', name: 'outdoor' },
  { term: 'table', kind: 'table', name: 'a"b/中文' }
] }
const binding = { item_fingerprint: 'a'.repeat(64), boundary_hash: 'b'.repeat(64), native_target: path }
const requirement = { binding, engine_id: '2', catalog_path: path }
const candidates = [{ id: '9007199254740993', name: 'Outdoor reader' }]
const form = { action: 'read', recipientType: 'user', recipientID: candidates[0].id, expiryMode: 'until_revoked', expiresAt: null, reason: ' Explicit read ' }
const collectionPath = { engine_id: '11', version: 'catalog.path/v1', segments: [
  { term: 'server', kind: 'server', name: '' }, { term: 'database', kind: 'namespace', name: 'Outdoor' },
  { term: 'collection', kind: 'collection', name: 'Persons' }
] }
const collectionRequirement = { engine_id: '11', catalog_path: collectionPath,
  binding: { ...binding, native_target: collectionPath } }

describe('exact native collection grant capture', () => {
  it.each(['read', 'metadata_read'])('captures %s with the exact collection, subject, hash and one-hour expiry', action => {
    const now = Date.UTC(2026, 10, 4), expiresAt = new Date(now + 3600000)
    const captured = captureIndependentGrant('11', collectionRequirement, { ...form, action, expiryMode: 'at_time', expiresAt }, candidates, now)
    expect(JSON.parse(captured.payload)).toEqual({ catalog_path: { ...collectionPath, engine_id: 11 },
      item_fingerprint: binding.item_fingerprint, expected_boundary_hash: binding.boundary_hash,
      request_id: captured.requestID, recipient_type: 'user', recipient_id: candidates[0].id,
      action, expiry_mode: 'at_time', expires_at: expiresAt.toISOString(), reason: 'Explicit read' })
    expect(captured.nodeRecursive).toBeUndefined()
    expect(captured.targetLabel).toBe('Outdoor / Persons')
    expect(Object.isFrozen(captured)).toBe(true)
  })
  it('rejects node substitution, wrong native identity, missing or malformed binding and expired commands', () => {
    const changedPath = change => ({ ...collectionPath, ...change })
    const wrongDatabase = changedPath({ segments: collectionPath.segments.map((part, i) => i === 1 ? { ...part, name: 'Other' } : part) })
    const wrongCollection = changedPath({ segments: collectionPath.segments.map((part, i) => i === 2 ? { ...part, name: 'Other' } : part) })
    for (const changed of [{ binding: null }, { node_recursive: false }, { engine_id: '12' },
      { catalog_path: changedPath({ engine_id: '12' }) },
      { catalog_path: changedPath({ segments: [...collectionPath.segments.slice(0, -1), { term: 'collection', kind: 'table', name: 'Persons' }] }) },
      ...[{}, { item_fingerprint: '' }, { boundary_hash: '' }, { boundary_hash: 'not-a-hash' },
        { native_target: wrongDatabase }, { native_target: wrongCollection },
        { native_target: changedPath({ engine_id: '12' }) }].map(change => ({ binding: Object.keys(change).length ? { ...collectionRequirement.binding, ...change } : {} }))]) {
      expect(() => captureIndependentGrant('11', { ...collectionRequirement, ...changed }, form, candidates)).toThrow()
    }
    for (const changed of [{ recipientID: 'unknown' }, { recipientType: 'service_principal' },
      { expiryMode: 'at_time', expiresAt: new Date(1000) }, { expiryMode: 'at_time', expiresAt: new Date(NaN) }]) {
      expect(() => captureIndependentGrant('11', collectionRequirement, { ...form, ...changed }, candidates, 1000)).toThrow()
    }
  })
  it('allows metadata-only collection identity only for metadata_read', () => {
    const metadata = { ...collectionRequirement, binding: { item_fingerprint: binding.item_fingerprint,
      boundary_hash: binding.boundary_hash, metadata_target: { target: collectionPath, layout: 'single',
        data_type: 'collection', format: 'mongodb', composition_digest: 'c'.repeat(64) } } }
    expect(JSON.parse(captureIndependentGrant('11', metadata, { ...form, action: 'metadata_read' }, candidates).payload).action).toBe('metadata_read')
    expect(() => captureIndependentGrant('11', metadata, form, candidates)).toThrow()
  })
})
describe('independent read-grant command capture', () => {
  it('captures one whole file DataItem, lists its constituents and never submits caller composition', () => {
    const file = name => ({ engine_id: '33', version: 'catalog.path/v1', segments: [{ term: 'root', kind: 'root', name: '' }, { term: 'directory', kind: 'directory', name: 'Models' }, { term: 'file', kind: 'file', name }] })
    const primary = file('Model.3DS'), texture = file('TEST.PNG')
    const binding = { item_fingerprint: 'a'.repeat(64), boundary_hash: 'b'.repeat(64), file_composition: { format: '3ds', refs: [
      { target: primary, role: 'model', required: true, primary: true }, { target: texture, role: 'texture', required: true, primary: false }
    ] } }
    const requirement = { engine_id: '33', catalog_path: primary, binding }
    const captured = captureIndependentGrant('33', requirement, form, candidates)
    expect(captured.compositionPaths).toEqual(['Models / Model.3DS', 'Models / TEST.PNG'])
    expect(Object.isFrozen(captured.compositionPaths)).toBe(true)
    expect(JSON.parse(captured.payload)).toMatchObject({ catalog_path: { ...primary, engine_id: 33 }, item_fingerprint: binding.item_fingerprint, expected_boundary_hash: binding.boundary_hash })
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
    expect(body).toEqual({ item_fingerprint: binding.item_fingerprint, expected_boundary_hash: binding.boundary_hash, request_id: captured.requestID, recipient_type: 'user', recipient_id: candidates[0].id,
      action: 'read', expiry_mode: 'until_revoked', expires_at: null, reason: 'Explicit read', catalog_path: { ...path, engine_id: 2 } })
    expect(Object.isFrozen(captured)).toBe(true)
    expect(captured.recipientLabel).toBe(candidates[0].name)
    expect(captured.recipientType).toBe('user')
    expect(captured.recipientID).toBe('9007199254740993')
    expect(captured.targetLabel).toBe('outdoor / a"b/中文')
    expect(body).not.toHaveProperty('direct_permission_source')
    expect(body).not.toHaveProperty('direct_permission_version')
    expect(body).not.toHaveProperty('initialize_approval')
    expect(body).not.toHaveProperty('requirement_version')
    expect(captureIndependentGrant('2', requirement, { ...form, recipientType: 'department' }, candidates).payload).toContain('"recipient_type":"department"')
  })
  it('requires current candidates, exact independent target, valid boundary and explicit expiry', () => {
    for (const changed of [{ recipientID: '1' }, { recipientType: 'service_principal' }, { expiryMode: '' }, { reason: ' ' }, { reason: '字'.repeat(2001) }, { expiryMode: 'at_time', expiresAt: new Date(1) }]) {
      expect(() => captureIndependentGrant('2', requirement, { ...form, ...changed }, candidates)).toThrow()
    }
    for (const changed of [{ binding: null }, { binding: { ...binding, boundary_hash: '' } }, { engine_id: '3' },
      { catalog_path: { ...path, segments: [...path.segments.slice(0, -1), { term: 'table', kind: 'view', name: 'view' }] } }]) {
      expect(() => captureIndependentGrant('2', { ...requirement, ...changed }, form, candidates)).toThrow()
    }
    expect(() => captureIndependentGrant('2', requirement, form, [])).toThrow()
    const future = captureIndependentGrant('2', requirement, { ...form, expiryMode: 'at_time', expiresAt: new Date(2000) }, candidates, 1000)
    expect(JSON.parse(future.payload).expires_at).toBe('1970-01-01T00:00:02.000Z')
  })
})

it('requires an explicit action and freezes metadata separately from read', () => {
 for (const action of ['', undefined, 'write']) expect(() => captureIndependentGrant('2', requirement, { ...form, action }, candidates)).toThrow()
 const draft = { ...form, action: 'metadata_read' }
 const command = captureIndependentGrant('2', requirement, draft, candidates)
 draft.action = 'read'
 expect(command.action).toBe('metadata_read')
 expect(JSON.parse(command.payload).action).toBe('metadata_read')
})


it('captures a persistent node scope independently of DataItem boundaries', () => {
 const node = { engine_id: '2', version: 'catalog.path/v1', segments: path.segments.slice(0,2) }
 for (const recursive of [false,true]) {
  const captured=captureIndependentGrant('2',{engine_id:'2',catalog_path:node,node_recursive:recursive},form,candidates)
  const body=JSON.parse(captured.payload)
  expect(body.node_recursive).toBe(recursive);expect(captured.nodeRecursive).toBe(recursive)
  expect(body).not.toHaveProperty('item_fingerprint');expect(body).not.toHaveProperty('expected_boundary_hash')
  expect(captured.compositionPaths).toEqual([])
  expect(() => captureIndependentGrant('2',{engine_id:'2',catalog_path:path,node_recursive:recursive},form,candidates)).toThrow()
  expect(() => captureIndependentGrant('2',{engine_id:'2',catalog_path:node,node_recursive:recursive,binding},form,candidates)).toThrow()
 }
 expect(() => captureIndependentGrant('2',{engine_id:'2',catalog_path:node},form,candidates)).toThrow()
 expect(zh.system.engine.sourceGrants.confirmNode).toContain('以后新识别')
 expect(en.system.engine.sourceGrants.confirmNode).toContain('newly identified')
})

it('metadata-only targets can issue metadata grants without content elevation', () => {
 const target={engine_id:'2',version:'catalog.path/v1',segments:[{term:'root',kind:'root',name:''},{term:'directory',kind:'directory',name:'outputs'},{term:'file',kind:'file',name:'nodes.gpkg'}]}
 const metadata_target={target,layout:'single',data_type:'container',format:'geopackage',composition_digest:'c'.repeat(64)}
 const requirement={engine_id:'2',catalog_path:target,binding:{item_fingerprint:'a'.repeat(64),boundary_hash:'b'.repeat(64),metadata_target}}
 const captured=captureIndependentGrant('2',requirement,{...form,action:'metadata_read'},candidates)
 expect(JSON.parse(captured.payload)).toMatchObject({action:'metadata_read',item_fingerprint:'a'.repeat(64),expected_boundary_hash:'b'.repeat(64)})
 expect(JSON.parse(captured.payload).metadata_target).toBeUndefined()
 expect(()=>captureIndependentGrant('2',requirement,form,candidates)).toThrow()
 expect(()=>captureIndependentGrant('2',{...requirement,binding:{...requirement.binding,native_target:target}},{...form,action:'metadata_read'},candidates)).toThrow()
})
