import { describe, expect, it } from 'vitest'
import { compareEngineTypePopularity } from '../src/utils/engineTypeOrdering'

const option = (value, label = value) => ({ value, label })
const sortedTypes = options => [...options].sort(compareEngineTypePopularity).map(item => item.value)

describe('engine registration display order', () => {
  it('places popular relational databases ahead of less common choices', () => {
    expect(sortedTypes(['highgo', 'clickhouse', 'dameng', 'postgresql', 'mysql', 'doris', 'oracle', 'tidb', 'kingbase', 'oceanbase', 'opengauss'].map(value => option(value)))).toEqual([
      'postgresql', 'mysql', 'oracle', 'clickhouse', 'tidb', 'oceanbase', 'opengauss', 'kingbase', 'doris', 'dameng', 'highgo'
    ])
  })

  it('orders document and storage alternatives consistently', () => {
    expect(sortedTypes(['elasticsearch', 'mongodb'].map(value => option(value)))).toEqual(['mongodb', 'elasticsearch'])
    expect(sortedTypes(['minio', 's3'].map(value => option(value)))).toEqual(['s3', 'minio'])
    expect(sortedTypes(['nfs', 'hdfs'].map(value => option(value)))).toEqual(['hdfs', 'nfs'])
  })

  it('retains new plugin types after ranked choices and resolves ties deterministically', () => {
    const options = [option('zeta', 'Zeta database'), option('new_b', 'New database'), option('oracle', 'Oracle'), option('new_a', 'New database')]
    expect(sortedTypes(options)).toEqual(['oracle', 'new_a', 'new_b', 'zeta'])
    expect(options[0].value).toBe('zeta')
  })
})
