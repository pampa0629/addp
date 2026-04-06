<template>
  <el-dialog
    v-model="visible"
    title="从 Neo4j 图谱推导本体"
    width="820px"
    :close-on-click-modal="false"
    @open="handleOpen"
  >
    <div v-if="loading" class="dialog-loading">
      <el-icon class="is-loading"><Loading /></el-icon>
      <span>推导中，正在分析图谱 Schema...</span>
    </div>

    <template v-else-if="preview">
      <el-alert
        v-if="preview.entity_types?.length === 0 && preview.relation_types?.length === 0"
        type="warning"
        title="图谱中未发现任何节点标签或关系类型"
        :closable="false"
        style="margin-bottom: 16px"
      />

      <div v-else>
        <div class="summary-bar">
          发现 <strong>{{ preview.entity_types?.length || 0 }}</strong> 个标签，
          <strong>{{ preview.relation_types?.length || 0 }}</strong> 种关系类型
        </div>

        <!-- 实体类型 -->
        <div class="section-title">建议添加的实体类型</div>
        <el-table
          :data="preview.entity_types"
          @selection-change="handleEntitySelectionChange"
          ref="entityTableRef"
          max-height="220"
          row-key="label"
          size="small"
        >
          <el-table-column type="selection" width="50" />
          <el-table-column label="标签名" prop="label" min-width="120" />
          <el-table-column label="节点数" prop="count" min-width="80" />
          <el-table-column label="采样属性" min-width="200">
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
          <el-table-column label="状态" min-width="90">
            <template #default="{ row }">
              <el-tag v-if="row.exists" type="warning" size="small">已存在</el-tag>
              <el-tag v-else type="success" size="small">新增</el-tag>
            </template>
          </el-table-column>
        </el-table>

        <!-- 关系类型 -->
        <div class="section-title" style="margin-top: 16px">建议添加的关系类型</div>
        <el-table
          :data="preview.relation_types"
          @selection-change="handleRelationSelectionChange"
          ref="relationTableRef"
          max-height="180"
          row-key="name"
          size="small"
        >
          <el-table-column type="selection" width="50" />
          <el-table-column label="关系类型" prop="name" min-width="140" />
          <el-table-column label="来源" prop="source_label" min-width="100" />
          <el-table-column label="目标" prop="target_label" min-width="100" />
          <el-table-column label="条数" prop="count" min-width="80" />
          <el-table-column label="状态" min-width="90">
            <template #default="{ row }">
              <el-tag v-if="row.exists" type="warning" size="small">已存在</el-tag>
              <el-tag v-else type="success" size="small">新增</el-tag>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <!-- 应用到本体 -->
      <div class="apply-row">
        <span class="apply-label">应用到本体：</span>
        <el-select v-model="targetOntologyId" placeholder="选择目标本体" style="width: 220px">
          <el-option
            v-for="o in ontologies"
            :key="o.id"
            :label="o.name"
            :value="o.id"
          />
        </el-select>
        <span class="conflict-label">已存在时：</span>
        <el-radio-group v-model="conflict" size="small">
          <el-radio-button value="skip">跳过</el-radio-button>
          <el-radio-button value="overwrite">覆盖</el-radio-button>
        </el-radio-group>
      </div>
    </template>

    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button
        type="primary"
        :loading="applying"
        :disabled="!canApply"
        @click="handleApply"
      >
        应用选中项
        ({{ selectedEntityLabels.length }} 实体 + {{ selectedRelationNames.length }} 关系)
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
const selectedEntityLabels = ref([])
const selectedRelationNames = ref([])
const entityTableRef = ref(null)
const relationTableRef = ref(null)

const canApply = computed(() =>
  targetOntologyId.value &&
  (selectedEntityLabels.value.length > 0 || selectedRelationNames.value.length > 0)
)

function open() {
  visible.value = true
}

async function handleOpen() {
  loading.value = true
  preview.value = null
  selectedEntityLabels.value = []
  selectedRelationNames.value = []
  try {
    const [previewData, ontologyList] = await Promise.all([
      browseAPI.inferSchema(props.graphId, targetOntologyId.value),
      ontologyAPI.list()
    ])
    preview.value = previewData
    ontologies.value = Array.isArray(ontologyList) ? ontologyList : (ontologyList?.data || [])
  } catch (e) {
    ElMessage.error('推导失败：' + (e.message || e))
    visible.value = false
  } finally {
    loading.value = false
  }
}

function handleEntitySelectionChange(rows) {
  selectedEntityLabels.value = rows.map(r => r.label)
}

function handleRelationSelectionChange(rows) {
  selectedRelationNames.value = rows.map(r => r.name)
}

async function handleApply() {
  applying.value = true
  try {
    const result = await browseAPI.applyInferredSchema(props.graphId, {
      ontology_id: targetOntologyId.value,
      entity_type_names: selectedEntityLabels.value,
      relation_type_names: selectedRelationNames.value,
      conflict: conflict.value
    })
    const msg = `应用完成：新增 ${result.created}，更新 ${result.updated}，跳过 ${result.skipped}`
    if (result.errors?.length) {
      ElMessage.warning(msg + `，${result.errors.length} 个失败`)
    } else {
      ElMessage.success(msg)
    }
    visible.value = false
    emit('applied')
  } catch (e) {
    ElMessage.error('应用失败：' + (e.message || e))
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
