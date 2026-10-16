import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { engineTypeIcon } from '../src/utils/engineTypeIcons.mjs'

test('product glyphs distinguish engine types including compute engines', () => {
  const types = ['clickhouse', 'doris', 'kafka', 'spark', 'elasticsearch', 'hdfs', 'minio', 'mongodb', 'mysql', 'neo4j', 'postgresql', 'redis', 'tidb']
  const paths = types.map(type => {
    const icon = engineTypeIcon(type)
    assert.equal(icon.type, type)
    assert.ok(icon.path.startsWith('M'), `${type} must have its product glyph`)
    assert.equal(icon.initials, '')
    return icon.path
  })
  assert.equal(new Set(paths).size, types.length)
  assert.deepEqual(engineTypeIcon(' PostgreSQL '), engineTypeIcon('postgresql'))
})

test('engines without bundled glyphs retain distinct product-specific initials', () => {
  const marks = { dameng: 'DM', highgo: 'HG', kingbase: 'KB', nfs: 'NFS', oceanbase: 'OB', opengauss: 'OG', oracle: 'OR', s3: 'S3' }
  for (const [type, initials] of Object.entries(marks)) {
    assert.deepEqual(engineTypeIcon(type), { type, path: '', initials })
  }
  assert.equal(engineTypeIcon('new_vendor').initials, 'NE')
  assert.equal(engineTypeIcon('other_vendor').initials, 'OT')
})

test('registration uses the sole shared product icon renderer', () => {
  const owner = readFileSync(new URL('../src/components/EngineTypeIcon.vue', import.meta.url), 'utf8')
  const registration = readFileSync(new URL('../../../system/frontend/src/views/Engines.vue', import.meta.url), 'utf8')
  assert.match(owner, /aria-hidden="true"/)
  assert.match(registration, /<EngineTypeIcon :type="item.value"/)
  assert.doesNotMatch(registration, /◈|class="engine-type-icon"/)
})
