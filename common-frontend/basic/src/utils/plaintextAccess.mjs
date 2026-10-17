export const plaintextAccessScopes=Object.freeze([
  Object.freeze({consumer_owner:'manager',action:'preview'}),
  Object.freeze({consumer_owner:'develop',action:'query'})
])

export function plaintextRequestPayload({assessmentId,owners,expiresAt,rationale,maximum},now=Date.now()) {
  const deadline=new Date(expiresAt).getTime()
  if(!assessmentId || !Array.isArray(owners) || !owners.length || owners.length>2 || new Set(owners).size!==owners.length || !Number.isFinite(deadline) || deadline<=now || deadline>maximum || deadline>now+30*24*60*60*1000 || !String(rationale||'').trim())throw new Error('Invalid plaintext access request')
  const scopes=owners.map(owner=>plaintextAccessScopes.find(scope=>scope.consumer_owner===owner))
  if(scopes.some(scope=>!scope))throw new Error('Unsupported plaintext outlet')
  return {assessment_id:assessmentId,scopes,requested_expires_at:new Date(deadline).toISOString(),rationale:String(rationale).trim()}
}
