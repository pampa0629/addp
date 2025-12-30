<template>
  <el-dialog
    v-model="visible"
    title="保存为 查询任务"
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
      <el-form-item label="任务名称" prop="name">
        <el-input
          v-model="formData.name"
          placeholder="请输入任务名称（唯一标识）"
          maxlength="100"
          show-word-limit
        />
      </el-form-item>

      <el-form-item label="显示名称" prop="display_name">
        <el-input
          v-model="formData.display_name"
          placeholder="请输入显示名称（可选）"
          maxlength="100"
          show-word-limit
        />
      </el-form-item>

      <el-form-item label="描述" prop="description">
        <el-input
          v-model="formData.description"
          type="textarea"
          :rows="3"
          placeholder="请输入任务描述（可选）"
          maxlength="500"
          show-word-limit
        />
      </el-form-item>

      <el-form-item label="标签" prop="tags">
        <el-select
          v-model="formData.tags"
          multiple
          filterable
          allow-create
          default-first-option
          placeholder="请输入标签（可选，支持多个）"
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

      <el-form-item label="超时时间" prop="timeout">
        <el-input-number
          v-model="formData.timeout"
          :min="10"
          :max="3600"
          :step="10"
          controls-position="right"
          style="width: 200px"
        />
        <span style="margin-left: 10px; color: #909399">秒</span>
      </el-form-item>

      <el-divider content-position="left">调度设置</el-divider>

      <el-form-item label="启用调度">
        <el-switch v-model="formData.is_scheduled" />
      </el-form-item>

      <el-form-item
        v-if="formData.is_scheduled"
        label="调度配置"
        prop="schedule"
      >
        <ScheduleConfig
          v-model="formData.schedule"
          :allow-custom-cron="true"
        />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="handleClose">取消</el-button>
      <el-button type="primary" :loading="saving" @click="handleSave">
        保存
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { ScheduleConfig } from '@addp/common-frontend'

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
  timeout: 300,
  is_scheduled: false,
  schedule: ''
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
    { required: true, message: '请输入任务名称', trigger: 'blur' },
    { min: 2, max: 100, message: '长度在 2 到 100 个字符', trigger: 'blur' },
    { pattern: /^[a-zA-Z0-9_\u4e00-\u9fa5]+$/, message: '只能包含字母、数字、下划线和中文', trigger: 'blur' }
  ],
  schedule: [
    {
      validator: (rule, value, callback) => {
        if (formData.value.is_scheduled && !value) {
          callback(new Error('启用调度时必须配置调度表达式'))
        } else {
          callback()
        }
      },
      trigger: 'change'
    }
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
    timeout: 300,
    is_scheduled: false,
    schedule: ''
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
      sql: props.sql,
      description: formData.value.description,
      tags: formData.value.tags,
      timeout: formData.value.timeout,
      is_scheduled: formData.value.is_scheduled,
      schedule: formData.value.schedule
    }

    emit('saved', taskData)

  } catch (error) {
    console.error('表单验证失败:', error)
  } finally {
    saving.value = false
  }
}
</script>
