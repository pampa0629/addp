<template>
  <el-alert type="warning" :closable="false" :title="hint" />
  <div v-if="conflict" class="version-conflict-notice" role="alert">
    <span>{{ t('security.accessRequest.decisionVersionConflict') }}</span>
    <el-button link type="primary" :loading="reloading" @click="emit('reload')">
      {{ t('security.accessRequest.reloadLatestForDecision') }}
    </el-button>
  </div>
  <div class="policy-target">
    <strong>{{ request.target_full_name }} · {{ request.component?.key }}</strong>
    <span>{{ t('security.accessRequest.requester') }}：{{ request.requester?.display_name }}（{{ releaseActorLabel(request.requester?.id) }}）</span>
    <span>{{ t('security.accessRequest.requestedUntil') }}：{{ formatDateTime(request.requested_expires_at) }}</span>
    <span>{{ t('security.accessRequest.rationale') }}：{{ request.rationale }}</span>
  </div>
  <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
    <el-form-item :label="t('security.accessRequest.decisionRationaleLabel')" prop="rationale" required>
      <el-input
        v-model="form.rationale"
        type="textarea"
        :rows="4"
        maxlength="2000"
        show-word-limit
        :placeholder="t('security.accessRequest.decisionRationale')"
      />
    </el-form-item>
  </el-form>
</template>

<script setup>
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

defineProps({
  request: { type: Object, required: true },
  hint: { type: String, required: true },
  conflict: { type: Boolean, default: false },
  reloading: { type: Boolean, default: false },
  form: { type: Object, required: true },
  rules: { type: Object, required: true },
  releaseActorLabel: { type: Function, required: true },
  formatDateTime: { type: Function, required: true }
})

const emit = defineEmits(['reload'])
const { t } = useI18n()
const formRef = ref(null)

function validate() {
  return formRef.value?.validate() ?? Promise.resolve(false)
}

function clearValidate() {
  formRef.value?.clearValidate()
}

defineExpose({ validate, clearValidate })
</script>

<style scoped>
.policy-target { display: flex; flex-direction: column; gap: 4px; margin: 16px 0; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.policy-target span { color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; }
.version-conflict-notice { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 12px; padding: 10px 12px; color: var(--el-color-warning); border: 1px solid var(--el-color-warning); border-radius: 8px; background: var(--addp-bg-secondary); font-size: 13px; line-height: 1.5; }
@media (max-width: 720px) {
  .version-conflict-notice { align-items: flex-start; flex-direction: column; }
}
</style>
