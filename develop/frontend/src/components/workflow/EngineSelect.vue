<template>
  <el-select
    :model-value="modelValue"
    @update:model-value="handleChange"
    :placeholder="t('develop.engineSelect.placeholder')"
    filterable
    :loading="loading"
  >
    <el-option
      v-for="engine in filteredEngines"
      :key="engine.id"
      `:label="`${engine.name} (${engine.engine_type})`"
      :value="engine.id"
    >
      <div class="engine-option">
        <span class="engine-name">{{ engine.name }}</span>
        <el-tag size="small" type="info">{{ engine.engine_type }}</el-tag>
      </div>
    </el-option>
  </el-select>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { listEngines } from '@/api/engines'
import { ElMessage } from 'element-plus'

const { t } = useI18n()

const props = defineProps({
  modelValue: {
    type: [Number, String],
    default: null
  },
  engineTypes: {
    type: Array,
    default: () => []
  }
})

const emit = defineEmits(['update:modelValue', 'change'])

const engines = ref([])
const loading = ref(false)

// 根据 engineTypes 过滤引擎
const filteredEngines = computed(() => {
  if (!props.engineTypes || props.engineTypes.length === 0) {
    return engines.value
  }
  return engines.value.filter(r =>
    props.engineTypes.includes(r.engine_type)
  )
})

// 加载引擎列表
const loadEngines = async () => {
  loading.value = true
  try {
    const data = await listEngines()
    engines.value = Array.isArray(data) ? data : []
  } catch (error) {
    console.error('获取引擎列表失败:', error)
    ElMessage.error(t('develop.engineSelect.loadFailed'))
    engines.value = []
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
  loadEngines()
})
</script>

<style scoped>
.engine-option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
}

.engine-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
