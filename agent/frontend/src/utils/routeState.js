function sessionLocation(sessionId) {
  return {
    name: 'ChatSession',
    params: { session_id: String(sessionId) }
  }
}

export function resolveAgentSessionRouteState(sessions, requestedSessionId) {
  const sessionId = requestedSessionId == null ? '' : String(requestedSessionId)
  if (!sessionId) {
    const firstSession = sessions[0]
    return firstSession
      ? { kind: 'redirect', location: sessionLocation(firstSession.id) }
      : { kind: 'empty' }
  }

  return sessions.some(session => String(session.id) === sessionId)
    ? { kind: 'ready', sessionId }
    : { kind: 'unavailable', sessionId }
}

export function routeAfterSessionDeletion(sessions) {
  return sessions[0] ? sessionLocation(sessions[0].id) : { name: 'Chat' }
}
