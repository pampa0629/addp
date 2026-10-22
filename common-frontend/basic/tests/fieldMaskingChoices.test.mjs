import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import { currentMaskingChoice, isCurrentMaskingChoice, setMaskingChoice } from '../src/utils/fieldMaskingChoices.mjs'
const field = { target: { owner_module: 'meta', resource_type: 'data_item', resource_identity: 'one' }, component_key: 'phone', policy_id: 'policy', policy_revision: 2, assessment_revision: 3, options: [{ id: 'hash', name: '统计哈希' }, { id: 'mask', name: '掩码' }] }
test('explicit choices preserve unrelated sources and never silently update a revision', () => {
 const other = { ...field, target: { ...field.target, resource_identity: 'two' } }
 const first = setMaskingChoice([field, other], [], other, 'mask')
 const chosen = setMaskingChoice([field, other], first, field, 'hash')
 assert.equal(chosen.length, 2)
 assert.equal(currentMaskingChoice(field, chosen).selection.option_id, 'hash')
 assert.equal(isCurrentMaskingChoice(field, currentMaskingChoice(field, chosen)), true)
 assert.equal(isCurrentMaskingChoice({ ...field, policy_revision: 4 }, currentMaskingChoice(field, chosen)), false)
 assert.deepEqual(setMaskingChoice([field, other], chosen, field, ''), first)
 assert.equal(chosen.length, 2)
 assert.throws(() => setMaskingChoice([field], chosen, field, 'not-an-option'))
 const component = fs.readFileSync(new URL('../src/components/FieldMaskingChoices.vue', import.meta.url), 'utf8')
 assert.ok(component.includes('isCurrentMaskingChoice'))
 assert.ok(component.includes('orphans'))
 assert.ok(!component.includes('authStore'))
})
