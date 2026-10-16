// Display preference only: available types always come from the plugin catalog.
// Listed databases follow DB-Engines, October 2026: https://db-engines.com/en/ranking
// PostgreSQL and Oracle positions are swapped by user preference.
// Unlisted domestic products and non-database stores use editorial defaults.
const popularEngineTypes = [
  'postgresql', 'mysql', 'oracle', 'clickhouse', 'tidb', 'oceanbase',
  'opengauss', 'kingbase', 'doris', 'dameng', 'highgo',
  'spark', 'mongodb', 'elasticsearch', 'redis', 'neo4j',
  's3', 'minio', 'hdfs', 'nfs', 'kafka'
]
const displayRank = new Map(popularEngineTypes.map((type, index) => [type, index]))

export function compareEngineTypePopularity(left, right) {
  const rank = type => displayRank.get(type) ?? Number.MAX_SAFE_INTEGER
  return rank(left.value) - rank(right.value) ||
    left.label.localeCompare(right.label, 'en', { sensitivity: 'base' }) ||
    left.value.localeCompare(right.value, 'en')
}
