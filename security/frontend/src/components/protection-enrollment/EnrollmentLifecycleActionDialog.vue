<template>
  <el-dialog
    :model-value="modelValue"
    class="addp-dialog"
    :title="t(titleKey)"
    width="min(560px, calc(100vw - 24px))"
    :close-on-click-modal="!saving"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="emit('update:modelValue', $event)"
    @opened="focusPrimary"
    @closed="emit('closed')"
  >
    <template v-if="enrollment">
      <el-alert :type="mode === 're-enroll' ? 'warning' : 'info'" :closable="false" :title="hintTitle" />
      <div v-if="conflict" class="version-conflict-notice" role="alert">
        <span>{{ t(conflictKey) }}</span>
        <el-button link type="primary" :loading="reloading" @click="emit('reload')">
          {{ t(reloadKey) }}
        </el-button>
      </div>
      <div class="policy-target">
        <strong>{{ enrollment.target_snapshot?.full_name || t('security.common.notAvailable') }}</strong>
        <template v-if="mode === 're-enroll'">
          <span>{{ t('security.enrollment.releaseBasisLabel') }}：{{ releaseBasisLabel(enrollment.release_basis) }}</span>
          <span>{{ t('security.enrollment.releasedAt') }}：{{ formatDateTime(enrollment.released_at) }}</span>
          <span>{{ t('security.enrollment.releaseReasonLabel') }}：{{ enrollment.release_reason || t('security.common.notAvailable') }}</span>
        </template>
        <span v-else>{{ t('security.enrollment.lastDiscovered') }}：{{ formatDateTime(enrollment.last_discovered_at) }}</span>
      </div>
    </template>
    <template #footer>
      <el-button ref="cancelButton" :disabled="saving" @click="emit('close')">
        {{ t('security.common.cancel') }}
      </el-button>
      <el-button type="primary" :loading="saving" :disabled="conflict" @click="emit('submit')">
        {{ t(confirmKey) }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  mode: { type: String, required: true, validator: value => ['re-enroll', 'rediscover'].includes(value) },
  enrollment: { type: Object, default: null },
  saving: { type: Boolean, default: false },
  reloading: { type: Boolean, default: false },
  conflict: { type: Boolean, default: false },
  resourceName: { type: Function, required: true },
  releaseBasisLabel: { type: Function, required: true },
  formatDateTime: { type: Function, required: true }
})

const emit = defineEmits(['update:modelValue', 'reload', 'close', 'closed', 'submit'])
const { t } = useI18n()
const cancelButton = ref(null)

const titleKey = computed(() => props.mode === 're-enroll' ? 'security.enrollment.reEnrollTitle' : 'security.enrollment.rediscoverTitle')
const conflictKey = computed(() => props.mode === 're-enroll' ? 'security.enrollment.reEnrollmentVersionConflict' : 'security.enrollment.rediscoveryVersionConflict')
const reloadKey = computed(() => props.mode === 're-enroll' ? 'security.enrollment.reloadLatestForReEnrollment' : 'security.enrollment.reloadLatestForRediscovery')
const confirmKey = computed(() => props.mode === 're-enroll' ? 'security.enrollment.confirmReEnroll' : 'security.enrollment.confirmRediscover')
const hintTitle = computed(() => props.mode === 're-enroll'
  ? t('security.enrollment.reEnrollWarning', { resource: props.resourceName(props.enrollment) })
  : t('security.enrollment.rediscoverHint'))

function focusPrimary() {
  const button = cancelButton.value?.$el || cancelButton.value
  button?.focus?.()
}

defineExpose({ focusPrimary })
</script>

<style scoped>
.policy-target { display: flex; flex-direction: column; gap: 4px; margin: 16px 0; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.policy-target span { color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; }
.version-conflict-notice { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 12px; padding: 10px 12px; color: var(--el-color-warning); border: 1px solid var(--el-color-warning); border-radius: 8px; background: var(--addp-bg-secondary); font-size: 13px; line-height: 1.5; }
@media (max-width: 720px) {
  .version-conflict-notice { align-items: flex-start; flex-direction: column; }
}
</style>
