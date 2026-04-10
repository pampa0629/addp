<template>
  <div class="basic-info-step">
    <el-form :model="form" ref="formRef" label-width="120px">
      <el-form-item :label="t('transfer.taskWizard.taskName')" prop="name" :rules="[{ required: true, message: t('transfer.taskWizard.taskNameRequired') }]">
        <el-input v-model="form.name" :placeholder="t('transfer.taskWizard.taskNamePlaceholder')" />
      </el-form-item>

      <el-form-item :label="t('transfer.taskWizard.taskDescription')">
        <el-input v-model="form.description" type="textarea" :rows="3"
          :placeholder="t('transfer.taskWizard.taskDescriptionPlaceholder')" />
      </el-form-item>

      <el-form-item :label="t('transfer.taskWizard.executionMode')">
        <el-radio-group v-model="form.mode">
          <el-radio-button value="batch">{{ t('transfer.taskWizard.batchMode') }}</el-radio-button>
          <el-radio-button value="stream">{{ t('transfer.taskWizard.streamMode') }}</el-radio-button>
          <el-radio-button value="micro-batch">{{ t('transfer.taskWizard.microBatchMode') }}</el-radio-button>
        </el-radio-group>
        <div class="hint">
          <p>• {{ t('transfer.taskWizard.batchModeHint') }}</p>
          <p>• {{ t('transfer.taskWizard.streamModeHint') }}</p>
          <p>• {{ t('transfer.taskWizard.microBatchModeHint') }}</p>
        </div>
      </el-form-item>

      <el-form-item :label="t('transfer.taskWizard.batchSize')">
        <el-input-number v-model="form.batch_size" :min="100" :max="10000" :step="100" />
        <div class="hint">{{ t('transfer.taskWizard.batchSizeHint') }}</div>
      </el-form-item>

      <el-form-item :label="t('transfer.taskWizard.maxParallelism')">
        <el-input-number v-model="form.max_parallelism" :min="1" :max="32" />
        <div class="hint">{{ t('transfer.taskWizard.maxParallelismHint') }}</div>
      </el-form-item>

      <el-form-item :label="t('transfer.taskWizard.schedule')">
        <slot name="schedule"></slot>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps({
  modelValue: {
    type: Object,
    required: true
  }
})

const emit = defineEmits(['update:modelValue', 'validate'])

const formRef = ref(null)
const form = ref({ ...props.modelValue })

// 同步数据
watch(() => props.modelValue, (newVal) => {
  form.value = { ...newVal }
}, { deep: true })

watch(form, (newVal) => {
  emit('update:modelValue', newVal)
}, { deep: true })

// 暴露验证方法
defineExpose({
  validate: () => formRef.value?.validate()
})
</script>

<style scoped>
.hint {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  margin-top: 5px;
  line-height: 1.6;
}

.hint p {
  margin: 2px 0;
}
</style>
