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
            v-if="!paramDefinitions || Object.keys(paramDefinitions).length === 0"
            type="info"
            :closable="false"
            show-icon
          >
            该算子无需配置参数
          </el-alert>

          <div v-else>
            <el-form-item
              v-for="(desc, paramName) in paramDefinitions"
              :key="paramName"
              :label="paramName"
              :required="isRequired(desc)"
            >
              <template #label>
                <div class="param-label">
                  <span>{{ paramName }}</span>
                  <el-tooltip v-if="desc" placement="top">
                    <template #content>{{ desc }}</template>
                    <el-icon class="help-icon"><QuestionFilled /></el-icon>
                  </el-tooltip>
                </div>
              </template>

              <!-- 根据参数类型渲染不同的输入组件 -->
              <component
                :is="getInputComponent(desc)"
                v-model="formData[paramName]"
                v-bind="getInputProps(desc)"
                :placeholder="getPlaceholder(paramName, desc)"
              />
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
  initialParams: {
    type: Object,
    default: () => ({})
  }
})

const emit = defineEmits(['save'])

const formData = ref({})

// 监听 initialParams 变化，初始化表单
watch(() => props.initialParams, (newParams) => {
  formData.value = { ...newParams }
}, { immediate: true, deep: true })

// 监听 operator 变化，重置表单
watch(() => props.operator, () => {
  formData.value = { ...props.initialParams }
}, { deep: true })

// 判断参数是否必填
const isRequired = (desc) => {
  if (!desc) return false
  // 简单判断：描述中包含 "required" 或 "*" 表示必填
  return desc.toLowerCase().includes('required') || desc.includes('*')
}

// 根据参数描述推断输入组件类型
const getInputComponent = (desc) => {
  if (!desc) return 'el-input'

  const descLower = desc.toLowerCase()

  // 数字类型
  if (descLower.includes('float') || descLower.includes('number') || descLower.includes('距离') || descLower.includes('半径')) {
    return 'el-input-number'
  }

  // 整数类型
  if (descLower.includes('int') || descLower.includes('count') || descLower.includes('个数')) {
    return 'el-input-number'
  }

  // 布尔类型
  if (descLower.includes('bool') || descLower.includes('是否')) {
    return 'el-switch'
  }

  // 选项类型
  if (descLower.includes('select') || descLower.includes('选择')) {
    return 'el-select'
  }

  // 多行文本
  if (descLower.includes('text') || descLower.includes('多行')) {
    return 'el-input'
  }

  // 默认单行文本
  return 'el-input'
}

// 根据参数描述获取输入组件的 props
const getInputProps = (desc) => {
  if (!desc) return {}

  const descLower = desc.toLowerCase()
  const props = {}

  // el-input-number 专用 props
  if (descLower.includes('float') || descLower.includes('number')) {
    props.step = 0.01
    props.precision = 2
    props.min = 0
  } else if (descLower.includes('int')) {
    props.step = 1
    props.min = 0
  }

  // el-input 专用 props
  if (descLower.includes('text') || descLower.includes('多行')) {
    props.type = 'textarea'
    props.rows = 3
  }

  return props
}

// 获取 placeholder
const getPlaceholder = (paramName, desc) => {
  if (desc && desc.includes('例如:')) {
    const match = desc.match(/例如[：:]\s*(.+)/)
    if (match) return match[1]
  }

  return `请输入 ${paramName}`
}

// 保存参数
const saveParams = () => {
  // 简单验证：检查必填参数
  for (const [paramName, desc] of Object.entries(props.paramDefinitions)) {
    if (isRequired(desc) && !formData.value[paramName]) {
      ElMessage.warning(`请填写必填参数: ${paramName}`)
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
