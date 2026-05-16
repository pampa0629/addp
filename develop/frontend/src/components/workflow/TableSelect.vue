<template>
  <el-select
    :model-value="modelValue"
    @update:model-value="handleChange"
    placeholder="请选择表"
    filterable
    :loading="loading"
    :disabled="!engineId || !schema"
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
import { listCatalogChildren } from '@/api/engines'
import { ElMessage } from 'element-plus'

const props = defineProps({
  modelValue: {
    type: String,
    default: null
  },
  engineId: {
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
const loadTables = async (engineId, schema) => {
  if (!engineId || !schema) {
    tables.value = []
    return
  }

  loading.value = true
  try {
    const data = await listCatalogChildren(engineId, {
      segments: [{
        term: 'namespace',
        kind: 'namespace',
        name: schema
      }]
    })
    const nodes = Array.isArray(data?.nodes) ? data.nodes : []
    tables.value = nodes
      .filter(node => node.is_item)
      .map(node => ({
        name: node.name,
        type: node.kind || node.term,
        description: node.attributes?.description || node.attributes?.catalog?.description || ''
      }))
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

// 监听 engineId 和 schema 变化
watch(
  () => [props.engineId, props.schema],
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
  color: var(--addp-text-tertiary);
}
</style>
