export function isCurrentSharingAccount(type, id, authContext) {
  return type === 'user' && authContext?.principal?.type === 'user' &&
    typeof id === 'string' && id !== '' && id === authContext.principal.id
}

export function sharingRecipientLabel(row, authContext, t) {
  const name = row?.recipient_label_status === 'resolved' && row.recipient_name
    ? [row.recipient_name, row.recipient_code].filter(Boolean).join(' · ')
    : t(row?.recipient_label_status === 'not_found' ? 'catalog.sharing.recipientMissing' : 'catalog.sharing.recipientUnavailable')
  const current = isCurrentSharingAccount(row?.recipient_type, row?.recipient_id, authContext)
    ? ` (${t('catalog.sharing.currentAccount')})` : ''
  return `${t(`catalog.sharing.${row?.recipient_type}`)} · ${name}${current}`
}

export function sharingRecipientOptionLabel(option, type, authContext, t) {
  return sharingRecipientLabel({ recipient_type: type, recipient_id: option.id,
    recipient_name: option.name, recipient_code: option.code, recipient_label_status: 'resolved' }, authContext, t)
}
