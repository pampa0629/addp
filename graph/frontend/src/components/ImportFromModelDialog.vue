<template>
  <el-dialog
    v-model="visible"
    title="从 Model 模块导入本体"
    width="800px"
    :close-on-click-modal="false"
    @open="handleOpen"
  >
    <div v-if="loading" class="dialog-loading">
      <el-icon class="is-loading"><Loading /></el-icon>
      <span>加载 Model 数据中...</span>
    </div>

    <template v-else>
      <!-- 步骤指示 -->
      <el-steps :active="step" simple style="margin-bottom: 20px">
        <el-step title="选择实体" />
        <el-step title="选择关系" />
      </el-steps>

      <!-- 步骤 1：选择实体 -->
      <div v-if="step === 0">
        <div class="step-header">
          <span>共 {{ preview.entities?.length || 0 }} 个实体，选择要导入的实体类型</span>
          <div>
            <el-button size="small" @click="selectAllEntities">全选</el-button>
            <el-button size="small" @click="selectedEntityIds = []">清空</el-button>
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
          <el-table-column label="实体名 (name)" prop="name" min-width="120" />
          <el-table-column label="显示名 (label)" prop="label" min-width="120" />
          <el-table-column label="属性数" min-width="80">
            <template #default="{ row }">{{ row.properties?.length || 0 }}</template>
          </el-table-column>
          <el-table-column label="状态" min-width="100">
            <template #default="{ row }">
              <el-tag v-if="row.exists" type="warning" size="small">已存在</el-tag>
              <el-tag v-else type="success" size="small">新增</el-tag>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <!-- 步骤 2：选择关系 -->
      <div v-if="step === 1">
        <div class="step-header">
          <span>共 {{ filteredRelations.length }} 个关系（仅显示两端实体均已选中的关系）</span>
          <div>
            <el-button size="small" @click="selectAllRelations">全选</el-button>
            <el-button size="small" @click="selectedRelationIds = []">清空</el-button>
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
          <el-table-column label="关系名" prop="name" min-width="120" />
          <el-table-column label="来源" prop="source_name" min-width="100" />
          <el-table-column label="目标" prop="target_name" min-width="100" />
          <el-table-column label="有向" min-width="70">
            <template #default="{ row }">
              <el-icon v-if="row.directed" color="#67c23a"><Check /></el-icon>
            </template>
          </el-table-column>
          <el-table-column label="状态" min-width="100">
            <template #default="{ row }">
              <el-tag v-if="row.exists" type="warning" size="small">已存在</el-tag>
              <el-tag v-else type="success" size="small">新增</el-tag>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <!-- 冲突策略 -->
      <div class="conflict-row">
        <span class="conflict-label">已存在时：</span>
        <el-radio-group v-model="conflict" size="small">
          <el-radio-button value="skip">跳过</el-radio-button>
          <el-radio-button value="overwrite">覆盖</el-radio-button>
        </el-radio-group>
      </div>
    </template>

    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button v-if="step === 1" @click="step = 0">上一步</el-button>
      <el-button
        v-if="step === 0"
        type="primary"
        :disabled="selectedEntityIds.length === 0"
        @click="step = 1"
      >下一步</el-button>
      <el-button
        v-if="step === 1"
        type="primary"
        :loading="importing"
        @click="handleImport"
      >
        导入 ({{ selectedEntityIds.length }} 实体 + {{ selectedRelationIds.length }} 关系)
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { Loading, Check } from '@element-plus/icons-vue'
import { ontologyAPI } from '../api/ontology'

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
    ElMessage.error('获取 Model 数据失败：' + (e.message || e))
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
    const msg = `导入完成：新增 ${result.created}，更新 ${result.updated}，跳过 ${result.skipped}`
    if (result.errors?.length) {
      ElMessage.warning(msg + `，${result.errors.length} 个失败`)
    } else {
      ElMessage.success(msg)
    }
    visible.value = false
    emit('imported')
  } catch (e) {
    ElMessage.error('导入失败：' + (e.message || e))
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
