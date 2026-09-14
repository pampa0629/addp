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
    <template v-if="presentation">
      <el-alert :type="presentation.alert.type" :closable="false" :title="presentation.alert.title" />
      <div v-if="conflict" class="version-conflict-notice" role="alert">
        <span>{{ t(conflictKey) }}</span>
        <el-button link type="primary" :loading="reloading" @click="emit('reload')">
          {{ t(reloadKey) }}
        </el-button>
      </div>
      <div class="policy-target">
        <strong>{{ presentation.targetName }}</strong>
        <span v-for="detail in presentation.details" :key="detail.key">{{ detail.label }}：{{ detail.value }}</span>
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
  presentation: { type: Object, default: null },
  saving: { type: Boolean, default: false },
  reloading: { type: Boolean, default: false },
  conflict: { type: Boolean, default: false }
})

const emit = defineEmits(['update:modelValue', 'reload', 'close', 'closed', 'submit'])
const { t } = useI18n()
const cancelButton = ref(null)

const titleKey = computed(() => props.mode === 're-enroll' ? 'security.enrollment.reEnrollTitle' : 'security.enrollment.rediscoverTitle')
const conflictKey = computed(() => props.mode === 're-enroll' ? 'security.enrollment.reEnrollmentVersionConflict' : 'security.enrollment.rediscoveryVersionConflict')
const reloadKey = computed(() => props.mode === 're-enroll' ? 'security.enrollment.reloadLatestForReEnrollment' : 'security.enrollment.reloadLatestForRediscovery')
const confirmKey = computed(() => props.mode === 're-enroll' ? 'security.enrollment.confirmReEnroll' : 'security.enrollment.confirmRediscover')

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
