export function resolveRatingAction({ hasOwnRating, canCreate, canUpdate, knownAccessStatus }) {
  if (knownAccessStatus && knownAccessStatus !== 'effective') return null
  if (hasOwnRating) return canUpdate ? 'update' : null
  return canCreate ? 'create' : null
}
