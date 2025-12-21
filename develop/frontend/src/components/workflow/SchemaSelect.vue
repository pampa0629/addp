<template>
  <el-select
    :model-value="modelValue"
    @update:model-value="handleChange"
    placeholder="请选择 Schema"
    filterable
    :loading="loading"
    :disabled="!resourceId"
  >
    <el-option
      v-for="schema in schemas"
      :key="schema.name"
      :label="schema.name"
      :value="schema.name"
    >
      <div class="schema-option">
        <span class="schema-name">{{ schema.name }}</span>
        <span v-if="schema.description" class="schema-desc">{{ schema.description }}</span>
      </div>
    </el-option>
  </el-select>
</template>

<script setup>
import { ref, watch } from 'vue'
import { listSchemas } from '@/api/resources'
import { ElMessage } from 'element-plus'

const props = defineProps({
  modelValue: {
    type: String,
    default: null
  },
  resourceId: {
    type: [Number, String],
    default: null
  }
})

const emit = defineEmits(['update:modelValue', 'change'])

const schemas = ref([])
const loading = ref(false)

// 加载 Schema 列表
const loadSchemas = async (resourceId) => {
  if (!resourceId) {
    schemas.value = []
    return
  }

  loading.value = true
  try {
    const data = await listSchemas(resourceId)
    schemas.value = data || []
  } catch (error) {
    console.error('获取 Schema 列表失败:', error)
    ElMessage.error('获取 Schema 列表失败')
    schemas.value = []
  } finally {
    loading.value = false
  }
}

// 处理选择变化
const handleChange = (value) => {
  emit('update:modelValue', value)
  emit('change', value)
}

// 监听 resourceId 变化
watch(
  () => props.resourceId,
  (newResourceId) => {
    loadSchemas(newResourceId)
  },
  { immediate: true }
)
</script>

<style scoped>
.schema-option {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.schema-name {
  font-weight: 500;
}

.schema-desc {
  font-size: 12px;
  color: #909399;
}
</style>
