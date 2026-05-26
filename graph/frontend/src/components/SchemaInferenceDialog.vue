<template>
  <el-dialog
    v-model="visible"
    :title="t('graph.schemaInference.title')"
    width="820px"
    :close-on-click-modal="false"
    @open="handleOpen"
  >
    <div v-if="loading" class="dialog-loading">
      <el-icon class="is-loading"><Loading /></el-icon>
      <span>{{ t('graph.schemaInference.inferring') }}</span>
    </div>

    <template v-else-if="preview">
      <el-alert
        v-if="preview.entity_types?.length === 0 && preview.relation_types?.length === 0"
        type="warning"
        :title="t('graph.schemaInference.noLabels')"
        :closable="false"
        style="margin-bottom: 16px"
      />

      <div v-else>
        <div class="summary-bar">
          {{ t('graph.schemaInference.found', { entities: preview.entity_types?.length || 0, relations: preview.relation_types?.length || 0 }) }}
        </div>

        <!-- 实体类型 -->
        <div class="section-title">{{ t('graph.schemaInference.suggestedEntities') }}</div>
        <el-table
          :data="preview.entity_types"
          @selection-change="handleEntitySelectionChange"
          ref="entityTableRef"
          max-height="220"
          row-key="name"
          size="small"
        >
          <el-table-column type="selection" width="50" />
          <el-table-column :label="t('graph.schemaInference.labelName')" prop="name" min-width="120" />
          <el-table-column :label="t('graph.schemaInference.nodeCount')" prop="count" min-width="80" />
          <el-table-column :label="t('graph.schemaInference.sampledProps')" min-width="200">
            <template #default="{ row }">
              <el-tag
                v-for="p in (row.properties || []).slice(0, 5)"
                :key="p.name"
                size="small"
                style="margin: 2px"
              >{{ p.name }}</el-tag>
              <span v-if="(row.properties?.length || 0) > 5" style="color:#999;font-size:12px">
                +{{ row.properties.length - 5 }}
              </span>
            </template>
          </el-table-column>
          <el-table-column :label="t('graph.schemaInference.status')" min-width="90">
            <template #default="{ row }">
              <el-tag v-if="row.exists" type="warning" size="small">{{ t('graph.common.alreadyExists') }}</el-tag>
              <el-tag v-else type="success" size="small">{{ t('graph.common.new') }}</el-tag>
            </template>
          </el-table-column>
        </el-table>

        <!-- 关系类型 -->
        <div class="section-title" style="margin-top: 16px">{{ t('graph.schemaInference.suggestedRelations') }}</div>
        <el-table
          :data="preview.relation_types"
          @selection-change="handleRelationSelectionChange"
          ref="relationTableRef"
          max-height="180"
          row-key="key"
          size="small"
        >
          <el-table-column type="selection" width="50" />
          <el-table-column :label="t('graph.schemaInference.relType')" prop="name" min-width="140" />
          <el-table-column :label="t('graph.schemaInference.source')" prop="source_shape_name" min-width="100" />
          <el-table-column :label="t('graph.schemaInference.target')" prop="target_shape_name" min-width="100" />
          <el-table-column :label="t('graph.schemaInference.count')" prop="count" min-width="80" />
          <el-table-column :label="t('graph.schemaInference.status')" min-width="90">
            <template #default="{ row }">
              <el-tag v-if="row.exists" type="warning" size="small">{{ t('graph.common.alreadyExists') }}</el-tag>
              <el-tag v-else type="success" size="small">{{ t('graph.common.new') }}</el-tag>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <!-- 应用到本体 -->
      <div class="apply-row">
        <span class="apply-label">{{ t('graph.schemaInference.applyToOntology') }}</span>
        <el-select v-model="targetOntologyId" :placeholder="t('graph.schemaInference.selectOntology')" style="width: 220px">
          <el-option
            v-for="o in ontologies"
            :key="o.id"
            :label="o.name"
            :value="o.id"
          />
        </el-select>
        <span class="conflict-label">{{ t('graph.schemaInference.conflictLabel') }}</span>
        <el-radio-group v-model="conflict" size="small">
          <el-radio-button value="skip">{{ t('graph.common.skip') }}</el-radio-button>
          <el-radio-button value="overwrite">{{ t('graph.common.overwrite') }}</el-radio-button>
        </el-radio-group>
      </div>
    </template>

    <template #footer>
      <el-button @click="visible = false">{{ t('graph.common.cancel') }}</el-button>
      <el-button
        type="primary"
        :loading="applying"
        :disabled="!canApply"
        @click="handleApply"
      >
        {{ t('graph.schemaInference.applyBtn', { entities: selectedEntityNames.length, relations: selectedRelationKeys.length }) }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { Loading } from '@element-plus/icons-vue'
import { browseAPI } from '../api/browse'
import { ontologyAPI } from '../api/ontology'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps({
  graphId: { type: Number, required: true }
})
const emit = defineEmits(['applied'])

const visible = ref(false)
const loading = ref(false)
const applying = ref(false)
const preview = ref(null)
const ontologies = ref([])
const targetOntologyId = ref(null)
const conflict = ref('skip')
const selectedEntityNames = ref([])
const selectedRelationKeys = ref([])
const entityTableRef = ref(null)
const relationTableRef = ref(null)

const canApply = computed(() =>
  targetOntologyId.value &&
  (selectedEntityNames.value.length > 0 || selectedRelationKeys.value.length > 0)
)

function open() {
  visible.value = true
}

async function handleOpen() {
  loading.value = true
  preview.value = null
  selectedEntityNames.value = []
  selectedRelationKeys.value = []
  try {
    const [previewData, ontologyList] = await Promise.all([
      browseAPI.inferSchema(props.graphId, targetOntologyId.value),
      ontologyAPI.list()
    ])
    preview.value = previewData
    ontologies.value = Array.isArray(ontologyList) ? ontologyList : (ontologyList?.data || [])
  } catch (e) {
    ElMessage.error(t('graph.schemaInference.inferFailed', { msg: e.message || e }))
    visible.value = false
  } finally {
    loading.value = false
  }
}

function handleEntitySelectionChange(rows) {
  selectedEntityNames.value = rows.map(r => r.name)
}

function handleRelationSelectionChange(rows) {
  selectedRelationKeys.value = rows.map(r => r.key)
}

async function handleApply() {
  applying.value = true
  try {
    const result = await browseAPI.applyInferredSchema(props.graphId, {
      ontology_id: targetOntologyId.value,
      entity_type_names: selectedEntityNames.value,
      relation_type_keys: selectedRelationKeys.value,
      conflict: conflict.value
    })
    const msg = t('graph.schemaInference.applySuccess', { created: result.created, updated: result.updated, skipped: result.skipped })
    if (result.errors?.length) {
      ElMessage.warning(msg + t('graph.schemaInference.applyWarning', { count: result.errors.length }))
    } else {
      ElMessage.success(msg)
    }
    visible.value = false
    emit('applied')
  } catch (e) {
    ElMessage.error(t('graph.schemaInference.applyFailed', { msg: e.response?.data?.error || e.message || e }))
  } finally {
    applying.value = false
  }
}

defineExpose({ open })
</script>

<style scoped>
.dialog-loading {
  display: flex;
  align-items: center;
  gap: 10px;
  justify-content: center;
  padding: 40px 0;
  color: #909399;
}
.summary-bar {
  font-size: 14px;
  color: #606266;
  margin-bottom: 12px;
  padding: 8px 12px;
  background: #f5f7fa;
  border-radius: 4px;
}
.section-title {
  font-size: 13px;
  font-weight: 600;
  color: #303133;
  margin-bottom: 8px;
}
.apply-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid #f0f0f0;
  flex-wrap: wrap;
}
.apply-label, .conflict-label {
  font-size: 13px;
  color: #606266;
  white-space: nowrap;
}
</style>
