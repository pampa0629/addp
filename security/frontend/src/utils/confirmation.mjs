function focusLatestConfirmationCancelButton(documentRef) {
  const messageBoxes = [...documentRef.querySelectorAll('.el-message-box.addp-message-box')]
  const messageBox = messageBoxes.at(-1)
  const buttons = [...(messageBox?.querySelectorAll('.el-message-box__btns button') || [])]
  const cancelButton = buttons.find(button => !button.classList.contains('el-button--danger'))
  cancelButton?.focus({ preventScroll: true })
}

export function confirmDangerousAction(confirm, {
  message,
  title,
  confirmButtonText,
  cancelButtonText
}, environment = {}) {
  const confirmation = confirm(message, title, {
    type: 'warning',
    customClass: 'addp-message-box',
    confirmButtonText,
    cancelButtonText,
    confirmButtonClass: 'el-button--danger',
    autofocus: false
  })

  const documentRef = environment.documentRef ?? globalThis.document
  const schedule = environment.schedule ?? globalThis.requestAnimationFrame?.bind(globalThis) ?? (callback => globalThis.setTimeout(callback, 0))
  if (documentRef) schedule(() => focusLatestConfirmationCancelButton(documentRef))

  return confirmation
}
