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
import { ref, computed, onMounted } from 'vue'
import { InfoFilled } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import * as spatialApi from '@/api/spatial'

// Props
const emit = defineEmits(['operator-click', 'operator-drag'])

// Data
const loading = ref(false)
const loadError = ref('')
const searchQuery = ref('')
const operators = ref({}) // 从后端获取的算子列表 {operatorName: {params, category, description}}
const activeCategories = ref(['几何处理', '空间关系']) // 默认展开的分类

// 分类映射 (category → icon + 中文名)
const categoryMapping = {
  geometry_processing: { name: '几何处理', icon: '📐' },
  spatial_relationships: { name: '空间关系', icon: '🔗' },
  geometry_attributes: { name: '几何属性', icon: '📏' },
  format_conversion: { name: '格式转换', icon: '🔄' },
  batch_operations: { name: '批处理', icon: '📦' },
  advanced_operations: { name: '高级算子', icon: '⚡' }
}

// 加载算子列表
const loadOperators = async () => {
  loading.value = true
  loadError.value = ''
  try {
    const res = await spatialApi.listOperators()
    operators.value = res.data.operators || {}
  } catch (error) {
    loadError.value = '加载算子列表失败: ' + error.message
    ElMessage.error(loadError.value)
  } finally {
    loading.value = false
  }
}

// 分类后的算子列表
const categorizedOperators = computed(() => {
  const categories = {}

  // 按分类组织算子
  Object.entries(operators.value).forEach(([name, info]) => {
    const categoryKey = info.category || 'other'
    const categoryInfo = categoryMapping[categoryKey] || { name: '其他', icon: '📝' }

    if (!categories[categoryInfo.name]) {
      categories[categoryInfo.name] = {
        name: categoryInfo.name,
        icon: categoryInfo.icon,
        operators: []
      }
    }

    categories[categoryInfo.name].operators.push({
      name,
      description: info.description || '',
      params: info.params || {},
      category: categoryInfo.name
    })
  })

  // 转换为数组并按预定义顺序排序
  const categoryOrder = Object.values(categoryMapping).map(c => c.name)
  return categoryOrder
    .filter(name => categories[name])
    .map(name => categories[name])
    .concat(
      Object.values(categories).filter(c => !categoryOrder.includes(c.name))
    )
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
  color: #303133;
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
  background: #f5f7fa;
  border: 1px solid #e4e7ed;
  border-radius: 4px;
  cursor: move;
  transition: all 0.2s;
}

.operator-item:hover {
  background: #ecf5ff;
  border-color: #409eff;
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
  color: #303133;
  font-size: 14px;
}

.info-icon {
  color: #909399;
  cursor: help;
  font-size: 16px;
}

.info-icon:hover {
  color: #409eff;
}

.operator-description {
  font-size: 12px;
  color: #606266;
  line-height: 1.4;
}

/* Tooltip 帮助内容 */
.operator-help {
  max-width: 400px;
}

.operator-help h4 {
  margin: 0 0 8px 0;
  font-size: 16px;
  color: #303133;
}

.operator-help .description {
  margin: 0 0 12px 0;
  font-size: 14px;
  color: #606266;
  line-height: 1.5;
}

.operator-help .params-section h5 {
  margin: 0 0 8px 0;
  font-size: 14px;
  font-weight: 600;
  color: #303133;
}

.operator-help .params-section ul {
  margin: 0;
  padding-left: 20px;
}

.operator-help .params-section li {
  margin: 4px 0;
  font-size: 13px;
  color: #606266;
  line-height: 1.4;
}

.operator-help .params-section strong {
  color: #409eff;
}

/* 自定义滚动条 */
.operator-categories::-webkit-scrollbar {
  width: 6px;
}

.operator-categories::-webkit-scrollbar-track {
  background: #f5f7fa;
  border-radius: 3px;
}

.operator-categories::-webkit-scrollbar-thumb {
  background: #c0c4cc;
  border-radius: 3px;
}

.operator-categories::-webkit-scrollbar-thumb:hover {
  background: #909399;
}
</style>
