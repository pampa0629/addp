<template>
  <div class="field-mapping-editor">
    <el-alert type="info" :closable="false" style="margin-bottom: 20px">
      <template #title>
        {{ t('transfer.fieldMapping.configTitle') }}
      </template>
      <div>
        <p>{{ t('transfer.fieldMapping.autoMatchDesc') }}</p>
        <p v-if="autoCreateMode" style="color: var(--el-color-success); margin-top: 5px;">
          <el-icon><Check /></el-icon>
          {{ t('transfer.fieldMapping.objectStorageHint') }}
        </p>
      </div>
    </el-alert>

    <div class="toolbar">
      <el-button type="primary" @click="handleAutoMatch">
        <el-icon><MagicStick /></el-icon>
        {{ t('transfer.fieldMapping.autoMatch') }}
      </el-button>
      <el-button @click="handleAddMapping">
        <el-icon><Plus /></el-icon>
        {{ t('transfer.fieldMapping.addMapping') }}
      </el-button>
      <el-button @click="handleClearAll" :disabled="mappings.length === 0">
        <el-icon><Delete /></el-icon>
        {{ t('transfer.fieldMapping.clearAll') }}
      </el-button>

      <div style="flex: 1"></div>

      <el-button @click="fetchSourceFields" :loading="fetchingSource">
        <el-icon><Refresh /></el-icon>
        {{ t('transfer.fieldMapping.refreshSource') }}
      </el-button>
      <el-button @click="fetchTargetFields" :loading="fetchingTarget">
        <el-icon><Refresh /></el-icon>
        {{ t('transfer.fieldMapping.refreshTarget') }}
      </el-button>
    </div>

    <div class="mapping-table">
      <el-table :data="mappings" border stripe>
        <el-table-column type="index" :label="t('transfer.fieldMapping.index')" width="60" />

        <el-table-column :label="t('transfer.fieldMapping.sourceField')" min-width="180">
          <template #default="{ row, $index }">
            <el-select v-model="row.source_field" :placeholder="t('transfer.fieldMapping.selectSourceField')" filterable allow-create
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

        <el-table-column :label="t('transfer.fieldMapping.targetField')" min-width="180">
          <template #default="{ row }">
            <el-select v-model="row.target_field" :placeholder="t('transfer.fieldMapping.selectTargetField')" filterable allow-create
              style="width: 100%">
              <el-option v-for="field in targetFields" :key="field" :label="field" :value="field" />
            </el-select>
          </template>
        </el-table-column>

        <el-table-column :label="t('transfer.fieldMapping.targetType')" width="140">
          <template #default="{ row }">
            <el-select v-model="row.target_type" :placeholder="t('transfer.fieldMapping.targetType')" size="small">
              <el-option :label="t('transfer.fieldMapping.typeString')" value="string" />
              <el-option :label="t('transfer.fieldMapping.typeInteger')" value="integer" />
              <el-option :label="t('transfer.fieldMapping.typeFloat')" value="float" />
              <el-option :label="t('transfer.fieldMapping.typeDouble')" value="double" />
              <el-option :label="t('transfer.fieldMapping.typeBoolean')" value="boolean" />
              <el-option :label="t('transfer.fieldMapping.typeDate')" value="date" />
              <el-option :label="t('transfer.fieldMapping.typeTimestamp')" value="timestamp" />
              <el-option :label="t('transfer.fieldMapping.typeJSON')" value="json" />
              <el-option :label="t('transfer.fieldMapping.typeGeometry')" value="geometry" />
            </el-select>
          </template>
        </el-table-column>

        <el-table-column :label="t('transfer.fieldMapping.defaultValue')" width="120">
          <template #default="{ row }">
            <el-input v-model="row.default_value" :placeholder="t('transfer.fieldMapping.defaultValuePlaceholder')" size="small" />
          </template>
        </el-table-column>

        <el-table-column :label="t('transfer.fieldMapping.nullable')" width="80" align="center">
          <template #default="{ row }">
            <el-switch v-model="row.nullable" />
          </template>
        </el-table-column>

        <el-table-column :label="t('transfer.fieldMapping.actions')" width="80" fixed="right">
          <template #default="{ $index }">
            <el-button type="danger" size="small" link @click="handleDeleteMapping($index)">
              <el-icon><Delete /></el-icon>
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <div v-if="mappings.length === 0" class="empty-state">
        <el-empty :description="t('transfer.fieldMapping.noMappings')">
          <el-button type="primary" @click="handleAutoMatch" v-if="sourceFields.length > 0 && targetFields.length > 0">
            {{ t('transfer.fieldMapping.autoMatch') }}
          </el-button>
          <el-button @click="handleAddMapping" v-else>{{ t('transfer.fieldMapping.addMapping') }}</el-button>
        </el-empty>
      </div>
    </div>

    <div class="footer-info">
      <el-tag>{{ t('transfer.fieldMapping.sourceFieldCount', { count: sourceFields.length }) }}</el-tag>
      <el-tag type="success" style="margin-left: 10px">{{ t('transfer.fieldMapping.targetFieldCount', { count: targetFields.length }) }}</el-tag>
      <el-tag type="warning" style="margin-left: 10px">{{ t('transfer.fieldMapping.mappedCount', { count: mappings.length }) }}</el-tag>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, defineProps, defineEmits } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { MagicStick, Plus, Delete, Refresh, Right, Check } from '@element-plus/icons-vue'

const { t } = useI18n()

const props = defineProps({
  sourceFields: {
    type: Array,
    default: () => []
  },
  targetFields: {
    type: Array,
    default: () => []
  },
  sourceFieldDetails: {
    type: Array,
    default: () => []
  },
  targetFieldDetails: {
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
    ElMessage.warning(t('transfer.fieldMapping.getSourceFirst'))
    return
  }
  if (props.targetFields.length === 0) {
    ElMessage.warning(t('transfer.fieldMapping.getTargetFirst'))
    return
  }

  const newMappings = []

  // 创建源字段详情的快速查找 Map
  const sourceFieldMap = new Map()
  props.sourceFieldDetails.forEach(field => {
    sourceFieldMap.set(field.name, field)
  })

  // 完全匹配
  props.sourceFields.forEach(sourceField => {
    if (props.targetFields.includes(sourceField)) {
      const fieldDetail = sourceFieldMap.get(sourceField)
      const fieldType = fieldDetail?.is_geometry ? 'geometry' : 'string'

      newMappings.push({
        source_field: sourceField,
        target_field: sourceField,
        target_type: fieldType,
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
        const fieldDetail = sourceFieldMap.get(sourceField)
        const fieldType = fieldDetail?.is_geometry ? 'geometry' : 'string'

        newMappings.push({
          source_field: sourceField,
          target_field: targetField,
          target_type: fieldType,
          format: '',
          default_value: '',
          nullable: true
        })
      }
    })
  })

  if (newMappings.length === 0) {
    ElMessage.warning(t('transfer.fieldMapping.noMatchFound'))
    return
  }

  mappings.value = newMappings
  const geometryCount = newMappings.filter(m => m.target_type === 'geometry').length
  if (geometryCount > 0) {
    ElMessage.success(t('transfer.fieldMapping.autoMatchSuccess', { count: newMappings.length, spatial: geometryCount }))
  } else {
    ElMessage.success(t('transfer.fieldMapping.autoMatchSuccessSimple', { count: newMappings.length }))
  }
}

// 添加映射
const handleAddMapping = () => {
  mappings.value.push({
    source_field: '',
    target_field: '',
    target_type: 'string',
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
      t('transfer.fieldMapping.clearConfirm'),
      t('transfer.fieldMapping.clearConfirmTitle'),
      {
        type: 'warning'
      }
    )
    mappings.value = []
    ElMessage.success(t('transfer.fieldMapping.clearSuccess'))
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
      ElMessage.success(t('transfer.fieldMapping.autoMatchTarget', { field: match }))
    }
  }
}

// 获取源字段
const fetchSourceFields = async () => {
  fetchingSource.value = true
  try {
    emit('fetch-fields', 'source')
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
  background: var(--addp-bg-secondary);
  border-radius: 4px;
}
</style>
