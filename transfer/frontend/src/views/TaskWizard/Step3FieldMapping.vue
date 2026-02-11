<template>
  <div class="step3-field-mapping">
    <h3>字段映射</h3>
    <p class="step-description">配置源字段到目标字段的映射关系</p>

    <div class="mapping-controls">
      <el-button type="primary" @click="autoMap">自动映射（同名）</el-button>
      <el-button @click="addMapping">添加映射</el-button>
      <el-button @click="clearMappings">清空全部</el-button>
    </div>

    <el-table :data="wizardState.fieldMappings.value" border style="margin-top: 20px">
      <el-table-column label="源字段" width="200">
        <template #default="{ row, $index }">
          <el-select
            v-model="row.source_field"
            placeholder="选择源字段"
            filterable
            @change="handleMappingChange($index)"
          >
            <el-option
              v-for="field in wizardState.sourceFields.value"
              :key="field.name"
              :label="`${field.name} (${field.type})`"
              :value="field.name"
            />
          </el-select>
        </template>
      </el-table-column>

      <el-table-column label="目标字段" width="200">
        <template #default="{ row, $index }">
          <el-select
            v-model="row.target_field"
            placeholder="选择目标字段"
            filterable
            allow-create
            @change="handleMappingChange($index)"
          >
            <el-option
              v-for="field in wizardState.targetFields.value"
              :key="field.name"
              :label="`${field.name} (${field.type})`"
              :value="field.name"
            />
          </el-select>
        </template>
      </el-table-column>

      <el-table-column label="数据类型" width="140">
        <template #default="{ row, $index }">
          <el-select
            v-model="row.field_type"
            placeholder="类型"
            @change="handleMappingChange($index)"
          >
            <el-option label="字符串" value="string" />
            <el-option label="整数" value="integer" />
            <el-option label="单精度浮点" value="float" />
            <el-option label="双精度浮点" value="double" />
            <el-option label="布尔" value="boolean" />
            <el-option label="日期" value="date" />
            <el-option label="时间戳" value="timestamp" />
            <el-option label="几何" value="geometry" />
          </el-select>
        </template>
      </el-table-column>

      <el-table-column label="默认值" width="150">
        <template #default="{ row, $index }">
          <el-input
            v-model="row.default_value"
            placeholder="默认值"
            @input="handleMappingChange($index)"
          />
        </template>
      </el-table-column>

      <el-table-column label="格式" width="140">
        <template #default="{ row, $index }">
          <el-input
            v-model="row.format"
            placeholder="如: 2006-01-02"
            @input="handleMappingChange($index)"
          />
        </template>
      </el-table-column>

      <el-table-column label="可为空" width="90" align="center">
        <template #default="{ row, $index }">
          <el-switch
            v-model="row.nullable"
            @change="handleMappingChange($index)"
          />
        </template>
      </el-table-column>

      <el-table-column label="操作" width="100" fixed="right">
        <template #default="{ $index }">
          <el-button
            type="danger"
            size="small"
            @click="removeMapping($index)"
          >
            删除
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <div v-if="wizardState.fieldMappings.value.length === 0" class="empty-hint">
      <el-empty description="暂无字段映射，请点击上方按钮添加映射或自动映射" />
    </div>
  </div>
</template>

<script setup>
import { ElMessage, ElMessageBox } from 'element-plus'

const props = defineProps({
  wizardState: {
    type: Object,
    required: true
  }
})

function autoMap() {
  props.wizardState.autoGenerateFieldMappings()
  ElMessage.success('已自动生成字段映射')
}

function addMapping() {
  props.wizardState.addFieldMapping()
}

function removeMapping(index) {
  props.wizardState.removeFieldMapping(index)
}

async function clearMappings() {
  try {
    await ElMessageBox.confirm(
      '确定要清空所有字段映射吗？',
      '警告',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    while (props.wizardState.fieldMappings.value.length > 0) {
      props.wizardState.removeFieldMapping(0)
    }
    ElMessage.success('已清空字段映射')
  } catch (error) {
    // 用户取消
  }
}

function handleMappingChange(index) {
  const mapping = props.wizardState.fieldMappings.value[index]
  props.wizardState.updateFieldMapping(index, mapping)
}
</script>

<style scoped>
.step3-field-mapping {
  max-width: 1200px;
  margin: 0 auto;
}

.step-description {
  color: var(--addp-text-secondary);
  margin-bottom: 20px;
}

.mapping-controls {
  display: flex;
  gap: 12px;
}

.empty-hint {
  margin-top: 40px;
}
</style>
