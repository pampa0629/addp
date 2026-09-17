import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'
import { buildBusinessDomainOptions, businessDomainReferenceOptions, matchesBusinessDomain } from '../src/utils/businessDomainOptions.mjs'

test('domain options preserve parent-first order, identities, depths and full paths without changing the tree', () => {
  const tree = [{ id: 9, name: '户外域' }, { id: 1, name: '客户域', children: [{ id: 2, name: 'VIP', code: 'customer_vip', children: [{ id: 3, name: '国内' }] }] }]
  const original = structuredClone(tree)
  const options = buildBusinessDomainOptions(tree)
  assert.deepEqual(options.map(({ id, depth }) => [id, depth]), [[9, 0], [1, 0], [2, 1], [3, 2]])
  assert.deepEqual(options[3].path, ['客户域', 'VIP', '国内'])
  assert.equal(options[2].name, 'VIP')
  assert.deepEqual(tree, original)
  assert.deepEqual(buildBusinessDomainOptions(), [])
})

test('search matches code and ancestor paths including whitespace and case', () => {
  const [parent, child] = buildBusinessDomainOptions([{ id: 1, name: '客户', children: [{ id: 2, name: 'VIP', code: 'customer_vip' }] }])
  assert.equal(matchesBusinessDomain(child, ' 客户 / vip '), true)
  assert.equal(matchesBusinessDomain(child, 'CUSTOMER_VIP'), true)
  assert.equal(matchesBusinessDomain(parent, 'VIP'), false)
  assert.equal(matchesBusinessDomain(child, '户外'), false)
})

test('a remote page can contain a child without its parent and retains its full depth', () => {
  const [child, unresolved] = businessDomainReferenceOptions([
    { id: '42', name: 'VIP', domain_path: ['客户', '海外', 'VIP'] },
    { id: '99', name: '引用不可用' }
  ])
  assert.equal(child.depth, 2)
  assert.deepEqual(child.path, ['客户', '海外', 'VIP'])
  assert.equal(child.id, '42')
  assert.deepEqual(unresolved.path, [])
})

const root = resolve(import.meta.dirname, '../../..')
function vueSources(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = resolve(directory, entry.name)
    return entry.isDirectory() ? vueSources(path) : entry.name.endsWith('.vue') ? [path] : []
  })
}
test('business domain options have one renderer across consuming modules', () => {
  let consumers = 0
  for (const module of ['standard', 'model', 'quality', 'catalog']) {
    for (const file of vueSources(resolve(root, module, 'frontend/src'))) {
      const source = readFileSync(file, 'utf8')
      assert.doesNotMatch(source, /<el-option\b[^>]*v-for="[^"\n]*\bin (?:domains|domainList|domainOptions|candidateState\.domain\.options)"/, file)
      assert.doesNotMatch(source, /const flattenDomains\s*=|const flatten\s*=\s*nodes\s*=>/, file)
      if (source.includes('<BusinessDomainSelect')) consumers++
    }
  }
  assert.equal(consumers, 19)
})
