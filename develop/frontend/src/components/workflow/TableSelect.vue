<template>
  <el-select
    :model-value="modelValue"
    @update:model-value="handleChange"
    placeholder="请选择表"
    filterable
    :loading="loading"
    :disabled="!resourceId || !schema"
  >
    <el-option
      v-for="table in tables"
      :key="table.name"
      :label="table.name"
      :value="table.name"
    >
      <div class="table-option">
        <span class="table-name">{{ table.name }}</span>
        <div class="table-meta">
          <el-tag v-if="table.type" size="small" type="info">{{ table.type }}</el-tag>
          <span v-if="table.description" class="table-desc">{{ table.description }}</span>
        </div>
      </div>
    </el-option>
  </el-select>
</template>

<script setup>
import { ref, watch } from 'vue'
import { listTables } from '@/api/resources'
import { ElMessage } from 'element-plus'

const props = defineProps({
  modelValue: {
    type: String,
    default: null
  },
  resourceId: {
    type: [Number, String],
    default: null
  },
  schema: {
    type: String,
    default: null
  }
})

const emit = defineEmits(['update:modelValue', 'change'])

const tables = ref([])
const loading = ref(false)

// 加载表列表
const loadTables = async (resourceId, schema) => {
  if (!resourceId || !schema) {
    tables.value = []
    return
  }

  loading.value = true
  try {
    const data = await listTables(resourceId, schema)
    tables.value = data || []
  } catch (error) {
    console.error('获取表列表失败:', error)
    ElMessage.error('获取表列表失败')
    tables.value = []
  } finally {
    loading.value = false
  }
}

// 处理选择变化
const handleChange = (value) => {
  emit('update:modelValue', value)
  emit('change', value)
}

// 监听 resourceId 和 schema 变化
watch(
  () => [props.resourceId, props.schema],
  ([newResourceId, newSchema]) => {
    loadTables(newResourceId, newSchema)
  },
  { immediate: true }
)
</script>

<style scoped>
.table-option {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.table-name {
  font-weight: 500;
}

.table-meta {
  display: flex;
  align-items: center;
  gap: 8px;
}

.table-desc {
  font-size: 12px;
  color: #909399;
}
</style>
