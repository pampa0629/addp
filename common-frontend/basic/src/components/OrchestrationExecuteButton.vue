<template>
  <el-button type="success" :loading="busy" :disabled="disabled || busy" @click="run">
    {{ t('common.orchestration.execute') }}
  </el-button>
</template>
<script setup>
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
const props = defineProps({
  orchestration: { type: Object, required: true },
  execute: { type: Function, required: true },
  disabled: Boolean
})
const emit = defineEmits(['executed', 'busy'])
const { t } = useI18n()
const busy = ref(false)
async function run() {
  if (busy.value || props.disabled) return
  busy.value = true
  emit('busy', true)
  try {
    await ElMessageBox.confirm(
      t('common.orchestration.confirmMessage', { name: props.orchestration.name, count: props.orchestration.steps?.length || 0 }),
      t('common.orchestration.confirmTitle'),
      { type: 'warning', customClass: 'addp-message-box', confirmButtonText: t('common.orchestration.execute'), cancelButtonText: t('common.orchestration.cancel') }
    )
    const result = await props.execute(props.orchestration.id)
    emit('executed', result)
    ElMessage.success(t('common.orchestration.started'))
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(error?.response?.data?.error || t('common.orchestration.executeFailed'))
  } finally {
    busy.value = false
    emit('busy', false)
  }
}
</script>
