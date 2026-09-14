import { ElMessageBox } from 'element-plus'

export const confirmReturnToDraft = t => ElMessageBox.confirm(
  t('model.common.reopen_confirm'), t('model.common.reopen'), {
    type: 'warning', customClass: 'addp-message-box',
    confirmButtonText: t('model.common.reopen'), cancelButtonText: t('model.common.cancel')
  }
)
