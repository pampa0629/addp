<template>
  <div class="field-mapping-editor">
    <el-alert type="info" :closable="false" style="margin-bottom: 20px">
      <template #title>
        配置源字段到目标字段的映射关系
      </template>
      <div>
        <p>系统已自动进行同名匹配。您可以手动调整映射关系、添加转换函数或设置默认值。</p>
        <p v-if="autoCreateMode" style="color: #67C23A; margin-top: 5px;">
          <el-icon><Check /></el-icon>
          目标为对象存储，将自动创建目标字段（所有源字段都会导出到文件）
        </p>
      </div>
    </el-alert>

    <div class="toolbar">
      <el-button type="primary" @click="handleAutoMatch">
        <el-icon><MagicStick /></el-icon>
        自动匹配同名字段
      </el-button>
      <el-button @click="handleAddMapping">
        <el-icon><Plus /></el-icon>
        添加映射
      </el-button>
      <el-button @click="handleClearAll" :disabled="mappings.length === 0">
        <el-icon><Delete /></el-icon>
        清空所有
      </el-button>

      <div style="flex: 1"></div>

      <el-button @click="fetchSourceFields" :loading="fetchingSource">
        <el-icon><Refresh /></el-icon>
        刷新源字段
      </el-button>
      <el-button @click="fetchTargetFields" :loading="fetchingTarget">
        <el-icon><Refresh /></el-icon>
        刷新目标字段
      </el-button>
    </div>

    <div class="mapping-table">
      <el-table :data="mappings" border stripe>
        <el-table-column type="index" label="序号" width="60" />

        <el-table-column label="源字段" min-width="180">
          <template #default="{ row, $index }">
            <el-select v-model="row.source_field" placeholder="选择源字段" filterable allow-create
              @change="handleSourceFieldChange(row, $index)" style="width: 100%">
              <el-option v-for="field in sourceFields" :key="field" :label="field" :value="field" />
            </el-select>
          </template>
        </el-table-column>

        <el-table-column label="" width="60" align="center">
          <template #default>
            <el-icon><Right /></el-icon>
          </template>
        </el-table-column>

        <el-table-column label="目标字段" min-width="180">
          <template #default="{ row }">
            <el-select v-model="row.target_field" placeholder="选择目标字段" filterable allow-create
              style="width: 100%">
              <el-option v-for="field in targetFields" :key="field" :label="field" :value="field" />
            </el-select>
          </template>
        </el-table-column>

        <el-table-column label="字段类型" width="140">
          <template #default="{ row }">
            <el-select v-model="row.field_type" placeholder="类型" size="small">
              <el-option label="字符串" value="string" />
              <el-option label="整数" value="integer" />
              <el-option label="浮点数" value="float" />
              <el-option label="布尔" value="boolean" />
              <el-option label="日期" value="date" />
              <el-option label="时间戳" value="timestamp" />
              <el-option label="JSON" value="json" />
            </el-select>
          </template>
        </el-table-column>

        <el-table-column label="转换函数" width="160">
          <template #default="{ row }">
            <el-select v-model="row.transform" placeholder="无" clearable size="small">
              <el-option label="转大写" value="upper" />
              <el-option label="转小写" value="lower" />
              <el-option label="去空格" value="trim" />
              <el-option label="格式化日期" value="format_date" />
              <el-option label="转时间戳" value="to_timestamp" />
              <el-option label="转整数" value="to_int" />
              <el-option label="转浮点数" value="to_float" />
              <el-option label="JSON 解析" value="parse_json" />
              <el-option label="JSON 序列化" value="stringify_json" />
            </el-select>
          </template>
        </el-table-column>

        <el-table-column label="格式" width="140">
          <template #default="{ row }">
            <el-input v-model="row.format" placeholder="如: 2006-01-02" size="small"
              v-if="['format_date', 'to_timestamp'].includes(row.transform)" />
          </template>
        </el-table-column>

        <el-table-column label="默认值" width="120">
          <template #default="{ row }">
            <el-input v-model="row.default_value" placeholder="默认值" size="small" />
          </template>
        </el-table-column>

        <el-table-column label="可空" width="80" align="center">
          <template #default="{ row }">
            <el-switch v-model="row.nullable" />
          </template>
        </el-table-column>

        <el-table-column label="操作" width="80" fixed="right">
          <template #default="{ $index }">
            <el-button type="danger" size="small" link @click="handleDeleteMapping($index)">
              <el-icon><Delete /></el-icon>
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <div v-if="mappings.length === 0" class="empty-state">
        <el-empty description="暂无字段映射">
          <el-button type="primary" @click="handleAutoMatch" v-if="sourceFields.length > 0 && targetFields.length > 0">
            自动匹配字段
          </el-button>
          <el-button @click="handleAddMapping" v-else>添加映射</el-button>
        </el-empty>
      </div>
    </div>

    <div class="footer-info">
      <el-tag>源字段数: {{ sourceFields.length }}</el-tag>
      <el-tag type="success" style="margin-left: 10px">目标字段数: {{ targetFields.length }}</el-tag>
      <el-tag type="warning" style="margin-left: 10px">已映射: {{ mappings.length }}</el-tag>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, defineProps, defineEmits } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { MagicStick, Plus, Delete, Refresh, Right, Check } from '@element-plus/icons-vue'

const props = defineProps({
  sourceFields: {
    type: Array,
    default: () => []
  },
  targetFields: {
    type: Array,
    default: () => []
  },
  mappings: {
    type: Array,
    default: () => []
  },
  autoCreateMode: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['update:mappings', 'fetch-fields'])

const mappings = ref([])
const fetchingSource = ref(false)
const fetchingTarget = ref(false)

// 监听外部传入的 mappings 变化
watch(() => props.mappings, (newVal) => {
  mappings.value = newVal || []
}, { immediate: true })

// 监听内部 mappings 变化，同步到外部
watch(mappings, (newVal) => {
  emit('update:mappings', newVal)
}, { deep: true })

// 自动匹配同名字段
const handleAutoMatch = () => {
  if (props.sourceFields.length === 0) {
    ElMessage.warning('请先获取源字段列表')
    return
  }
  if (props.targetFields.length === 0) {
    ElMessage.warning('请先获取目标字段列表')
    return
  }

  const newMappings = []

  // 完全匹配
  props.sourceFields.forEach(sourceField => {
    if (props.targetFields.includes(sourceField)) {
      newMappings.push({
        source_field: sourceField,
        target_field: sourceField,
        field_type: 'string',
        transform: '',
        format: '',
        default_value: '',
        nullable: true
      })
    }
  })

  // 模糊匹配（去掉下划线、转小写后比较）
  props.sourceFields.forEach(sourceField => {
    const normalizedSource = sourceField.toLowerCase().replace(/_/g, '')

    props.targetFields.forEach(targetField => {
      const normalizedTarget = targetField.toLowerCase().replace(/_/g, '')

      if (normalizedSource === normalizedTarget &&
          !newMappings.find(m => m.source_field === sourceField)) {
        newMappings.push({
          source_field: sourceField,
          target_field: targetField,
          field_type: 'string',
          transform: '',
          format: '',
          default_value: '',
          nullable: true
        })
      }
    })
  })

  if (newMappings.length === 0) {
    ElMessage.warning('未找到可匹配的字段')
    return
  }

  mappings.value = newMappings
  ElMessage.success(`成功匹配 ${newMappings.length} 个字段`)
}

// 添加映射
const handleAddMapping = () => {
  mappings.value.push({
    source_field: '',
    target_field: '',
    field_type: 'string',
    transform: '',
    format: '',
    default_value: '',
    nullable: true
  })
}

// 删除映射
const handleDeleteMapping = (index) => {
  mappings.value.splice(index, 1)
}

// 清空所有映射
const handleClearAll = async () => {
  try {
    await ElMessageBox.confirm(
      '确定要清空所有字段映射吗？',
      '确认删除',
      {
        type: 'warning'
      }
    )
    mappings.value = []
    ElMessage.success('已清空所有映射')
  } catch {
    // 取消操作
  }
}

// 源字段变更时，尝试自动匹配目标字段
const handleSourceFieldChange = (row, index) => {
  if (!row.target_field) {
    // 尝试在目标字段中找到同名字段
    if (props.targetFields.includes(row.source_field)) {
      row.target_field = row.source_field
      return
    }

    // 尝试模糊匹配
    const normalizedSource = row.source_field.toLowerCase().replace(/_/g, '')
    const match = props.targetFields.find(field =>
      field.toLowerCase().replace(/_/g, '') === normalizedSource
    )

    if (match) {
      row.target_field = match
      ElMessage.success(`自动匹配到目标字段: ${match}`)
    }
  }
}

// 获取源字段
const fetchSourceFields = async () => {
  fetchingSource.value = true
  try {
    emit('fetch-fields', 'source')
    // 实际应该调用后端 API
    // 这里由父组件处理
  } finally {
    fetchingSource.value = false
  }
}

// 获取目标字段
const fetchTargetFields = async () => {
  fetchingTarget.value = true
  try {
    emit('fetch-fields', 'target')
  } finally {
    fetchingTarget.value = false
  }
}
</script>

<style scoped>
.field-mapping-editor {
  width: 100%;
}

.toolbar {
  display: flex;
  gap: 10px;
  margin-bottom: 20px;
  align-items: center;
}

.mapping-table {
  margin-bottom: 20px;
}

.empty-state {
  padding: 40px 0;
}

.footer-info {
  display: flex;
  justify-content: flex-end;
  padding: 10px;
  background: #f5f7fa;
  border-radius: 4px;
}
</style>
