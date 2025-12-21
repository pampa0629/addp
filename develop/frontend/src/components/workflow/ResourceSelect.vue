<template>
  <el-select
    :model-value="modelValue"
    @update:model-value="handleChange"
    placeholder="请选择存储引擎"
    filterable
    :loading="loading"
  >
    <el-option
      v-for="resource in filteredResources"
      :key="resource.id"
      :label="`${resource.name} (${resource.resource_type})`"
      :value="resource.id"
    >
      <div class="resource-option">
        <span class="resource-name">{{ resource.name }}</span>
        <el-tag size="small" type="info">{{ resource.resource_type }}</el-tag>
      </div>
    </el-option>
  </el-select>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { listResources } from '@/api/resources'
import { ElMessage } from 'element-plus'

const props = defineProps({
  modelValue: {
    type: [Number, String],
    default: null
  },
  resourceTypes: {
    type: Array,
    default: () => []
  }
})

const emit = defineEmits(['update:modelValue', 'change'])

const resources = ref([])
const loading = ref(false)

// 根据 resourceTypes 过滤资源
const filteredResources = computed(() => {
  if (!props.resourceTypes || props.resourceTypes.length === 0) {
    return resources.value
  }
  return resources.value.filter(r =>
    props.resourceTypes.includes(r.resource_type)
  )
})

// 加载资源列表
const loadResources = async () => {
  loading.value = true
  try {
    const data = await listResources()
    resources.value = Array.isArray(data) ? data : []
  } catch (error) {
    console.error('获取资源列表失败:', error)
    ElMessage.error('获取存储引擎列表失败')
    resources.value = []
  } finally {
    loading.value = false
  }
}

// 处理选择变化
const handleChange = (value) => {
  emit('update:modelValue', value)
  emit('change', value)
}

onMounted(() => {
  loadResources()
})
</script>

<style scoped>
.resource-option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
}

.resource-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
