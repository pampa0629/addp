<template>
  <el-dialog
    :model-value="modelValue"
    class="addp-dialog"
    :title="t('security.assessment.designateTitle')"
    width="min(640px, calc(100vw - 24px))"
    :close-on-click-modal="!saving"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="emit('update:modelValue', $event)"
    @open="clearValidate"
    @opened="focusPrimary"
    @closed="emit('closed')"
  >
    <el-alert type="info" :closable="false" :title="t('security.assessment.designateHint')" />
    <el-form ref="formRef" :model="form" :rules="rules" class="manual-assessment-form" label-position="top">
      <el-form-item :label="t('security.assessment.component')" prop="componentKey" required>
        <el-select
          v-model="form.componentKey"
          class="wide"
          filterable
          :placeholder="t('security.assessment.selectComponent')"
          :loading="componentsLoading"
        >
          <el-option
            v-for="option in componentOptions"
            :key="option.component.key"
            :value="option.component.key"
            :label="option.component.key"
          >
            <div class="component-option">
              <span>{{ option.component.key }}</span>
              <small>{{ option.component.value_type }}</small>
            </div>
          </el-option>
        </el-select>
      </el-form-item>
      <el-form-item :label="t('security.finding.sensitiveDataType')" prop="sensitiveDataTypeID" required>
        <el-select
          v-model="form.sensitiveDataTypeID"
          class="wide"
          :placeholder="t('security.finding.selectSensitiveDataType')"
          @change="emit('sensitiveTypeChange', $event)"
        >
          <el-option v-for="item in sensitiveTypes" :key="item.id" :label="item.name" :value="String(item.id)" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('security.finding.securityGrade')" prop="securityGradeID" required>
        <el-select v-model="form.securityGradeID" class="wide" :placeholder="t('security.finding.selectSecurityGrade')">
          <el-option v-for="item in activeGradesForType(form.sensitiveDataTypeID)" :key="item.id" :label="item.name" :value="String(item.id)" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('security.assessment.rationale')" prop="rationale" required>
        <el-input
          ref="rationaleInput"
          v-model="form.rationale"
          type="textarea"
          :rows="4"
          maxlength="2000"
          show-word-limit
          :placeholder="t('security.assessment.rationalePlaceholder')"
        />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button :disabled="saving" @click="emit('close')">{{ t('security.common.cancel') }}</el-button>
      <el-button type="primary" :loading="saving" @click="emit('submit')">
        {{ t('security.assessment.confirmDesignation') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

defineProps({
  modelValue: { type: Boolean, default: false },
  saving: { type: Boolean, default: false },
  componentsLoading: { type: Boolean, default: false },
  componentOptions: { type: Array, default: () => [] },
  sensitiveTypes: { type: Array, default: () => [] },
  form: { type: Object, required: true },
  rules: { type: Object, required: true },
  activeGradesForType: { type: Function, required: true }
})

const emit = defineEmits(['update:modelValue', 'sensitiveTypeChange', 'close', 'closed', 'submit'])
const { t } = useI18n()
const formRef = ref(null)
const rationaleInput = ref(null)

function validate() {
  return formRef.value?.validate() ?? Promise.resolve(false)
}

function clearValidate() {
  formRef.value?.clearValidate()
}

function focusPrimary() {
  rationaleInput.value?.focus?.()
}

defineExpose({ validate, clearValidate, focusPrimary })
</script>

<style scoped>
.manual-assessment-form { margin-top: 16px; }
.component-option { display: flex; align-items: center; justify-content: space-between; gap: 20px; }
.component-option small { color: var(--addp-text-tertiary); }
.wide { width: 100%; }
</style>
