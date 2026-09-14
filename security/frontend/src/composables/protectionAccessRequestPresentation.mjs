export function createProtectionAccessRequestPresentation({ t, formatDateTime, releaseActorLabel }) {
  function accessRequestDecisionDialogView(request, decision) {
    const normalized = decision === 'reject' ? 'reject' : 'approve'
    const unavailable = t('security.common.notAvailable')
    const requesterName = String(request?.requester?.display_name || '').trim() || unavailable
    const targetName = String(request?.target_full_name || '').trim() || unavailable
    const componentKey = String(request?.component?.key || '').trim() || unavailable
    return {
      title: t(`security.accessRequest.${normalized}`),
      hint: t(`security.accessRequest.${normalized}Hint`),
      confirmLabel: t(`security.accessRequest.confirmActions.${normalized}`),
      confirmType: normalized === 'reject' ? 'danger' : 'primary',
      target: `${targetName} · ${componentKey}`,
      details: [
        {
          key: 'requester',
          label: t('security.accessRequest.requester'),
          value: `${requesterName}（${releaseActorLabel(request?.requester?.id)}）`
        },
        {
          key: 'requestedUntil',
          label: t('security.accessRequest.requestedUntil'),
          value: formatDateTime(request?.requested_expires_at)
        },
        {
          key: 'rationale',
          label: t('security.accessRequest.rationale'),
          value: request?.rationale || unavailable
        }
      ]
    }
  }

  function accessRequestReviewRowView(request, context = {}) {
    const unavailable = t('security.common.notAvailable')
    const requestState = String(request?.state || '')
    const authorizationState = String(request?.authorization_state || '')
    const unavailableReason = String(request?.decision_unavailable_reason || '')
    const actorView = actor => actor
      ? {
          displayName: String(actor.display_name || '').trim() || unavailable,
          idLabel: releaseActorLabel(actor.id)
        }
      : null
    return {
      id: request?.id,
      request,
      target: {
        fullName: request?.target_full_name,
        componentKey: request?.component?.key
      },
      requester: actorView(request?.requester) || { displayName: unavailable, idLabel: unavailable },
      requestedUntil: formatDateTime(request?.requested_expires_at),
      createdAt: formatDateTime(request?.created_at),
      rationale: request?.rationale,
      state: {
        type: requestState === 'approved' ? 'success' : requestState === 'rejected' ? 'danger' : 'info',
        label: t(`security.accessRequest.states.${requestState}`)
      },
      authorization: authorizationState
        ? {
            state: {
              type: authorizationState === 'active' ? 'success' : authorizationState === 'revoked' ? 'danger' : 'info',
              label: t(`security.accessRequest.authorizationStates.${authorizationState}`)
            },
            untilLabel: request?.authorized_until
              ? t('security.accessRequest.authorizedUntil', { time: formatDateTime(request.authorized_until) })
              : '',
            canOpen: Boolean(context.canReadExemptions && request?.enrollment_id && request?.exemption_id)
          }
        : null,
      authorizationFallback: t('security.accessRequest.authorizationStates.not_granted'),
      reviewer: actorView(request?.reviewer),
      reviewerFallback: unavailable,
      processedAt: formatDateTime(request?.decided_at || request?.requested_expires_at),
      decisionRationale: request?.decision_rationale || unavailable,
      actions: {
        canDecide: Boolean(request?.can_decide),
        unavailable: unavailableReason
          ? {
              label: t(`security.accessRequest.unavailableLabels.${unavailableReason}`),
              description: t(`security.accessRequest.unavailableReasons.${unavailableReason}`)
            }
          : null
      }
    }
  }

  function accessRequestReviewWorkspaceView(requests, context = {}) {
    const rows = Array.isArray(requests) ? requests : []
    return {
      rows: rows.map(request => accessRequestReviewRowView(request, context))
    }
  }

  return {
    accessRequestDecisionDialogView,
    accessRequestReviewRowView,
    accessRequestReviewWorkspaceView
  }
}
