import { ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { executeWithCurrentResultConfirmation } from '@/utils/currentResultConfirmation'

export const useCurrentResultConfirmation = () => {
  const { t } = useI18n()
  return execute => executeWithCurrentResultConfirmation(execute, () => ElMessageBox.confirm(
    t('manager.common.currentResultOverwriteConfirm'),
    t('manager.common.currentResultOverwriteTitle'),
    {
      type: 'warning',
      confirmButtonText: t('manager.common.currentResultOverwriteButton'),
      cancelButtonText: t('common.cancel')
    }
  ))
}
