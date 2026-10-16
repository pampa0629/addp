import { isTargetUUID } from './monitoringTargets.js'

// Resolve only this authorized page, without caching identities across requests.
export async function loadHostReferences(nodeIDs, read, current) {
  const ids = [...new Set(nodeIDs.filter(isTargetUUID))], hosts = {}
  for (let i = 0; i < ids.length; i += 4) {
    if (!current()) return {}
    const batch = ids.slice(i, i + 4)
    const results = await Promise.allSettled(batch.map(read))
    if (!current()) return {}
    results.forEach((result, index) => {
      const host = result.status === 'fulfilled' ? result.value : null
      if (host?.node_id === batch[index] && typeof host.display_name === 'string' && host.display_name.trim()) hosts[host.node_id] = host
    })
  }
  return hosts
}
