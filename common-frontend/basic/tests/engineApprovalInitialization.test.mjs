import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'
import { serializeEngineApprovalInitialization, serializeEngineCatalogTarget, readNativeApprovalFacts, observeNativeApprovalBinding, approvedBoundaryFields } from '../src/utils/engineApprovalInitialization.mjs'

const target = { engine_id: '9007199254740993', version: 'v1', segments: [{ term: 'table', kind: 'table', name: 'a"b/中文' }] }
const binding = { item_fingerprint: 'a'.repeat(64), boundary_hash: 'b'.repeat(64), native_target: target }
test('direct grants reuse the same structured target serializer', () => {
  assert.match(serializeEngineCatalogTarget(target), /"engine_id":9007199254740993,/)
  const root = resolve(import.meta.dirname, '../../..')
  const grant = readFileSync(resolve(root, 'system/frontend/src/utils/independentGrant.js'), 'utf8')
  assert.match(grant, /serializeEngineCatalogTarget.*from '@common-ui'/)
  assert.doesNotMatch(grant, /Number\(.*engine_id/)
})
test('both explicit modes use one lossless numeric path serializer', () => {
  for (const mode of ['catalog', 'independent']) {
    const body = serializeEngineApprovalInitialization(target, mode, ' Reason "quoted" ', binding)
    assert.match(body, /"engine_id":9007199254740993,/)
    assert.doesNotMatch(body, /9007199254740992/)
    assert.equal(JSON.parse(body).mode, mode)
    assert.equal(JSON.parse(body).reason, 'Reason "quoted"')
    assert.deepEqual(JSON.parse(body).catalog_path.segments, target.segments)
    assert.deepEqual(Object.keys(JSON.parse(body)), ['catalog_path', 'mode', 'reason', 'item_fingerprint', 'expected_boundary_hash'])
  }
})
test('missing mode, malformed IDs, oversized int64 and empty reasons never serialize', () => {
  for (const id of [9007199254740993, '', '0', '01', '-1', '1e3', '1,"mode":"independent"', '9223372036854775808']) {
    assert.throws(() => serializeEngineApprovalInitialization({ ...target, engine_id: id }, 'independent', 'Reason'))
  }
  for (const mode of ['', undefined, 'unknown']) assert.throws(() => serializeEngineApprovalInitialization(target, mode, 'Reason'))
  for (const reason of ['', ' ', '字'.repeat(2001)]) assert.throws(() => serializeEngineApprovalInitialization(target, 'catalog', reason))
  assert.throws(() => serializeEngineApprovalInitialization(null, 'catalog', 'Reason'))
})
test('System and Catalog consume the same initializer and shared tree owner', () => {
  const root = resolve(import.meta.dirname, '../../..')
  const read = path => readFileSync(resolve(root, path), 'utf8')
  assert.match(read('common-frontend/basic/src/index.js'), /engineApprovalInitialization.mjs/)
  const panel = read('system/frontend/src/components/engines/EngineDataAuthorization.vue')
  assert.match(panel, /ResourceTreeSelect/)
  assert.doesNotMatch(panel, /<el-tree\b|<ResourceTree\s|\/meta\//)
  assert.match(panel, /serializeEngineApprovalInitialization/)
  assert.match(read('catalog/frontend/src/components/SharingPanel.vue'), /serializeEngineApprovalInitialization/)
  assert.doesNotMatch(read('catalog/frontend/src/utils/sharingConfirmation.js'), /serializeApprovalInitialization/)
})

test('native approval observation accepts exact numeric IDs and requires the trusted complete typed boundary', async () => {
  const native = { engine_id: 2, version: 'catalog.path/v1', segments: [
    { term: 'server', kind: 'server', name: '' }, { term: 'schema', kind: 'namespace', name: 'a.b' },
    { term: 'table', kind: 'table', name: 'c' }
  ] }
  const read = async (id, fingerprint) => {
    assert.equal(id, '2')
    assert.match(fingerprint, /^[0-9a-f]{64}$/)
    return { schema_version: 'meta.data_item_authorization_facts/v1', item_fingerprint: fingerprint, basis_state: 'complete',
      boundary_hash: 'b'.repeat(64), boundary: { native_target: native } }
  }
  const observed = await observeNativeApprovalBinding(native, read)
  assert.equal(Object.isFrozen(observed), true)
  assert.equal(observed.boundary_hash, 'b'.repeat(64))
  await assert.rejects(observeNativeApprovalBinding(native, async (...args) => ({ ...await read(...args), basis_state: 'incomplete' })))
  const collision = { ...native, segments: [native.segments[0], { ...native.segments[1], name: 'a' }, { ...native.segments[2], name: 'b.c' }] }
  await assert.rejects(observeNativeApprovalBinding(native, async (...args) => ({ ...await read(...args), boundary: { native_target: collision } })))
  await assert.rejects(observeNativeApprovalBinding({ ...native, engine_id: 9007199254740992 }, () => { throw new Error('rounded identity reached owner') }))
  assert.throws(() => serializeEngineApprovalInitialization({ ...native, engine_id: '2' }, 'independent', 'Explicit', undefined))
  for (const state of ['incomplete', 'needs_refresh', 'unsupported']) {
    const readState = async (...args) => ({ ...await read(...args), basis_state: state, boundary: null, boundary_hash: null })
    assert.deepEqual(await readNativeApprovalFacts(native, readState), { state, binding: null })
    await assert.rejects(observeNativeApprovalBinding(native, readState))
  }
  for (const basis_state of ['', 'unknown']) await assert.rejects(readNativeApprovalFacts(native, async (...args) => ({ ...await read(...args), basis_state })))
  await assert.rejects(readNativeApprovalFacts(native, async () => { throw new Error('dependency unavailable') }), /dependency unavailable/)
})

const filePath = (name = 'Model.3DS') => ({ engine_id: 33, version: 'catalog.path/v1', segments: [
  { term: 'root', kind: 'root', name: '' }, { term: 'directory', kind: 'directory', name: 'models' }, { term: 'file', kind: 'file', name }
] })
const fileFacts = candidate => ({ schema_version: 'meta.data_item_authorization_facts/v1', engine_id: 33, item_type: 'file',
  item_fingerprint: candidate, basis_state: 'complete', boundary_hash: 'c'.repeat(64), boundary: { layout: 'single', file_composition: { format: '3ds', refs: [
    { target: filePath(), role: 'model', required: true, primary: true },
    { target: filePath('TEST.PNG'), role: 'texture', required: true, primary: false }
  ] } } })
test('file approval uses the exact primary fingerprint and freezes the full composition without submitting refs', async () => {
  const target = filePath()
  const candidate = Buffer.from(await crypto.subtle.digest('SHA-256', new TextEncoder().encode('33:models/Model.3DS'))).toString('hex')
  let calls = 0
  const binding = await observeNativeApprovalBinding(target, async (id, fp) => { calls++; assert.equal(id, '33'); assert.equal(fp, candidate); return fileFacts(fp) })
  assert.equal(calls, 1)
  assert.equal(binding.native_target, undefined)
  assert.equal(binding.file_composition.refs.length, 2)
  assert.equal(Object.isFrozen(binding.file_composition.refs[1].target.segments), true)
  assert.equal(approvedBoundaryFields(target, binding).expected_boundary_hash, 'c'.repeat(64))
  const command = JSON.parse(serializeEngineApprovalInitialization({ ...target, engine_id: '33' }, 'independent', 'Explicit model', binding))
  assert.equal(command.item_fingerprint, candidate)
  assert.equal('file_composition' in command, false)
  assert.throws(() => serializeEngineApprovalInitialization({ ...target, engine_id: '33' }, 'catalog', 'Unsupported file business mode', binding))
  const textureFirst = await observeNativeApprovalBinding(target, async (_, fp) => { const facts = fileFacts(fp); facts.boundary.file_composition.refs.reverse(); return facts })
  assert.deepEqual(approvedBoundaryFields(target, textureFirst), approvedBoundaryFields(target, binding))
})
test('file candidates reject unrelated refs, ambiguous or incomplete composition and native-only old bindings', async () => {
  const changes = [f => { f.boundary.native_target = filePath() }, f => { f.boundary.layout = 'multi' },
    f => { f.engine_id = 34 }, f => { f.boundary.file_composition.format = 'obj' },
    f => { f.boundary.file_composition.refs[0].primary = false }, f => { f.boundary.file_composition.refs[1].primary = true },
    f => { f.boundary.file_composition.refs[0].required = false }, f => { delete f.boundary.file_composition.refs[1].primary },
    f => { f.boundary.file_composition.refs[1].target.engine_id = 34 },
    f => { f.boundary.file_composition.refs[1].target.segments[1].name = 'outside' },
    f => { f.boundary.file_composition.refs[1].target.segments[2].name = '../TEST.PNG' },
    f => { f.boundary.file_composition.refs.push(f.boundary.file_composition.refs[0]) }]
  for (const change of changes) await assert.rejects(observeNativeApprovalBinding(filePath(), async (_, fp) => { const f = fileFacts(fp); change(f); return f }))
  await assert.rejects(observeNativeApprovalBinding(filePath('TEST.PNG'), async (_, fp) => fileFacts(fp)))
  assert.throws(() => approvedBoundaryFields(filePath(), { item_fingerprint: 'a'.repeat(64), boundary_hash: 'b'.repeat(64), native_target: filePath() }))
  for (const state of ['incomplete', 'needs_refresh', 'unsupported']) assert.deepEqual(await readNativeApprovalFacts(filePath(), async (_, fp) => ({ ...fileFacts(fp), basis_state: state, boundary: null, boundary_hash: null })), { state, binding: null })
})

test('DAE approval binds the full model and texture set and rejects an incomplete boundary', async () => {
  const target = filePath('scene.dae')
  const makeFacts = fp => { const facts = fileFacts(fp); facts.boundary.file_composition.format = 'dae'; facts.boundary.file_composition.refs[0].target = target; return facts }
  const binding = await observeNativeApprovalBinding(target, async (_, fp) => makeFacts(fp))
  assert.equal(binding.file_composition.format, 'dae')
  assert.equal(binding.file_composition.refs.length, 2)
  assert.equal(JSON.parse(serializeEngineApprovalInitialization({ ...target, engine_id: '33' }, 'independent', 'DAE exact composition', binding)).expected_boundary_hash, 'c'.repeat(64))
  await assert.rejects(observeNativeApprovalBinding(target, async (_, fp) => { const facts = makeFacts(fp); facts.boundary.file_composition.refs[1].target = filePath('../outside.PNG'); return facts }))
  await assert.rejects(observeNativeApprovalBinding(target, async (_, fp) => { const facts = makeFacts(fp); facts.boundary.file_composition.format = 'obj'; return facts }))
})

const objectPath = (name = 'Model.3DS', bucket = 'models') => ({ engine_id: 33, version: 'catalog.path/v1', segments: [
  {term:'service',kind:'service',name:''}, {term:'bucket',kind:'bucket',name:bucket},
  {term:'prefix',kind:'prefix',name:'scene'}, {term:'object',kind:'object',name}
] })
const objectFacts = (candidate, format='3ds') => ({...fileFacts(candidate),item_type:'object',boundary:{layout:'single',file_composition:{format,refs:[
  {target:objectPath('model.'+format),role:'model',required:true,primary:true},
  ...(format === 'obj' ? [{target:objectPath('model.mtl'),role:'material_library',required:true,primary:false}] : []),
  ...(format === 'ifc' ? [] : [{target:objectPath(' tex.png   '),role:'texture',required:true,primary:false}])
]}}})
test('object model binding preserves bucket, prefix and names and never accepts wider or mixed targets', async () => {
  for (const format of ['ifc','3ds','dae','obj']) {
    const target = objectPath('model.'+format)
    const candidate = Buffer.from(await crypto.subtle.digest('SHA-256',new TextEncoder().encode(`33:models/scene/model.${format}`))).toString('hex')
    const binding = await observeNativeApprovalBinding(target, async (_,fp) => {assert.equal(fp,candidate);return objectFacts(fp,format)})
    assert.equal(binding.file_composition.format,format)
    assert.equal(approvedBoundaryFields(target,binding).item_fingerprint,candidate)
    assert.throws(() => serializeEngineApprovalInitialization({...target,engine_id:'33'},'catalog','no bucket approval',binding))
  }
  const mutations = [
    f => {f.item_type='file'},
    f => {f.boundary.file_composition.refs[1].target=objectPath('tex.png','other')},
    f => {f.boundary.file_composition.refs[1].target=filePath('tex.png')},
    f => {f.boundary.file_composition.refs[1].target.segments[2].name='outside'},
    f => {f.boundary.file_composition.refs[1].target.segments.pop()},
    f => {f.boundary.file_composition.refs[1].target.segments[3].name='../tex.png'}
  ]
  for (const format of ['3ds','dae']) for (const change of mutations) await assert.rejects(observeNativeApprovalBinding(objectPath('model.'+format), async (_,fp) => {const f=objectFacts(fp,format);change(f);return f}))
  const bucket = objectPath();bucket.segments.splice(2)
  await assert.rejects(readNativeApprovalFacts(bucket,()=>{throw new Error('bucket must not reach facts')}))
})

test('STL approval binds one exact required primary for FTP, NFS and MinIO and rejects added constituents', async () => {
  for (const [source, target] of [
    ['ftp', filePath('model.stl')],
    ['nfs', { ...filePath('model.stl'), engine_id: 14 }],
    ['minio', { ...objectPath('model.stl'), engine_id: 12 }]
  ]) {
    const candidate = Buffer.from(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(
      `${target.engine_id}:${target.segments.slice(1).map(part => part.name).join('/')}`))).toString('hex')
    const facts = () => ({ schema_version: 'meta.data_item_authorization_facts/v1', engine_id: target.engine_id,
      item_type: target.segments.at(-1).term, item_fingerprint: candidate, basis_state: 'complete',
      boundary_hash: 'd'.repeat(64), boundary: { layout: 'single', file_composition: { format: 'stl', refs: [
        { target: structuredClone(target), role: 'model', required: true, primary: true }
      ] } } })
    const binding = await observeNativeApprovalBinding(target, async (id, fp) => {
      assert.equal(id, String(target.engine_id), source); assert.equal(fp, candidate, source); return facts()
    })
    assert.equal(Object.isFrozen(binding.file_composition.refs[0].target.segments), true)
    const command = JSON.parse(serializeEngineApprovalInitialization({ ...target, engine_id: String(target.engine_id) },
      'independent', `${source} STL acceptance`, binding))
    assert.equal(command.item_fingerprint, candidate)
    assert.equal(command.expected_boundary_hash, 'd'.repeat(64))
    assert.equal('file_composition' in command, false)
    const mutations = [
      f => { const extra = structuredClone(f.boundary.file_composition.refs[0]); extra.primary = false; extra.role = 'texture'; extra.target.segments.at(-1).name = 'image.png'; f.boundary.file_composition.refs.push(extra) },
      f => { f.boundary.file_composition.refs[0].required = false },
      f => { f.boundary.file_composition.refs[0].primary = false },
      f => { f.boundary.file_composition.refs[0].target.engine_id = 99 },
      f => { f.boundary.file_composition.refs[0].target.segments.at(-1).name = 'other.stl' },
      f => { f.boundary.file_composition.format = 'fbx' }
    ]
    for (const mutate of mutations) await assert.rejects(observeNativeApprovalBinding(target, async () => {
      const changed = facts(); mutate(changed); return changed
    }), /invalidDataItemBoundary/, source)
    await assert.rejects(observeNativeApprovalBinding(target, async () => ({ ...facts(), basis_state: 'incomplete',
      boundary: null, boundary_hash: null })), /invalidDataItemBoundary/, source)
  }
})

test('GLB approval binds one exact required primary for FTP, NFS and MinIO and rejects added constituents', async () => {
  for (const [source, target] of [
    ['ftp', filePath('model.glb')],
    ['nfs', { ...filePath('model.glb'), engine_id: 14 }],
    ['minio', { ...objectPath('model.glb'), engine_id: 12 }]
  ]) {
    const candidate = Buffer.from(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(
      `${target.engine_id}:${target.segments.slice(1).map(part => part.name).join('/')}`))).toString('hex')
    const facts = () => ({ schema_version: 'meta.data_item_authorization_facts/v1', engine_id: target.engine_id,
      item_type: target.segments.at(-1).term, item_fingerprint: candidate, basis_state: 'complete',
      boundary_hash: 'd'.repeat(64), boundary: { layout: 'single', file_composition: { format: 'glb', refs: [
        { target: structuredClone(target), role: 'model', required: true, primary: true }
      ] } } })
    const binding = await observeNativeApprovalBinding(target, async (id, fp) => {
      assert.equal(id, String(target.engine_id), source); assert.equal(fp, candidate, source); return facts()
    })
    assert.equal(Object.isFrozen(binding.file_composition.refs[0].target.segments), true)
    const command = JSON.parse(serializeEngineApprovalInitialization({ ...target, engine_id: String(target.engine_id) },
      'independent', `${source} GLB acceptance`, binding))
    assert.equal(command.item_fingerprint, candidate)
    assert.equal(command.expected_boundary_hash, 'd'.repeat(64))
    assert.equal('file_composition' in command, false)
    const mutations = [
      f => { const extra = structuredClone(f.boundary.file_composition.refs[0]); extra.primary = false; extra.role = 'texture'; extra.target.segments.at(-1).name = 'image.png'; f.boundary.file_composition.refs.push(extra) },
      f => { f.boundary.file_composition.refs[0].required = false },
      f => { f.boundary.file_composition.refs[0].primary = false },
      f => { f.boundary.file_composition.refs[0].target.engine_id = 99 },
      f => { f.boundary.file_composition.refs[0].target.segments.at(-1).name = 'other.glb' },
      f => { f.boundary.file_composition.format = 'fbx' }
    ]
    for (const mutate of mutations) await assert.rejects(observeNativeApprovalBinding(target, async () => {
      const changed = facts(); mutate(changed); return changed
    }), /invalidDataItemBoundary/, source)
    await assert.rejects(observeNativeApprovalBinding(target, async () => ({ ...facts(), basis_state: 'incomplete',
      boundary: null, boundary_hash: null })), /invalidDataItemBoundary/, source)
  }
})


test('FBX approval binds one exact required primary for FTP, NFS and MinIO and rejects added constituents', async () => {
  for (const [source, target] of [
    ['ftp', filePath('model.fbx')],
    ['nfs', { ...filePath('model.fbx'), engine_id: 14 }],
    ['minio', { ...objectPath('model.fbx'), engine_id: 12 }]
  ]) {
    const candidate = Buffer.from(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(
      `${target.engine_id}:${target.segments.slice(1).map(part => part.name).join('/')}`))).toString('hex')
    const facts = () => ({ schema_version: 'meta.data_item_authorization_facts/v1', engine_id: target.engine_id,
      item_type: target.segments.at(-1).term, item_fingerprint: candidate, basis_state: 'complete',
      boundary_hash: 'd'.repeat(64), boundary: { layout: 'single', file_composition: { format: 'fbx', refs: [
        { target: structuredClone(target), role: 'model', required: true, primary: true }
      ] } } })
    const binding = await observeNativeApprovalBinding(target, async (id, fp) => {
      assert.equal(id, String(target.engine_id), source); assert.equal(fp, candidate, source); return facts()
    })
    assert.equal(Object.isFrozen(binding.file_composition.refs[0].target.segments), true)
    const command = JSON.parse(serializeEngineApprovalInitialization({ ...target, engine_id: String(target.engine_id) },
      'independent', `${source} FBX acceptance`, binding))
    assert.equal(command.item_fingerprint, candidate)
    assert.equal(command.expected_boundary_hash, 'd'.repeat(64))
    assert.equal('file_composition' in command, false)
    const mutations = [
      f => { const extra = structuredClone(f.boundary.file_composition.refs[0]); extra.primary = false; extra.role = 'texture'; extra.target.segments.at(-1).name = 'image.png'; f.boundary.file_composition.refs.push(extra) },
      f => { f.boundary.file_composition.refs[0].required = false },
      f => { f.boundary.file_composition.refs[0].primary = false },
      f => { f.boundary.file_composition.refs[0].target.engine_id = 99 },
      f => { f.boundary.file_composition.refs[0].target.segments.at(-1).name = 'other.fbx' },
      f => { f.boundary.file_composition.format = 'skp' }
    ]
    for (const mutate of mutations) await assert.rejects(observeNativeApprovalBinding(target, async () => {
      const changed = facts(); mutate(changed); return changed
    }), /invalidDataItemBoundary/, source)
    await assert.rejects(observeNativeApprovalBinding(target, async () => ({ ...facts(), basis_state: 'incomplete',
      boundary: null, boundary_hash: null })), /invalidDataItemBoundary/, source)
  }
})


test('glTF multi approval retains manifest, buffer and image; rejects false layouts and partial roles', async () => {
  for (const target of [filePath(' 原名 %.gltf'), objectPath(' 原名 %.gltf')]) {
    const fullName = target.segments.slice(1).map(part => part.name).join('/')
    const fingerprint = Buffer.from(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(`${target.engine_id}:${fullName}`))).toString('hex')
    const facts = () => {
      const image=structuredClone(target);image.segments.at(-1).name='literal%20.png'
      return {schema_version:'meta.data_item_authorization_facts/v1',engine_id:target.engine_id,item_type:target.segments.at(-1).term,item_fingerprint:fingerprint,basis_state:'complete',boundary_hash:'d'.repeat(64),boundary:{layout:'multi',file_composition:{format:'gltf',refs:[
        {target:structuredClone(target),role:'manifest',primary:true,required:true},
        {target:image,role:'image',primary:false,required:true}
      ]}}}
    }
    const binding=await observeNativeApprovalBinding(target,async()=>facts())
    assert.equal(binding.file_composition.refs[0].role,'manifest')
    assert.equal(binding.file_composition.refs[1].target.segments.at(-1).name,'literal%20.png')
    for(const mutate of [f=>{f.boundary.layout='single'},f=>{f.boundary.layout='whole'},f=>{f.boundary.file_composition.refs[0].role='model'},f=>{f.boundary.file_composition.refs[1].role='texture'},f=>{f.boundary.file_composition.refs[1].required=false}]) {
      await assert.rejects(observeNativeApprovalBinding(target,async()=>{const f=facts();mutate(f);return f}),/invalidDataItemBoundary/)
    }
    const embedded=facts();embedded.boundary.file_composition.refs.length=1
    assert.equal((await observeNativeApprovalBinding(target,async()=>embedded)).file_composition.refs.length,1)
  }
})
