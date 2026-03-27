<template>
  <div class="operator-palette">
    <!-- 搜索框 -->
    <el-input
      v-model="searchQuery"
      placeholder="搜索算子..."
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
              @click="handleOperatorClick(operator)"
            >
              <div class="operator-header">
                <span class="operator-name">{{ operator.name }}</span>
                <el-tooltip placement="right" :show-after="300">
                  <template #content>
                    <div class="operator-help">
                      <h4>{{ operator.name }}</h4>
                      <p class="description">{{ operator.description }}</p>
                      <div class="params-section" v-if="operator.params && Object.keys(operator.params).length > 0">
                        <h5>参数:</h5>
                        <ul>
                          <li v-for="(desc, paramName) in operator.params" :key="paramName">
                            <strong>{{ paramName }}</strong>: {{ desc }}
                          </li>
                        </ul>
                      </div>
                    </div>
                  </template>
                  <el-icon class="info-icon"><InfoFilled /></el-icon>
                </el-tooltip>
              </div>
              <div class="operator-description">{{ operator.description }}</div>
            </div>
          </div>
        </el-collapse-item>
      </el-collapse>

      <!-- 空状态 -->
      <el-empty
        v-if="!loading && filteredCategories.length === 0"
        description="未找到匹配的算子"
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
import { ref, computed, onMounted, watch } from 'vue'
import { InfoFilled } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import * as operatorApi from '@/api/operator'

// Props
const props = defineProps({
  engineType: {
    type: String,
    default: null, // 引擎模块名称: 'python_workflow', 'spark_workflow', 'math_workflow', 或 null (全部)
  }
})

const emit = defineEmits(['operator-click', 'operator-drag'])

// Data
const loading = ref(false)
const loadError = ref('')
const searchQuery = ref('')
const operators = ref([]) // 从后端获取的算子列表 (数组格式,符合 common 标准)
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

  // Python Workflow 特有分类
  '几何处理': '📐',
  '空间关系': '🔗',
  '几何属性': '📏',
  '格式转换': '🔄',
  '数据操作': '🛠️',
  '属性计算': '🧮',
  '数据筛选': '🔍'
}

// 加载算子列表
const loadOperators = async () => {
  loading.value = true
  loadError.value = ''
  try {
    let res
    if (props.engineType) {
      // 按模块加载（python/spark/math）
      res = await operatorApi.listOperatorsByModule(props.engineType)
    } else {
      // 加载所有算子
      res = await operatorApi.listAllOperators()
    }
    // API client 已自动提取 response.data,所以 res 就是后端返回的数据对象
    // 后端返回格式: {status, operators: [], count}
    operators.value = res.operators || []
  } catch (error) {
    console.error('[OperatorPalette] 加载失败:', error)
    loadError.value = '加载算子列表失败: ' + (error.response?.data?.error || error.message)
    ElMessage.error(loadError.value)
  } finally {
    loading.value = false
  }
}

// 监听引擎类型变化，自动重新加载算子
watch(() => props.engineType, () => {
  loadOperators()
})

// 分类后的算子列表
const categorizedOperators = computed(() => {
  const categories = {}

  // 按分类组织算子 (operators.value 现在是数组)
  operators.value.forEach((op) => {
    // 后端已经返回中文分类名，直接使用
    const categoryName = op.category || '其他'
    const categoryIcon = categoryIcons[categoryName] || '📝'

    if (!categories[categoryName]) {
      categories[categoryName] = {
        name: categoryName,
        icon: categoryIcon,
        operators: []
      }
    }

    // 将 parameters 数组转换为 params 对象 (用于兼容前端组件)
    const params = {}
    if (op.parameters && Array.isArray(op.parameters)) {
      op.parameters.forEach(param => {
        params[param.name] = param.description || `${param.name}参数`
      })
    }

    categories[categoryName].operators.push({
      name: op.name,
      displayName: op.display_name || op.name,
      description: op.description || '',
      params: params,
      category: categoryName,
      parameters: op.parameters || []  // 保留原始参数定义
    })
  })

  // 返回所有分类，按名称排序
  return Object.values(categories).sort((a, b) => {
    // 定义分类顺序权重（可选）
    const order = ['数据I/O', '空间分析', '几何处理', '空间关系', '几何属性',
                   '数据转换', '数据操作', '数据筛选', '聚合分析', 'SQL查询', '格式转换', '属性计算']
    const aIndex = order.indexOf(a.name)
    const bIndex = order.indexOf(b.name)
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
        op.description.toLowerCase().includes(query)
      )
    }))
    .filter(category => category.operators.length > 0)
})

// 拖拽开始
const handleDragStart = (event, operator) => {
  event.dataTransfer.effectAllowed = 'copy'
  event.dataTransfer.setData('application/json', JSON.stringify({
    type: 'operator',
    name: operator.name,
    description: operator.description,
    params: operator.params
  }))
  emit('operator-drag', operator)
}

// 点击算子
const handleOperatorClick = (operator) => {
  emit('operator-click', operator)
}

onMounted(() => {
  loadOperators()
})
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
  background: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  cursor: move;
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

.operator-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 4px;
}

.operator-name {
  font-weight: 600;
  color: var(--addp-text-primary);
  font-size: 14px;
}

.info-icon {
  color: var(--addp-text-tertiary);
  cursor: help;
  font-size: 16px;
}

.info-icon:hover {
  color: var(--el-color-primary);
}

.operator-description {
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
