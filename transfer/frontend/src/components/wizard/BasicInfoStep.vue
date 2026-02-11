<template>
  <div class="basic-info-step">
    <el-form :model="form" ref="formRef" label-width="120px">
      <el-form-item label="任务名称" prop="name" :rules="[{ required: true, message: '请输入任务名称' }]">
        <el-input v-model="form.name" placeholder="例如:用户数据每日同步" />
      </el-form-item>

      <el-form-item label="任务描述">
        <el-input v-model="form.description" type="textarea" :rows="3"
          placeholder="描述任务的用途和注意事项" />
      </el-form-item>

      <el-form-item label="执行模式">
        <el-radio-group v-model="form.mode">
          <el-radio-button label="batch">批处理</el-radio-button>
          <el-radio-button label="stream">流式</el-radio-button>
          <el-radio-button label="micro-batch">微批</el-radio-button>
        </el-radio-group>
        <div class="hint">
          <p>• 批处理:适合定期大批量数据传输(推荐)</p>
          <p>• 流式:适合实时数据传输</p>
          <p>• 微批:小批量实时传输</p>
        </div>
      </el-form-item>

      <el-form-item label="批量大小">
        <el-input-number v-model="form.batch_size" :min="100" :max="10000" :step="100" />
        <div class="hint">每批处理的记录数,推荐 1000-5000</div>
      </el-form-item>

      <el-form-item label="最大并行度">
        <el-input-number v-model="form.max_parallelism" :min="1" :max="32" />
        <div class="hint">同时运行的 Worker 数量,推荐 2-8</div>
      </el-form-item>

      <el-form-item label="定时调度">
        <slot name="schedule"></slot>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'

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
