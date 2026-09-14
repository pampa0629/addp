import {
  isZeroFindingDiscovery,
  normalizeDiscoverySummary
} from '../utils/protectionEnrollment.mjs'

export function createProtectionEnrollmentResourcePresentation({
  t,
  engineNames,
  formatDateTime,
  releaseBasisLabel,
  releaseActorLabel,
  ownerLabel,
  effectLabel,
  actionLabel
}) {
  function resourceName(row) {
    const fullName = String(row.target_snapshot?.full_name || '').trim()
    if (!fullName) return t('security.enrollment.unknownResource')
    const separator = ['object', 'file', 'directory'].includes(String(row.target_snapshot?.item_type || '').toLowerCase()) ? '/' : '.'
    return fullName.split(separator).filter(Boolean).at(-1) || fullName
  }

  function resourcePath(row) {
    return String(row.target_snapshot?.full_name || '').trim() || t('security.enrollment.snapshotUnavailable')
  }

  function engineLabel(engineID) {
    const id = Number(engineID || 0)
    return engineNames.value.get(id) || t('security.enrollment.engineId', { id: id || '-' })
  }

  function itemTypeLabel(itemType) {
    const key = String(itemType || 'unknown').toLowerCase()
    const translated = t(`security.enrollment.itemTypes.${key}`)
    return translated === `security.enrollment.itemTypes.${key}` ? key : translated
  }

  function resourceIdentityView(resource) {
    return {
      resource,
      name: resourceName(resource),
      path: resourcePath(resource),
      itemType: itemTypeLabel(resource?.target_snapshot?.item_type),
      engine: engineLabel(resource?.target_snapshot?.engine_id)
    }
  }

  function enrollmentCreationSelectionView(item, context = {}) {
    return {
      name: item?.name,
      fullName: item?.full_name,
      itemType: itemTypeLabel(item?.item_type),
      engine: context.engineName || engineLabel(item?.engine_id),
      lastScannedAt: formatDateTime(item?.scanned_at),
      scope: t('security.enrollment.wholeResourceScope')
    }
  }

  function enrollmentLifecycleActionDialogView(enrollment, mode) {
    const reEnroll = mode === 're-enroll'
    const unavailable = t('security.common.notAvailable')
    return {
      alert: {
        type: reEnroll ? 'warning' : 'info',
        title: reEnroll
          ? t('security.enrollment.reEnrollWarning', { resource: resourceName(enrollment) })
          : t('security.enrollment.rediscoverHint')
      },
      targetName: String(enrollment?.target_snapshot?.full_name || '').trim() || unavailable,
      details: reEnroll
        ? [
            { key: 'basis', label: t('security.enrollment.releaseBasisLabel'), value: releaseBasisLabel(enrollment?.release_basis) },
            { key: 'releasedAt', label: t('security.enrollment.releasedAt'), value: formatDateTime(enrollment?.released_at) },
            { key: 'reason', label: t('security.enrollment.releaseReasonLabel'), value: enrollment?.release_reason || unavailable }
          ]
        : [
            { key: 'lastDiscoveredAt', label: t('security.enrollment.lastDiscovered'), value: formatDateTime(enrollment?.last_discovered_at) }
          ]
    }
  }

  function enrollmentReleaseDialogView(enrollment, basis) {
    const noSupportedFindings = basis === 'no_supported_findings'
    return {
      title: t(noSupportedFindings
        ? 'security.enrollment.confirmNoProtectionNeeded'
        : 'security.enrollment.release'),
      warning: t(noSupportedFindings
        ? 'security.enrollment.noFindingsReleaseWarning'
        : 'security.enrollment.releaseWarning'),
      targetName: String(enrollment?.target_snapshot?.full_name || '').trim() || t('security.common.notAvailable'),
      basisLabel: releaseBasisLabel(basis),
      reasonPlaceholder: t(noSupportedFindings
        ? 'security.enrollment.noFindingsReleaseReason'
        : 'security.enrollment.releaseReason'),
      confirmLabel: t(noSupportedFindings
        ? 'security.enrollment.confirmNoProtectionNeededAction'
        : 'security.enrollment.confirmRelease')
    }
  }

  function discoveryPresentation(row) {
    const summary = normalizeDiscoverySummary(row)
    if (summary.status !== 'completed') {
      return {
        type: 'info', alertType: 'info', label: t('security.enrollment.discoveryNotCompleted'),
        detailTitle: t('security.enrollment.discoveryNotCompletedTitle'),
        detailDescription: t('security.enrollment.discoveryNotCompletedDescription')
      }
    }
    if (summary.findingCount === 0) {
      return {
        type: 'success', alertType: 'info', label: t('security.enrollment.discoveryZeroFindings'),
        detailTitle: t('security.enrollment.discoveryZeroFindingsTitle'),
        detailDescription: t('security.enrollment.discoveryZeroFindingsDescription')
      }
    }
    if (summary.pendingReviewCount === 0) {
      return {
        type: 'success', alertType: 'success', label: t('security.enrollment.discoveryReviewCompleted'),
        detailTitle: t('security.enrollment.discoveryReviewCompletedTitle', { count: summary.findingCount }),
        detailDescription: t('security.enrollment.discoveryReviewCompletedDescription')
      }
    }
    return {
      type: 'warning', alertType: 'warning', label: t('security.enrollment.discoveryPendingCount', { count: summary.pendingReviewCount }),
      detailTitle: t('security.enrollment.discoveryFindingCountTitle', { count: summary.findingCount }),
      detailDescription: t('security.enrollment.discoveryFindingCountDescription', { count: summary.pendingReviewCount })
    }
  }

  function presentationState(row) {
    if (row.state === 'released') return { type: 'info', label: t('security.enrollment.states.released'), description: t('security.enrollment.stateDescriptions.released') }
    if (row.state === 'releasing') return { type: 'warning', label: t('security.enrollment.states.releasing'), description: t('security.enrollment.stateDescriptions.releasing') }
    const owners = Array.isArray(row.owner_progress) ? row.owner_progress : []
    if (owners.length > 0 && owners.every(owner => owner.acknowledged && owner.projection_state === 'active')) {
      return { type: 'success', label: t('security.enrollment.states.active'), description: t('security.enrollment.stateDescriptions.active') }
    }
    if (row.state === 'active') return { type: 'success', label: t('security.enrollment.states.active'), description: t('security.enrollment.stateDescriptions.active') }
    const activeOwners = owners.filter(owner => owner.acknowledged && owner.projection_state === 'active').length
    if (activeOwners > 0) return { type: 'primary', label: t('security.enrollment.states.partiallyActive'), description: t('security.enrollment.stateDescriptions.partiallyActive', { count: activeOwners }) }
    if (row.state === 'activating') return { type: 'warning', label: t('security.enrollment.states.activating'), description: t('security.enrollment.stateDescriptions.activating') }
    return { type: 'primary', label: t('security.enrollment.states.enrolling'), description: t('security.enrollment.stateDescriptions.enrolling') }
  }

  function ownerPresentation(row, owner) {
    if (row.state === 'released') return { type: 'info', label: t('security.enrollment.ownerStates.released') }
    if (!owner.acknowledged) return { type: 'warning', label: t('security.enrollment.ownerStates.waiting') }
    if (owner.projection_state === 'active') return { type: 'success', label: t('security.enrollment.ownerStates.active') }
    return { type: 'info', label: t('security.enrollment.ownerStates.denied') }
  }

  function resourceRowView(enrollment) {
    const discovery = discoveryPresentation(enrollment)
    const owners = Array.isArray(enrollment?.owner_progress) ? enrollment.owner_progress : []
    return {
      id: enrollment?.id,
      enrollment,
      identity: resourceIdentityView(enrollment),
      state: presentationState(enrollment),
      owners: owners.map(owner => ({
        key: owner.consumer_owner,
        label: ownerLabel(owner.consumer_owner),
        state: ownerPresentation(enrollment, owner)
      })),
      discovery: {
        type: discovery.type,
        label: discovery.label,
        observedAt: formatDateTime(enrollment?.last_discovered_at)
      },
      releasedAt: formatDateTime(enrollment?.released_at),
      canReEnroll: enrollment?.state === 'released'
    }
  }

  function resourceListView(enrollments) {
    const rows = Array.isArray(enrollments) ? enrollments : []
    return {
      rows: rows.map(resourceRowView)
    }
  }

  function ownerEffectDescription(owner) {
    const rules = Array.isArray(owner.rules) ? owner.rules : []
    if (!owner.acknowledged) return t('security.enrollment.ownerEffectWaiting')
    if (owner.projection_state === 'enrolling') {
      return owner.consumer_owner === 'manager'
        ? t('security.enrollment.ownerEffectDeniedPendingRule')
        : t('security.enrollment.ownerEffectDeniedUnsupported')
    }
    return rules.length
      ? t('security.enrollment.ownerEffectRequirements', {
          rules: rules.map(rule => t('security.finding.outletRule', {
            action: actionLabel(rule.action), effect: effectLabel(rule.effect)
          })).join('；')
        })
      : t('security.enrollment.ownerEffectActive')
  }

  function resourceDetailView(enrollment, lifecycle) {
    const state = String(enrollment?.state || '')
    const releaseAuditVisible = ['releasing', 'released'].includes(state)
    const zeroFindingDiscovery = isZeroFindingDiscovery(enrollment)
    const owners = Array.isArray(enrollment?.owner_progress) ? enrollment.owner_progress : []
    return {
      enrollment,
      identity: {
        name: resourceName(enrollment),
        path: resourcePath(enrollment)
      },
      state: presentationState(enrollment),
      releaseAudit: releaseAuditVisible
        ? {
            basis: releaseBasisLabel(enrollment?.release_basis),
            requestedBy: releaseActorLabel(enrollment?.release_requested_by),
            requestedAt: formatDateTime(enrollment?.release_requested_at),
            releasedAt: enrollment?.released_at ? formatDateTime(enrollment.released_at) : null,
            reason: enrollment?.release_reason || t('security.common.notAvailable')
          }
        : null,
      discovery: discoveryPresentation(enrollment),
      owners: owners.map(owner => ({
        key: owner.consumer_owner,
        label: ownerLabel(owner.consumer_owner),
        effectDescription: ownerEffectDescription(owner),
        state: ownerPresentation(enrollment, owner)
      })),
      facts: {
        lastDiscoveredAt: formatDateTime(enrollment?.last_discovered_at),
        createdAt: formatDateTime(enrollment?.created_at)
      },
      technical: {
        resourceIdentity: enrollment?.target?.resource_identity,
        enrollmentId: enrollment?.id,
        releaseSourceSnapshotHash: enrollment?.release_source_snapshot_hash || null
      },
      actions: {
        canReEnroll: Boolean(lifecycle?.canCreate && state === 'released'),
        canRediscover: Boolean(lifecycle?.canUpdate && ['enrolling', 'active'].includes(state)),
        release: lifecycle?.canUpdate && !releaseAuditVisible
          ? {
              basis: zeroFindingDiscovery ? 'no_supported_findings' : 'manual',
              label: zeroFindingDiscovery
                ? t('security.enrollment.confirmNoProtectionNeeded')
                : t('security.enrollment.release')
            }
          : null
      }
    }
  }

  return {
    itemTypeLabel,
    resourceIdentityView,
    resourceRowView,
    resourceListView,
    enrollmentCreationSelectionView,
    enrollmentLifecycleActionDialogView,
    enrollmentReleaseDialogView,
    discoveryPresentation,
    resourceDetailView,
    presentationState,
    ownerPresentation,
    ownerEffectDescription
  }
}
