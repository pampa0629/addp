export function captureDelegation(form, members, now = Date.now()) {
  const member = members.find(item => item.id === form.membershipID)
  const reason = typeof form.reason === 'string' ? form.reason.trim() : ''
  const expiresAt = form.expiresAt instanceof Date ? form.expiresAt.getTime() : NaN
  if (!member || member.principal_type !== 'user' || member.principal_status !== 'active' || member.status !== 'active' || member.ended_at ||
      !Number.isFinite(expiresAt) || expiresAt <= now || (member.expires_at && expiresAt > new Date(member.expires_at).getTime()) ||
      !reason || Array.from(reason).length > 2000) throw new Error('invalidDelegation')
  return Object.freeze({ tenant_membership_id: member.id, expires_at: new Date(expiresAt).toISOString(), reason })
}
