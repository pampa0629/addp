// Validate the collection projection shared by the configuration and observation pages.
export function validateEngineCollection(row, owner, queried) {
  const subject = row?.subject
  if (subject?.kind !== 'engine_deployment' || subject.deployment_id !== owner.deployment_id ||
      subject.engine_type !== owner.engine_type || subject.display_name !== owner.display_name ||
      row.node_id !== owner.node_id || !Number.isFinite(queried)) {
    throw new Error('invalid_resource_response')
  }
  const collection = row.collection
  const state = collection?.state
  if (['collecting', 'failed', 'stale', 'snapshot_unavailable'].includes(state)) {
    const sampled = Date.parse(collection.sampled_at)
    if (Number.isFinite(sampled) && sampled <= queried && (state === 'stale' ? queried - sampled > 60000 : queried - sampled <= 60000)) return collection
  } else if (['no_sample', 'not_active', 'not_connected', 'source_unavailable'].includes(state) && collection.sampled_at === null) {
    return collection
  }
  throw new Error('invalid_resource_response')
}
