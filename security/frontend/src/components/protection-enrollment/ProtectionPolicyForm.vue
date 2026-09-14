<template>
  <el-alert :type="mode === 'restore' ? 'warning' : 'info'" :closable="false" :title="t(hintKey)" />
  <div v-if="conflict" class="version-conflict-notice" role="alert">
    <span>{{ t(conflictKey) }}</span>
    <el-button link type="primary" :loading="reloading" @click="emit('reload')">
      {{ t(reloadKey) }}
    </el-button>
  </div>
  <div class="policy-target">
    <strong>{{ presentation.componentKey }}</strong>
    <span>{{ presentation.assessmentSummary }}</span>
    <span>{{ presentation.protectionSummary }}</span>
  </div>
  <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
    <template v-if="mode === 'tighten'">
      <el-form-item :label="t('security.policy.scope')">
        <el-input :model-value="t('security.policy.managerPreview')" disabled />
      </el-form-item>
      <el-form-item :label="t('security.policy.effect')" prop="effect" required>
        <el-radio-group v-model="form.effect">
          <el-radio-button v-for="option in presentation.effectOptions" :key="option.value" :value="option.value">
            {{ option.label }}
          </el-radio-button>
        </el-radio-group>
        <div class="field-help">{{ selectedEffectImpact }}</div>
      </el-form-item>
      <el-form-item :label="t('security.policy.rationale')" prop="rationale" required>
        <el-input v-model="form.rationale" type="textarea" :rows="4" maxlength="2000" show-word-limit :placeholder="t('security.policy.rationalePlaceholder')" />
      </el-form-item>
    </template>
    <el-form-item v-else :label="t('security.policy.restoreRationale')" prop="rationale" required>
      <el-input
        v-model="form.rationale"
        type="textarea"
        :rows="4"
        maxlength="2000"
        show-word-limit
        :placeholder="t('security.policy.restoreRationalePlaceholder')"
      />
    </el-form-item>
  </el-form>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  mode: { type: String, required: true, validator: value => ['tighten', 'restore'].includes(value) },
  presentation: { type: Object, required: true },
  conflict: { type: Boolean, default: false },
  reloading: { type: Boolean, default: false },
  form: { type: Object, required: true },
  rules: { type: Object, required: true }
})

const emit = defineEmits(['reload'])
const { t } = useI18n()
const formRef = ref(null)
const hintKey = computed(() => props.mode === 'restore' ? 'security.policy.restoreHint' : 'security.policy.hint')
const conflictKey = computed(() => props.mode === 'restore' ? 'security.policy.restoreVersionConflict' : 'security.policy.versionConflict')
const reloadKey = computed(() => props.mode === 'restore' ? 'security.policy.reloadLatestForRestore' : 'security.policy.reloadLatest')
const selectedEffectImpact = computed(() => props.presentation.effectOptions
  .find(option => option.value === props.form.effect)?.impact || '')

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
