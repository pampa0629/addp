<template>
  <div class="operator-params-panel">
    <div v-if="!operator" class="empty-state">
      <el-empty description="请从画布中选择一个节点" :image-size="100" />
    </div>

    <div v-else class="params-form">
      <el-form :model="formData" label-width="120px" label-position="top">
        <!-- 节点基本信息 -->
        <section class="section">
          <h4 class="section-title">节点信息</h4>
          <el-form-item label="算子">
            <el-input :value="operator" disabled />
          </el-form-item>
          <el-form-item label="节点ID">
            <el-input :value="nodeId" disabled />
          </el-form-item>
        </section>

        <!-- 参数配置 -->
        <section class="section">
          <h4 class="section-title">参数配置</h4>

          <el-alert
            v-if="effectiveParameters.length === 0"
            type="info"
            :closable="false"
            show-icon
          >
            该算子无需配置参数
          </el-alert>

          <div v-else>
            <el-form-item
              v-for="param in effectiveParameters"
              :key="param.name"
              :label="param.name"
              :required="param.required"
            >
              <template #label>
                <div class="param-label">
                  <span>{{ param.name }}</span>
                  <el-tooltip v-if="param.description" placement="top">
                    <template #content>{{ param.description }}</template>
                    <el-icon class="help-icon"><QuestionFilled /></el-icon>
                  </el-tooltip>
                </div>
              </template>

              <!-- 根据参数类型渲染不同的输入组件 -->
              <component
                :is="getComponentByType(param)"
                v-model="formData[param.name]"
                v-bind="getComponentProps(param)"
                :placeholder="getPlaceholder(param)"
                @change="onParamChange(param.name)"
              >
                <!-- 渲染 enum 下拉选项 -->
                <template v-if="param.enum && param.enum.length > 0">
                  <el-option
                    v-for="option in param.enum"
                    :key="option"
                    :label="option"
                    :value="option"
                  />
                </template>
              </component>
            </el-form-item>
          </div>
        </section>

        <!-- 保存按钮 -->
        <el-form-item>
          <el-button type="primary" @click="saveParams">保存参数</el-button>
          <el-button @click="resetParams">重置</el-button>
        </el-form-item>
      </el-form>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, computed } from 'vue'
import { QuestionFilled } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import ResourceSelect from './ResourceSelect.vue'
import SchemaSelect from './SchemaSelect.vue'
import TableSelect from './TableSelect.vue'

const props = defineProps({
  nodeId: {
    type: String,
    default: ''
  },
  operator: {
    type: String,
    default: ''
  },
  paramDefinitions: {
    type: Object,
    default: () => ({})
  },
  parameters: {
    type: Array,
    default: () => []
  },
  initialParams: {
    type: Object,
    default: () => ({})
  }
})

const emit = defineEmits(['save'])

const formData = ref({})

// 使用结构化参数定义(优先)或回退到旧格式
const effectiveParameters = computed(() => {
  if (props.parameters && props.parameters.length > 0) {
    return props.parameters
  }

  // 向后兼容:将旧的 paramDefinitions 转换为参数数组
  return Object.entries(props.paramDefinitions).map(([name, desc]) => ({
    name,
    type: 'string',
    description: desc,
    required: false
  }))
})

// 构建参数依赖关系映射
const dependencyMap = computed(() => {
  const deps = {}
  effectiveParameters.value.forEach(param => {
    if (param.depends_on) {
      if (!deps[param.depends_on]) {
        deps[param.depends_on] = []
      }
      deps[param.depends_on].push(param.name)
    }
  })
  return deps
})

// 监听 initialParams 变化,初始化表单
watch(() => props.initialParams, (newParams) => {
  formData.value = { ...newParams }
}, { immediate: true, deep: true })

// 监听 operator 变化,重置表单
watch(() => props.operator, () => {
  formData.value = { ...props.initialParams }
}, { deep: true })

// 根据参数元数据选择组件类型
const getComponentByType = (param) => {
  // 1. 优先检查 ui_type (自定义 UI 组件)
  if (param.ui_type === 'resource_select') return ResourceSelect
  if (param.ui_type === 'schema_select') return SchemaSelect
  if (param.ui_type === 'table_select') return TableSelect

  // 2. 检查 enum (下拉选择)
  if (param.enum && param.enum.length > 0) return 'el-select'

  // 3. 根据 type 选择基础组件
  if (param.type === 'integer' || param.type === 'float' || param.type === 'number') {
    return 'el-input-number'
  }
  if (param.type === 'boolean') return 'el-switch'

  // 4. 默认文本输入
  return 'el-input'
}

// 获取组件的 props
const getComponentProps = (param) => {
  const props = {}

  // 自定义组件的专用 props
  if (param.ui_type === 'resource_select') {
    props.engineTypes = param.resource_types || []
  } else if (param.ui_type === 'schema_select') {
    props.resourceId = formData.value[param.depends_on] || null
  } else if (param.ui_type === 'table_select') {
    // table_select 依赖两个参数: resource_id 和 schema
    // 找到 schema 参数的 depends_on (通常是 resource_id)
    const schemaParam = effectiveParameters.value.find(p => p.name === param.depends_on)
    if (schemaParam && schemaParam.depends_on) {
      props.resourceId = formData.value[schemaParam.depends_on] || null
    }
    props.schema = formData.value[param.depends_on] || null
  }

  // enum 类型的下拉选择
  if (param.enum && param.enum.length > 0) {
    // el-select 会在模板中自动渲染 options
  }

  // el-input-number 的 props
  if (param.type === 'integer') {
    props.step = 1
    props.precision = 0
  } else if (param.type === 'float' || param.type === 'number') {
    props.step = 0.01
    props.precision = 2
  }

  // 数值范围
  if (param.min !== undefined) props.min = param.min
  if (param.max !== undefined) props.max = param.max

  return props
}

// 获取 placeholder
const getPlaceholder = (param) => {
  if (param.default !== undefined && param.default !== null) {
    return `默认: ${param.default}`
  }
  return `请输入 ${param.name}`
}

// 参数变更处理(级联清空逻辑)
const onParamChange = (paramName) => {
  // 清空所有依赖此参数的子参数
  if (dependencyMap.value[paramName]) {
    dependencyMap.value[paramName].forEach(depParam => {
      formData.value[depParam] = null
    })
  }
}

// 保存参数
const saveParams = () => {
  // 验证必填参数
  for (const param of effectiveParameters.value) {
    if (param.required && !formData.value[param.name]) {
      ElMessage.warning(`请填写必填参数: ${param.name}`)
      return
    }
  }

  emit('save', {
    nodeId: props.nodeId,
    params: { ...formData.value }
  })

  ElMessage.success('参数已保存')
}

// 重置参数
const resetParams = () => {
  formData.value = { ...props.initialParams }
  ElMessage.info('已重置参数')
}
</script>

<style scoped>
.operator-params-panel {
  height: 100%;
  overflow-y: auto;
  padding: 16px;
}

.empty-state {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
}

.params-form {
  height: 100%;
}

.section {
  margin-bottom: 24px;
  padding-bottom: 16px;
  border-bottom: 1px solid #e4e7ed;
}

.section:last-child {
  border-bottom: none;
}

.section-title {
  font-size: 14px;
  font-weight: 600;
  color: #303133;
  margin: 0 0 16px 0;
  display: flex;
  align-items: center;
}

.section-title::before {
  content: '';
  display: inline-block;
  width: 4px;
  height: 14px;
  background: #409eff;
  margin-right: 8px;
  border-radius: 2px;
}

.param-label {
  display: flex;
  align-items: center;
  gap: 6px;
}

.help-icon {
  color: #909399;
  cursor: help;
  font-size: 14px;
}

.help-icon:hover {
  color: #409eff;
}

/* 自定义滚动条 */
.operator-params-panel::-webkit-scrollbar {
  width: 6px;
}

.operator-params-panel::-webkit-scrollbar-track {
  background: #f5f7fa;
  border-radius: 3px;
}

.operator-params-panel::-webkit-scrollbar-thumb {
  background: #c0c4cc;
  border-radius: 3px;
}

.operator-params-panel::-webkit-scrollbar-thumb:hover {
  background: #909399;
}
</style>
