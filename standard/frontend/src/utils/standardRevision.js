// Management display keeps history visible without changing effective revision resolution.
export const getStandardDisplayRevision = aggregate => aggregate?.draft_revision || aggregate?.current_revision || aggregate?.latest_revision

// An explicit historical reference must never fall back to the management draft.
export async function getStandardRouteRevision(aggregate, requestedID, readRevision) {
  if (requestedID === undefined) return getStandardDisplayRevision(aggregate)
  if (typeof requestedID !== 'string' || !/^[1-9]\d*$/.test(requestedID)) throw new Error('Invalid standard revision identity')
  const revision = await readRevision(requestedID)
  if (!revision || String(revision.id) !== requestedID) throw new Error('Requested standard revision is unavailable')
  return revision
}
