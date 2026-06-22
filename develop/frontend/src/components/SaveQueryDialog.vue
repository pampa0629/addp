<template>
  <el-dialog
    v-model="visible"
    :title="t('develop.saveQueryDialog.title')"
    width="600px"
    :close-on-click-modal="false"
    @close="handleClose"
  >
    <el-form
      ref="formRef"
      :model="formData"
      :rules="rules"
      label-width="100px"
    >
      <el-form-item :label="t('develop.saveQueryDialog.taskName')" prop="name">
        <el-input
          v-model="formData.name"
          :placeholder="t('develop.saveQueryDialog.taskNamePlaceholder')"
          maxlength="100"
          show-word-limit
        />
      </el-form-item>

      <el-form-item :label="t('develop.saveQueryDialog.displayName')" prop="display_name">
        <el-input
          v-model="formData.display_name"
          :placeholder="t('develop.saveQueryDialog.displayNamePlaceholder')"
          maxlength="100"
          show-word-limit
        />
      </el-form-item>

      <el-form-item :label="t('develop.saveQueryDialog.description')" prop="description">
        <el-input
          v-model="formData.description"
          type="textarea"
          :rows="3"
          :placeholder="t('develop.saveQueryDialog.descriptionPlaceholder')"
          maxlength="500"
          show-word-limit
        />
      </el-form-item>

      <el-form-item :label="t('develop.saveQueryDialog.tags')" prop="tags">
        <el-select
          v-model="formData.tags"
          multiple
          filterable
          allow-create
          default-first-option
          :placeholder="t('develop.saveQueryDialog.tagsPlaceholder')"
          style="width: 100%"
        >
          <el-option
            v-for="tag in tagOptions"
            :key="tag"
            :label="tag"
            :value="tag"
          />
        </el-select>
      </el-form-item>

      <el-form-item :label="t('develop.saveQueryDialog.timeout')" prop="timeout">
        <el-input-number
          v-model="formData.timeout"
          :min="10"
          :max="3600"
          :step="10"
          controls-position="right"
          style="width: 200px"
        />
        <span style="margin-left: 10px; color: var(--addp-text-tertiary)">{{ t('develop.saveQueryDialog.seconds') }}</span>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="handleClose">{{ t('develop.saveQueryDialog.cancel') }}</el-button>
      <el-button type="primary" :loading="saving" @click="handleSave">
        {{ t('develop.saveQueryDialog.save') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'

const { t } = useI18n()

const props = defineProps({
  modelValue: {
    type: Boolean,
    default: false
  },
  engineId: {
    type: Number,
    default: null
  },
  sql: {
    type: String,
    required: true
  }
})

const emit = defineEmits(['update:modelValue', 'saved'])

const visible = computed({
  get: () => props.modelValue,
  set: (val) => emit('update:modelValue', val)
})

const formRef = ref(null)
const saving = ref(false)

const formData = ref({
  name: '',
  display_name: '',
  description: '',
  tags: [],
  timeout: 300
})

const tagOptions = ref([
  '数据分析',
  '数据清洗',
  '报表',
  '监控',
  '测试'
])

const rules = {
  name: [
    { required: true, message: t('develop.saveQueryDialog.nameRequired'), trigger: 'blur' },
    { min: 2, max: 100, message: t('develop.saveQueryDialog.nameLengthHint'), trigger: 'blur' },
    { pattern: /^[a-zA-Z0-9_\u4e00-\u9fa5]+$/, message: t('develop.saveQueryDialog.namePatternHint'), trigger: 'blur' }
  ]
}

// 监听对话框打开，重置表单
watch(visible, (val) => {
  if (val) {
    resetForm()
  }
})

const resetForm = () => {
  formData.value = {
    name: '',
    display_name: '',
    description: '',
    tags: [],
    timeout: 300
  }
  formRef.value?.clearValidate()
}

const handleClose = () => {
  visible.value = false
}

const handleSave = async () => {
  try {
    await formRef.value.validate()

    saving.value = true

    const taskData = {
      name: formData.value.name,
      display_name: formData.value.display_name || formData.value.name,
      engine_id: props.engineId,
      query: props.sql,
      description: formData.value.description,
      tags: formData.value.tags,
      timeout: formData.value.timeout
    }

    emit('saved', taskData)

  } catch (error) {
    console.error('表单验证失败:', error)
  } finally {
    saving.value = false
  }
}
</script>
