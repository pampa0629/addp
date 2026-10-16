<template>
  <div class="operator-palette">
    <!-- 搜索框 -->
    <el-input
      v-model="searchQuery"
      :placeholder="t('develop.operatorPalette.searchPlaceholder')"
      clearable
      prefix-icon="Search"
      class="search-input"
    />

    <!-- 算子分类列表 -->
    <div class="operator-categories" v-loading="loading">
      <el-collapse v-model="activeCategories" v-if="!loading && categorizedOperators.length > 0">
        <OperatorPaletteCategory
          v-for="category in filteredCategories"
          :key="category.key"
          v-model:active-categories="activeCategories"
          :category="category"
          @operator-click="handleOperatorClick"
          @operator-drag-start="handleDragStart"
        />
      </el-collapse>

      <!-- 空状态 -->
      <el-empty
        v-if="!loading && !loadError && filteredCategories.length === 0"
        :description="t('develop.operatorPalette.notFound')"
        :image-size="80"
      />

      <!-- 加载失败 -->
      <el-alert
        v-if="loadError"
        type="error"
        :title="loadError"
        :closable="false"
        show-icon
      />
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import OperatorPaletteCategory from './OperatorPaletteCategory.vue'
import { buildOperatorCategories, filterOperatorCategories, operatorCategoryKeys } from '@/utils/operatorCategories'

const { t } = useI18n()

// Props
const props = defineProps({
  workflowEngineId: {
    type: [Number, String],
    default: null,
  },
  operators: {
    type: Array,
    default: () => []
  },
  loading: {
    type: Boolean,
    default: false
  },
  loadError: {
    type: String,
    default: ''
  }
})

const emit = defineEmits(['operator-click', 'operator-drag'])

// Data
const searchQuery = ref('')
// 初次加载时展开第一条目录路径，其余目录由用户展开。
const activeCategories = ref([])

// 目录及顺序由 Runtime 声明，卡片只消费 Public Operator Spec。
const categorizedOperators = computed(() => buildOperatorCategories(props.operators.map(op => {
  const params = {}
  op.public_parameters.forEach(param => {
    params[param.name] = param.description || t('develop.operatorPalette.paramDesc', { name: param.name })
  })
  return {
    name: op.name,
    displayName: op.display_name || op.name,
    description: op.description,
    params,
    categoryPath: op.category_path,
    publicParameters: op.public_parameters,
    outputPorts: op.output_ports
  }
})))

const filteredCategories = computed(() => (
  filterOperatorCategories(categorizedOperators.value, searchQuery.value)
))

watch([filteredCategories, () => props.workflowEngineId], ([categories, engineId], previous) => {
  const keys = operatorCategoryKeys(categories)
  if (searchQuery.value.trim()) {
    activeCategories.value = keys
    return
  }
  const retained = previous?.[1] === engineId
    ? activeCategories.value.filter(key => keys.includes(key))
    : []
  if (retained.length) {
    activeCategories.value = retained
    return
  }
  const initial = []
  for (let category = categories[0]; category; category = category.children[0]) {
    initial.push(category.key)
  }
  activeCategories.value = initial
}, { immediate: true })

// 拖拽开始
const handleDragStart = (event, operator) => {
  event.dataTransfer.effectAllowed = 'copy'
  event.dataTransfer.setData('application/json', JSON.stringify({
    type: 'operator',
    name: operator.name,
    description: operator.description,
    params: operator.params,
    publicParameters: operator.publicParameters,
    output_ports: operator.outputPorts
  }))
  emit('operator-drag', operator)
}

// 点击算子
const handleOperatorClick = (operator) => {
  emit('operator-click', operator)
}

</script>

<style scoped>
.operator-palette {
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.search-input {
  margin-bottom: 12px;
  flex-shrink: 0;
}

.operator-categories {
  flex: 1;
  overflow-y: auto;
}

.operator-categories > :deep(.el-collapse) {
  border: 0;
}

/* 自定义滚动条 */
.operator-categories::-webkit-scrollbar {
  width: 6px;
}

.operator-categories::-webkit-scrollbar-track {
  background: var(--addp-bg-secondary);
  border-radius: 3px;
}

.operator-categories::-webkit-scrollbar-thumb {
  background: var(--addp-border-secondary);
  border-radius: 3px;
}

.operator-categories::-webkit-scrollbar-thumb:hover {
  background: var(--addp-text-tertiary);
}
</style>
