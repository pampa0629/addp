import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import { computed, effectScope, nextTick, ref, watch } from 'vue'

const source = readFileSync(new URL('../src/views/TaskWizard/Step4Configure.vue', import.meta.url), 'utf8')
const block = source.slice(source.indexOf('const maskingPanels ='), source.indexOf('const formRef ='))
const deferred = () => {
  let resolve, reject
  const promise = new Promise((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const snapshot = value => JSON.parse(JSON.stringify(value))

// Execute the component's candidate-loading block with Vue's real watcher scheduling.
// The transport deliberately ignores cancellation and may finish in any order.
function harness(t) {
  const pending = [], scope = effectScope()
  const choice = { target: { resource_identity: 'first' }, component_key: 'phone', selection: { option_id: 'saved' } }
  const wizardState = {
    sourceProtectionChoices: ref([choice]), runtimeBoundary: ref('bounded'), loadMode: ref('snapshot'),
    taskConfig: ref({ config: { source: { locator: 'first', data_type: 'table', representation: 'native' } } })
  }
  const context = vm.createContext({
    ref, computed, watch, props: { wizardState }, t: key => key,
    taskAPI: { protectionOptions: locator => {
      const work = deferred(); pending.push({ locator, ...work }); return work.promise
    } }
  })
  const state = scope.run(() => vm.runInContext(`${block}\n;({ maskingPanels, maskingFields, maskingLoading, maskingError, refreshMasking })`, context))
  t.after(() => scope.stop())
  return { ...state, pending, wizardState, source: wizardState.taskConfig.value.config.source, choice }
}
const settle = async () => { await nextTick(); await Promise.resolve(); await nextTick() }

for (const outcome of ['success', 'failure']) {
  test(`source switch ignores obsolete ${outcome} after the current candidates arrive`, async t => {
    const h = harness(t)
    assert.equal(h.pending[0].locator, 'first')
    h.source.locator = 'second'; await settle()
    assert.equal(h.pending[1].locator, 'second')
    h.pending[1].resolve({ data: { fields: [{ component_key: 'current-phone' }] } }); await settle()
    if (outcome === 'success') h.pending[0].resolve({ fields: [{ component_key: 'obsolete-phone' }] })
    else h.pending[0].reject({ response: { data: { error: 'obsolete refusal' } } })
    await settle()
    assert.deepEqual(snapshot(h.maskingFields.value), [{ component_key: 'current-phone' }])
    assert.equal(h.maskingError.value, '')
    assert.equal(h.maskingLoading.value, false)
    assert.deepEqual(snapshot(h.wizardState.sourceProtectionChoices.value), [h.choice])
  })
}

test('obsolete finally does not clear loading for the replacement source', async t => {
  const h = harness(t)
  h.source.locator = 'second'; await settle()
  h.pending[0].resolve({ fields: [{ component_key: 'obsolete-phone' }] }); await settle()
  assert.equal(h.maskingLoading.value, true)
  assert.deepEqual(snapshot(h.maskingFields.value), [])
  h.pending[1].resolve({ fields: [{ component_key: 'current-phone' }] }); await settle()
  assert.equal(h.maskingLoading.value, false)
})

test('refreshing the same source only accepts the latest request', async t => {
  const h = harness(t), current = h.refreshMasking()
  assert.deepEqual(h.pending.map(item => item.locator), ['first', 'first'])
  h.pending[1].resolve({ fields: [{ component_key: 'refreshed-phone' }] }); await current
  h.pending[0].reject(new Error('obsolete refresh error')); await settle()
  assert.deepEqual(snapshot(h.maskingFields.value), [{ component_key: 'refreshed-phone' }])
  assert.equal(h.maskingError.value, '')
})

for (const kind of ['mode', 'representation', 'locator']) {
  test(`unsupported ${kind} invalidates pending candidates and retains the saved choice`, async t => {
    const h = harness(t)
    if (kind === 'mode') h.wizardState.loadMode.value = 'incremental'
    if (kind === 'representation') h.source.representation = 'encoded'
    if (kind === 'locator') h.source.locator = ''
    await settle()
    assert.equal(h.pending.length, 1)
    assert.equal(h.maskingError.value, 'transfer.taskWizard.exportMaskingUnsupported')
    assert.equal(h.maskingLoading.value, false)
    h.pending[0].resolve({ fields: [{ component_key: 'obsolete-phone' }] }); await settle()
    assert.deepEqual(snapshot(h.maskingFields.value), [])
    assert.equal(h.maskingError.value, 'transfer.taskWizard.exportMaskingUnsupported')
    assert.deepEqual(snapshot(h.wizardState.sourceProtectionChoices.value), [h.choice])
  })
}

test('collapse invalidates an outstanding failure and reopening reads the current source', async t => {
  const h = harness(t)
  h.maskingPanels.value = []; await settle()
  h.source.locator = 'second'; await settle()
  assert.equal(h.maskingLoading.value, false)
  assert.equal(h.pending.length, 1)
  h.pending[0].reject(new Error('hidden obsolete error')); await settle()
  assert.equal(h.maskingError.value, '')
  h.maskingPanels.value = ['masking']; await settle()
  assert.equal(h.pending[1].locator, 'second')
  h.pending[1].resolve({ fields: [{ component_key: 'current-phone' }] }); await settle()
  assert.deepEqual(snapshot(h.maskingFields.value), [{ component_key: 'current-phone' }])
})

test('current protection refusal remains visible and a manual refresh recovers', async t => {
  const h = harness(t)
  h.pending[0].reject({ response: { data: { error: 'current protection refusal' } } }); await settle()
  assert.equal(h.maskingError.value, 'current protection refusal')
  assert.equal(h.maskingLoading.value, false)
  assert.deepEqual(snapshot(h.wizardState.sourceProtectionChoices.value), [h.choice])
  const work = h.refreshMasking()
  assert.equal(h.maskingError.value, '')
  h.pending[1].resolve({ fields: [] }); await work
  assert.equal(h.maskingLoading.value, false)
  assert.deepEqual(snapshot(h.maskingFields.value), [])
})
