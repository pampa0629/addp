<template>
  <div class="preview-failure" role="alert" data-testid="preview-failure">
    <el-empty :description="failureText.title">
      <p>{{ failureText.description }}</p>
    </el-empty>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({ failure: { type: Object, required: true } })
const { t } = useI18n()

const failureKind = computed(() => {
  const status = props.failure?.status
  if (status === 409 && props.failure.code === 'preview_source_refresh_required') return 'sourceRefresh'
  if (status === 503 && props.failure.code === 'preview_source_unavailable') return 'sourceUnavailable'
  if (status === 422 && props.failure.code === 'preview_text_encoding_invalid') return 'textEncoding'
  if (status === 404) return 'inaccessible'
  if (status === 403) {
    return props.failure.code === 'security_protection_required' ? 'protected' : 'forbidden'
  }
  if (status === 0 || [502, 503, 504].includes(status)) return 'unavailable'
  return 'failed'
})
const failureText = computed(() => {
  const reason = props.failure?.timeoutReason
  if (reason) {
    return {
      title: t(`common.executionFailure.reasons.${reason}`),
      description: t(`common.executionFailure.actions.${reason}`)
    }
  }
  const key = `manager.explorer.previewFailure.${failureKind.value}`
  return { title: t(`${key}.title`), description: t(`${key}.description`) }
})
</script>

<style scoped>
.preview-failure {
  display: flex;
  flex: 1;
  align-items: center;
  justify-content: center;
  min-height: 300px;
  padding: 24px;
  text-align: center;
}
</style>
