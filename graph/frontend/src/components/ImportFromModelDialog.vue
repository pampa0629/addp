<template>
  <el-dialog
    v-model="visible"
    :title="t('graph.importFromModel.title')"
    width="800px"
    :close-on-click-modal="false"
    @open="handleOpen"
  >
    <div v-if="loading" class="dialog-loading">
      <el-icon class="is-loading"><Loading /></el-icon>
      <span>{{ t('graph.importFromModel.loading') }}</span>
    </div>

    <template v-else>
      <!-- 步骤指示 -->
      <el-steps :active="step" simple style="margin-bottom: 20px">
        <el-step :title="t('graph.importFromModel.selectEntities')" />
        <el-step :title="t('graph.importFromModel.selectRelations')" />
      </el-steps>

      <!-- 步骤 1：选择实体 -->
      <div v-if="step === 0">
        <div class="step-header">
          <span>{{ t('graph.importFromModel.entityCount', { count: preview.entities?.length || 0 }) }}</span>
          <div>
            <el-button size="small" @click="selectAllEntities">{{ t('graph.common.selectAll') }}</el-button>
            <el-button size="small" @click="selectedEntityIds = []">{{ t('graph.common.clearAll') }}</el-button>
          </div>
        </div>
        <el-table
          :data="preview.entities"
          @selection-change="handleEntitySelectionChange"
          ref="entityTableRef"
          max-height="340"
          row-key="model_id"
        >
          <el-table-column type="selection" width="50" />
          <el-table-column :label="t('graph.importFromModel.entityName')" prop="name" min-width="120" />
          <el-table-column :label="t('graph.importFromModel.displayName')" prop="label" min-width="120" />
          <el-table-column :label="t('graph.importFromModel.propCount')" min-width="80">
            <template #default="{ row }">{{ row.properties?.length || 0 }}</template>
          </el-table-column>
          <el-table-column :label="t('graph.importFromModel.status')" min-width="100">
            <template #default="{ row }">
              <el-tag v-if="row.exists" type="warning" size="small">{{ t('graph.common.alreadyExists') }}</el-tag>
              <el-tag v-else type="success" size="small">{{ t('graph.common.new') }}</el-tag>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <!-- 步骤 2：选择关系 -->
      <div v-if="step === 1">
        <div class="step-header">
          <span>{{ t('graph.importFromModel.relationCount', { count: filteredRelations.length }) }}</span>
          <div>
            <el-button size="small" @click="selectAllRelations">{{ t('graph.common.selectAll') }}</el-button>
            <el-button size="small" @click="selectedRelationIds = []">{{ t('graph.common.clearAll') }}</el-button>
          </div>
        </div>
        <el-table
          :data="filteredRelations"
          @selection-change="handleRelationSelectionChange"
          ref="relationTableRef"
          max-height="340"
          row-key="model_id"
        >
          <el-table-column type="selection" width="50" />
          <el-table-column :label="t('graph.importFromModel.relationName')" prop="name" min-width="120" />
          <el-table-column :label="t('graph.importFromModel.source')" prop="source_name" min-width="100" />
          <el-table-column :label="t('graph.importFromModel.target')" prop="target_name" min-width="100" />
          <el-table-column :label="t('graph.importFromModel.directed')" min-width="70">
            <template #default="{ row }">
              <el-icon v-if="row.directed" color="#67c23a"><Check /></el-icon>
            </template>
          </el-table-column>
          <el-table-column :label="t('graph.importFromModel.status')" min-width="100">
            <template #default="{ row }">
              <el-tag v-if="row.exists" type="warning" size="small">{{ t('graph.common.alreadyExists') }}</el-tag>
              <el-tag v-else type="success" size="small">{{ t('graph.common.new') }}</el-tag>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <!-- 冲突策略 -->
      <div class="conflict-row">
        <span class="conflict-label">{{ t('graph.common.exists') }}</span>
        <el-radio-group v-model="conflict" size="small">
          <el-radio-button value="skip">{{ t('graph.common.skip') }}</el-radio-button>
          <el-radio-button value="overwrite">{{ t('graph.common.overwrite') }}</el-radio-button>
        </el-radio-group>
      </div>
    </template>

    <template #footer>
      <el-button @click="visible = false">{{ t('graph.common.cancel') }}</el-button>
      <el-button v-if="step === 1" @click="step = 0">{{ t('graph.common.prevStep') }}</el-button>
      <el-button
        v-if="step === 0"
        type="primary"
        :disabled="selectedEntityIds.length === 0"
        @click="step = 1"
      >{{ t('graph.common.nextStep') }}</el-button>
      <el-button
        v-if="step === 1"
        type="primary"
        :loading="importing"
        @click="handleImport"
      >
        {{ t('graph.importFromModel.importBtn', { entities: selectedEntityIds.length, relations: selectedRelationIds.length }) }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { Loading, Check } from '@element-plus/icons-vue'
import { ontologyAPI } from '../api/ontology'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps({
  ontologyId: { type: Number, required: true }
})
const emit = defineEmits(['imported'])

const visible = ref(false)
const loading = ref(false)
const importing = ref(false)
const step = ref(0)
const conflict = ref('skip')
const preview = ref({ entities: [], relations: [] })
const selectedEntityIds = ref([])
const selectedRelationIds = ref([])
const entityTableRef = ref(null)
const relationTableRef = ref(null)

// 只显示两端实体都被选中的关系
const filteredRelations = computed(() => {
  if (!preview.value.relations) return []
  return preview.value.relations.filter(r =>
    selectedEntityIds.value.includes(r.source_entity_id) &&
    selectedEntityIds.value.includes(r.target_entity_id)
  )
})

function open() {
  visible.value = true
  step.value = 0
}

async function handleOpen() {
  loading.value = true
  selectedEntityIds.value = []
  selectedRelationIds.value = []
  try {
    preview.value = await ontologyAPI.getImportPreviewFromModel()
  } catch (e) {
    ElMessage.error(t('graph.importFromModel.loadFailed', { msg: e.message || e }))
    visible.value = false
  } finally {
    loading.value = false
  }
}

function handleEntitySelectionChange(rows) {
  selectedEntityIds.value = rows.map(r => r.model_id)
}

function handleRelationSelectionChange(rows) {
  selectedRelationIds.value = rows.map(r => r.model_id)
}

function selectAllEntities() {
  entityTableRef.value?.toggleAllSelection()
}

function selectAllRelations() {
  relationTableRef.value?.toggleAllSelection()
}

async function handleImport() {
  importing.value = true
  try {
    const result = await ontologyAPI.importFromModel(props.ontologyId, {
      entity_ids: selectedEntityIds.value,
      relation_ids: selectedRelationIds.value,
      conflict: conflict.value
    })
    const msg = t('graph.importFromModel.importSuccess', { created: result.created, updated: result.updated, skipped: result.skipped })
    if (result.errors?.length) {
      ElMessage.warning(msg + t('graph.importFromModel.importWarning', { count: result.errors.length }))
    } else {
      ElMessage.success(msg)
    }
    visible.value = false
    emit('imported')
  } catch (e) {
    ElMessage.error(t('graph.importFromModel.importFailed', { msg: e.message || e }))
  } finally {
    importing.value = false
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
.step-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
  font-size: 13px;
  color: #606266;
}
.conflict-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid #f0f0f0;
}
.conflict-label {
  font-size: 13px;
  color: #606266;
  white-space: nowrap;
}
</style>
