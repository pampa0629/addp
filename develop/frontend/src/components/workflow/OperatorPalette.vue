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
        <el-collapse-item
          v-for="category in filteredCategories"
          :key="category.name"
          :name="category.name"
        >
          <template #title>
            <div class="category-header">
              <span class="category-icon">{{ category.icon }}</span>
              <span class="category-name">{{ category.name }}</span>
              <el-tag size="small" type="info" class="category-count">
                {{ category.operators.length }}
              </el-tag>
            </div>
          </template>

          <!-- 算子列表 -->
          <div class="operator-list">
            <div
              v-for="operator in category.operators"
              :key="operator.name"
              class="operator-item"
              draggable="true"
              @dragstart="handleDragStart($event, operator)"
            >
              <button
                type="button"
                class="operator-add-button"
                :aria-label="t('develop.operatorPalette.addLabel', { name: operator.displayName })"
                @click="handleOperatorClick(operator)"
              >
                <span class="operator-name">{{ operator.displayName }}</span>
                <span
                  v-if="hasDistinctText(operator.description, operator.displayName, operator.name)"
                  class="operator-description"
                >
                  {{ operator.description }}
                </span>
              </button>
              <el-tooltip effect="light" placement="right" :show-after="300">
                <template #content>
                  <div class="operator-help">
                    <h4>{{ operator.displayName }}</h4>
                    <p
                      v-if="hasDistinctText(operator.description, operator.displayName, operator.name)"
                      class="description"
                    >
                      {{ operator.description }}
                    </p>
                    <div class="params-section" v-if="operator.params && Object.keys(operator.params).length > 0">
                      <h5>{{ t('develop.operatorPalette.params') }}:</h5>
                      <ul>
                        <li v-for="(desc, paramName) in operator.params" :key="paramName">
                          <strong>{{ paramName }}</strong>: {{ desc }}
                        </li>
                      </ul>
                    </div>
                  </div>
                </template>
                <button
                  type="button"
                  class="info-button"
                  :aria-label="t('develop.operatorPalette.helpLabel', { name: operator.displayName })"
                  @click.stop
                >
                  <el-icon class="info-icon"><InfoFilled /></el-icon>
                </button>
              </el-tooltip>
            </div>
          </div>
        </el-collapse-item>
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
import { InfoFilled } from '@element-plus/icons-vue'
import { hasDistinctText } from '@/utils/displayText'

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
// 默认收拢所有分类，用户可以按需展开
const activeCategories = ref([])

// 分类映射：后端的分类名称已经是中文，直接使用即可
// 这里只是为每个分类添加图标
const categoryIcons = {
  // 通用分类（Python 和 Spark 共用）
  '数据I/O': '💾',
  '空间分析': '🗺️',
  '数据转换': '🔄',
  '聚合分析': '📊',
  'SQL查询': '🔎',

  // GeoPython Workflow 特有分类
  '几何处理': '📐',
  '空间关系': '🔗',
  '几何属性': '📏',
  '格式转换': '🔄',
  '数据操作': '🛠️',
  '属性计算': '🧮',
  '数据筛选': '🔍'
}

// 分类后的算子列表
const categorizedOperators = computed(() => {
  const categories = {}

  // 按工作流引擎声明的分组目录组织算子。
  props.operators.forEach((op) => {
    const categoryPath = op.category_path
    const categoryName = categoryPath.join(' / ')
    const categoryIcon = categoryIcons[categoryPath[0]] || '📝'

    if (!categories[categoryName]) {
      categories[categoryName] = {
        name: categoryName,
        path: categoryPath,
        icon: categoryIcon,
        operators: []
      }
    }

    // 工具提示和画布只消费 Develop 聚合的 Public Operator Spec。
    const params = {}
    op.public_parameters.forEach(param => {
      params[param.name] = param.description || t('develop.operatorPalette.paramDesc', { name: param.name })
    })

    categories[categoryName].operators.push({
      name: op.name,
      displayName: op.display_name || op.name,
      description: op.description,
      params: params,
      category: categoryName,
      categoryPath,
      publicParameters: op.public_parameters,
      outputPorts: op.output_ports
    })
  })

  // 返回所有分类，按名称排序
  return Object.values(categories).sort((a, b) => {
    // 定义分类顺序权重（可选）
    const order = ['数据I/O', '空间分析', '几何处理', '空间关系', '几何属性',
                   '数据转换', '数据操作', '数据筛选', '聚合分析', 'SQL查询', '格式转换', '属性计算']
    const aIndex = order.indexOf(a.path[0])
    const bIndex = order.indexOf(b.path[0])
    if (aIndex !== -1 && bIndex !== -1) return aIndex - bIndex
    if (aIndex !== -1) return -1
    if (bIndex !== -1) return 1
    return a.name.localeCompare(b.name)
  })
})

// 过滤后的分类列表 (基于搜索)
const filteredCategories = computed(() => {
  if (!searchQuery.value.trim()) {
    return categorizedOperators.value
  }

  const query = searchQuery.value.toLowerCase()
  return categorizedOperators.value
    .map(category => ({
      ...category,
      operators: category.operators.filter(op =>
        op.name.toLowerCase().includes(query) ||
        op.displayName.toLowerCase().includes(query) ||
        op.description.toLowerCase().includes(query)
      )
    }))
    .filter(category => category.operators.length > 0)
})

watch(categorizedOperators, categories => {
  if (activeCategories.value.length === 0 && categories.length > 0) {
    activeCategories.value = [categories[0].name]
  }
}, { immediate: true })

watch(searchQuery, query => {
  if (query.trim()) {
    activeCategories.value = filteredCategories.value.map(category => category.name)
  }
})

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
  margin-bottom: 16px;
  flex-shrink: 0;
}

.operator-categories {
  flex: 1;
  overflow-y: auto;
}

.category-header {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
}

.category-icon {
  font-size: 18px;
}

.category-name {
  font-weight: 600;
  color: var(--addp-text-primary);
  flex: 1;
}

.category-count {
  margin-left: auto;
}

.operator-list {
  padding: 8px 0;
}

.operator-item {
  padding: 12px;
  margin: 4px 0;
  display: grid;
  grid-template-columns: minmax(0, 1fr) 24px;
  align-items: start;
  gap: 8px;
  background: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  cursor: grab;
  transition: all 0.2s;
}

.operator-item:hover {
  background: var(--addp-bg-secondary);
  border-color: var(--el-color-primary);
  transform: translateX(4px);
}

.operator-item:active {
  cursor: grabbing;
}

.operator-add-button {
  width: 100%;
  min-width: 0;
  padding: 0;
  color: inherit;
  font: inherit;
  text-align: left;
  background: transparent;
  border: 0;
  border-radius: 2px;
  cursor: pointer;
}

.operator-add-button:focus-visible {
  outline: 2px solid var(--el-color-primary);
  outline-offset: 3px;
}

.operator-name {
  display: block;
  min-width: 0;
  overflow: hidden;
  font-weight: 600;
  color: var(--addp-text-primary);
  font-size: 14px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.info-button {
  width: 24px;
  height: 24px;
  padding: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  color: var(--addp-text-tertiary);
  font: inherit;
  background: transparent;
  border: 0;
  border-radius: 4px;
  cursor: help;
}

.info-button:hover {
  color: var(--el-color-primary);
  background: var(--addp-bg-primary);
}

.info-button:focus-visible {
  color: var(--el-color-primary);
  outline: 2px solid var(--el-color-primary);
  outline-offset: 1px;
}

.info-icon {
  font-size: 16px;
  pointer-events: none;
}

.operator-description {
  margin-top: 4px;
  display: block;
  font-size: 12px;
  color: var(--addp-text-secondary);
  line-height: 1.4;
}

/* Tooltip 帮助内容 */
.operator-help {
  max-width: 400px;
}

.operator-help h4 {
  margin: 0 0 8px 0;
  font-size: 16px;
  color: var(--addp-text-primary);
}

.operator-help .description {
  margin: 0 0 12px 0;
  font-size: 14px;
  color: var(--addp-text-secondary);
  line-height: 1.5;
}

.operator-help .params-section h5 {
  margin: 0 0 8px 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.operator-help .params-section ul {
  margin: 0;
  padding-left: 20px;
}

.operator-help .params-section li {
  margin: 4px 0;
  font-size: 13px;
  color: var(--addp-text-secondary);
  line-height: 1.4;
}

.operator-help .params-section strong {
  color: var(--el-color-primary);
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
