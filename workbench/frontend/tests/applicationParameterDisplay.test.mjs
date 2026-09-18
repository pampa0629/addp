import test from 'node:test'
import assert from 'node:assert/strict'
import { buildParameterDisplayQuery, parameterDisplayFields, parameterDisplayResult, assertParameterDisplaySources } from '../src/utils/applicationParameterDisplay.mjs'
import { createParameterDisplayResolver } from '../src/utils/parameterDisplayResolver.mjs'

function fixture() {
  const snapshot = {
    components: [{ id: 'directory', contract_fingerprint: 'current', query_template: {
      select: ['key', 'name'], fixed_filter: { field: 'active', op: 'eq', value: true },
      parameter_filters: [{ parameter_key: 'search', field: 'name', operator: 'contains' }],
      named_parameter_bindings: [{ parameter_key: 'scope', name: 'scope' }],
      order_by: [{ field: 'key', direction: 'asc' }], page_limit: 50, format: 'json',
    } }],
    parameters: [{ key: 'person', display_source: { source_component_id: 'directory', label_field: 'name' } }, { key: 'search' }, { key: 'scope', required: true }],
    parameter_bindings: [{ component_id: 'directory', component_parameter_key: 'search', application_parameter_key: 'search' }, { component_id: 'directory', component_parameter_key: 'scope', application_parameter_key: 'scope' }],
    selection_bindings: [{ source_component_id: 'directory', assignments: [{ source_field: 'key', application_parameter_key: 'person' }] }],
  }
  const descriptors = { directory: { contract_fingerprint: 'current', operations: [{ key: 'query', method: 'POST', input_kind: 'structured_query', path: '/api/query/directory/query' }], input_contract: { fields: [
    { name: 'key', type: 'string', selectable: true, filterable: true, operators: ['eq'] }, { name: 'name', type: 'string', selectable: true },
  ], page: { max_limit: 100 } }, output_contract: { fields: [{ name: 'key', type: 'string' }, { name: 'name', type: 'string' }] } } }
  return { snapshot, descriptors }
}

test('name request uses declared identity, keeps fixed scope and named parameters, ignores search and resets pagination', () => {
  const { snapshot, descriptors } = fixture()
  const original = structuredClone(snapshot)
  const query = buildParameterDisplayQuery(snapshot, descriptors, 'person', 'outside-current-page', { search: 'unrelated name', scope: 'visible' })
  assert.deepEqual(query.body, { select: ['key', 'name'], parameters: { scope: 'visible' }, filter: { and: [{ field: 'active', op: 'eq', value: true }, { field: 'key', op: 'eq', value: 'outside-current-page' }] }, order_by: [{ field: 'key', direction: 'asc' }], page: { limit: 2, cursor: '' }, format: 'json' })
  assert.deepEqual(snapshot, original)
  assert.throws(() => buildParameterDisplayQuery(snapshot, descriptors, 'person', 'a', {}), /missing required/)
  assert.equal(buildParameterDisplayQuery(snapshot, descriptors, 'person', '', {}), null)
})

test('unavailable contracts, missing assignments, unselected names and non-equality keys cannot become label sources', () => {
  for (const mutate of [
    ({ descriptors }) => { descriptors.directory.contract_fingerprint = 'changed' },
    ({ snapshot }) => { snapshot.selection_bindings = [] },
    ({ snapshot }) => { snapshot.components[0].query_template.select = ['key'] },
    ({ descriptors }) => { descriptors.directory.input_contract.fields[0].operators = ['contains'] },
    ({ descriptors }) => { descriptors.directory.output_contract.fields[1].type = 'int' },
  ]) {
    const data = fixture(); mutate(data)
    assert.throws(() => assertParameterDisplaySources(data.snapshot, data.descriptors))
  }
  const { snapshot, descriptors } = fixture()
  assert.deepEqual(parameterDisplayFields(snapshot, descriptors, 'person', 'directory').map(f => f.name), ['key', 'name'])
  delete snapshot.parameters[0].display_source
  assert.equal(buildParameterDisplayQuery(snapshot, descriptors, 'person', 'a', {}), null)
})

test('only a complete unique exact match provides a name, including typed zero and false values', () => {
  for (const value of ['a', 0, false]) {
    const query = { valueField: 'key', labelField: 'name', value }
    const response = (rows, more = false) => ({ data: rows, page: { has_more: more } })
    assert.deepEqual(parameterDisplayResult(query, response([{ key: value, name: ' Latest name ' }])), { status: 'resolved', label: 'Latest name' })
    assert.equal(parameterDisplayResult(query, response([])).status, 'missing')
    assert.equal(parameterDisplayResult(query, response([{ key: value, name: null }])).status, 'unnamed')
    assert.equal(parameterDisplayResult(query, response([{ key: value, name: 'A' }], true)).status, 'ambiguous')
    assert.equal(parameterDisplayResult(query, response([{ key: value, name: 'A' }, { key: value, name: 'A' }])).status, 'ambiguous')
    assert.equal(parameterDisplayResult(query, response([{ key: 'different', name: 'A' }])).status, 'failed')
    assert.equal(parameterDisplayResult(query, { data: [{ key: value, name: 'A' }] }).status, 'failed')
  }
})

test('latest name wins; old failures and responses after clearing or closing never restore stale labels', async () => {
  const pending = [], states = []
  const resolver = createParameterDisplayResolver(() => new Promise((resolve, reject) => pending.push({ resolve, reject })), state => states.push(state))
  const query = value => ({ value, valueField: 'key', labelField: 'name' })
  const response = value => ({ data: { data: [{ key: value, name: value }], page: { has_more: false } } })
  const first = resolver.resolve(query('first')), second = resolver.resolve(query('second'))
  pending[1].resolve(response('second')); await second
  pending[0].reject(new Error('late')); await first
  assert.deepEqual(states.at(-1), { status: 'resolved', label: 'second' })
  const third = resolver.resolve(query('third'))
  await resolver.resolve(null, 'empty')
  pending[2].resolve(response('third')); await third
  assert.deepEqual(states.at(-1), { status: 'empty', label: '' })
  const fourth = resolver.resolve(query('fourth'))
  resolver.invalidate(); const count = states.length
  pending[3].resolve(response('fourth')); await fourth
  assert.equal(states.length, count)
})
