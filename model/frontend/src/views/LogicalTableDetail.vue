<template>
  <div class="logical-table-detail">
    <!-- 顶部操作栏 -->
    <div class="detail-header">
      <div class="header-left">
        <el-button text @click="backToList">
          <el-icon><ArrowLeft /></el-icon>
          {{ t('model.common.back') }}
        </el-button>
        <span class="table-name">{{ table.name || t('model.logical_table.detail') }}</span>
        <el-tag v-if="table.status" :type="statusTagType(table.status)" size="small">
          {{ statusLabel(table.status) }}
        </el-tag>
        <el-tag v-if="isDirty" type="warning" size="small">{{ t('model.common.unsaved') }}</el-tag>
      </div>
      <div v-if="!pageLoading && !pageError" class="header-right">
        <el-button :title="t('model.common.refresh')" :aria-label="t('model.common.refresh')" @click="handleRefresh">
          <el-icon><Refresh /></el-icon>
        </el-button>
        <el-button v-if="canEdit && (detailRouteState.tab === 'definition' || detailRouteState.tab === 'physical-target')" type="primary" @click="handleSave" :loading="saving">{{ t('model.common.save') }}</el-button>
        <el-button v-if="table.status === 'draft' && authStore.hasPermission('model.logical_model.update')" type="success" @click="handleApprove">
          {{ t('model.common.approve') }}
        </el-button>
        <el-button v-if="table.status === 'approved' && authStore.hasPermission('model.logical_model.update')"  :loading="reopening" @click="handleReopen">
          {{ t('model.common.reopen') }}
        </el-button>
      </div>
    </div>

    <el-skeleton v-if="pageLoading" :rows="8" animated />
    <el-result
      v-else-if="pageError"
      icon="error"
      :title="t('model.common.load_failed')"
      :sub-title="pageError"
    >
      <template #extra>
        <el-button type="primary" @click="loadPage">{{ t('model.common.retry') }}</el-button>
      </template>
    </el-result>

    <template v-else>

    <el-alert
      v-if="table.status === 'approved'"
      class="reference-warning"
      type="info"
      :title="t('model.logical_table.approved_help')"
      :closable="false"
      show-icon
    />


    <el-alert
      v-if="referenceError"
      class="reference-warning"
      type="warning"
      :title="referenceError"
      show-icon
      :closable="false"
    />

    <el-tabs :model-value="detailRouteState.tab" @tab-change="changeDetailTab">
    <el-tab-pane name="definition" :label="t('model.table_relation.definition')">
    <el-row :gutter="16">
      <!-- 基本信息 -->
      <el-col :span="24">
        <el-card shadow="never" class="info-card">
          <template #header><span class="card-title">{{ t('model.logical_table.basic_info') }}</span></template>
          <el-form :model="form" label-width="100px">
            <el-row :gutter="16">
              <el-col :xs="24" :md="12">
                <el-form-item :label="t('model.logical_table.name')">
                  <el-input v-model="form.name" maxlength="200" :disabled="!canEdit" />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :md="12">
                <el-form-item :label="t('model.logical_table.code')">
                  <el-input :value="table.code" disabled />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12" :md="8">
                <el-form-item :label="t('model.entity.domain')">
                  <BusinessDomainSelect v-model="form.domain_id" :disabled="!canEdit" clearable style="width:100%" :options="domains" />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12" :md="8">
                <el-form-item :label="t('model.logical_table.table_type')">
                  <el-select v-model="form.table_type" :disabled="!canEdit" style="width:100%">
                    <el-option :label="t('model.logical_table.type_entity')" value="entity" />
                    <el-option :label="t('model.logical_table.type_fact')" value="fact" />
                    <el-option :label="t('model.logical_table.type_dimension')" value="dimension" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12" :md="8">
                <el-form-item :label="t('model.logical_table.layer')">
                  <el-select v-model="form.layer" :disabled="!canEdit" style="width:100%">
                    <el-option v-for="layer in layers" :key="layer.layer_code" :label="layer.layer_name" :value="layer.layer_code" />
                  </el-select>
                </el-form-item>
              </el-col>
              <!-- 事实表专属：粒度声明 -->
              <el-col v-if="form.table_type === 'fact'" :span="24">
                <el-form-item :label="t('model.logical_table.grain_description')">
                  <el-input
                    v-model="form.grain_description"
                    type="textarea"
                    :rows="2"
                    :disabled="!canEdit"
                    :placeholder="t('model.logical_table.grain_placeholder')"
                  />
                </el-form-item>
              </el-col>
              <!-- 维度表专属：历史保留声明 -->
              <el-col v-if="form.table_type === 'dimension'" :xs="24" :md="16">
                <el-form-item :label="t('model.logical_table.scd_type')">
                  <el-select v-model="form.scd_type" :disabled="!canEdit" style="width:100%">
                    <el-option :label="t('model.logical_table.scd_0')" :value="0" />
                    <el-option :label="t('model.logical_table.scd_1')" :value="1" />
                    <el-option :label="t('model.logical_table.scd_2')" :value="2" />
                    <el-option :label="t('model.logical_table.scd_3')" :value="3" />
                  </el-select>
                  <span class="text-muted">{{ t('model.logical_table.scd_help') }}</span>
                </el-form-item>
              </el-col>
              <el-col :span="24">
                <el-form-item :label="t('model.entity.description')">
                  <el-input v-model="form.description" type="textarea" :rows="2" :disabled="!canEdit" />
                </el-form-item>
              </el-col>
            </el-row>
          </el-form>
        </el-card>
      </el-col>

      <!-- 字段定义 -->
      <el-col :span="24" style="margin-top:16px">
        <el-card shadow="never">
          <template #header>
            <div class="card-header-with-action">
              <span class="card-title">{{ t('model.field.title') }}</span>
              <el-button v-if="canCreateField" type="primary" size="small" @click="openFieldDialog()">
                <el-icon><Plus /></el-icon>
                {{ t('model.field.add') }}
              </el-button>
            </div>
          </template>

          <el-table :data="fields" v-loading="fieldLoading" stripe>
            <el-table-column :label="t('model.field.index')" type="index" width="60" />
            <el-table-column :label="t('model.field.name')" prop="name" min-width="120" />
            <el-table-column :label="t('model.field.column_name')" prop="column_name" min-width="140" />
            <el-table-column :label="t('model.field.data_type')" prop="data_type" width="110">
              <template #default="{ row }">
                <el-tag type="info" size="small">
                  {{ row.data_type.toUpperCase() }}{{ row.length ? `(${row.length})` : '' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('model.field.field_role')" width="130">
              <template #default="{ row }">
                <el-tag :type="fieldRoleTagType(row.field_role)" size="small">
                  {{ fieldRoleLabel(row.field_role) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('model.field.element')" min-width="190">
              <template #default="{ row }">
                <span>{{ getElementName(row) }}</span>
                <FrozenElementLink :binding="row" :label="t('model.field.element_revision')" />
              </template>
            </el-table-column>
            <el-table-column :label="t('model.field.constraints')" width="140">
              <template #default="{ row }">
                <el-tag v-if="row.is_pk" type="warning" size="small">PK</el-tag>
                <el-tag v-if="!row.nullable" type="danger" size="small">NOT NULL</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('model.field.description')" prop="description" show-overflow-tooltip />
            <el-table-column :label="t('model.field.actions')" width="130" fixed="right">
              <template #default="{ row }">
                <el-button v-if="canEdit && authStore.hasPermission('model.logical_model.update')" link type="primary" @click="openFieldDialog(row)">{{ t('model.common.edit') }}</el-button>
                <el-popconfirm v-if="canDeleteField" :title="t('model.field.delete_confirm')" @confirm="deleteField(row.id)">
                  <template #reference>
                    <el-button link type="danger">{{ t('model.common.delete') }}</el-button>
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>

      <el-col :span="24" class="structural-constraints-section">
        <LogicalTableConstraints :table-id="tableId" :version="table.version" />
      </el-col>

      <!-- 维度层级（仅维度表） -->
      <el-col v-if="form.table_type === 'dimension'" :span="24" style="margin-top:16px">
        <DimensionHierarchyEditor
          :table-id="tableId"
          :version="table.version"
          :fields="fields"
          :editable="canEdit && authStore.hasPermission('model.logical_model.update')"
          @update-version="table.version = $event"
        />
      </el-col>

      <el-col v-if="form.table_type === 'fact'" :span="24" style="margin-top:16px">
        <MetricImplementationLinks :table-id="tableId" />
      </el-col>

    </el-row>
    </el-tab-pane>
    <el-tab-pane name="physical-target" :label="t('model.materialization.title')">
      <el-card shadow="never">
        <template #header>
          <div class="card-header-with-action">
            <span class="card-title">{{ t('model.materialization.title') }}</span>
            <div class="card-header-actions">
              <el-button v-if="canClearMaterialization" plain @click="clearMaterializationConfig">
                {{ t('model.materialization.clear_config') }}
              </el-button>
              <el-button v-if="authStore.hasPermission('model.logical_model.read')" @click="handlePreviewDDL">
                <el-icon><View /></el-icon>
                {{ t('model.logical_table.preview_ddl') }}
              </el-button>
              <el-button
                v-if="table.status === 'approved' && authStore.hasPermission('model.materialization.execute')"
                type="primary"
                :disabled="isDirty || !hasConfiguredPhysicalTarget"
                :loading="creatingTarget"
                @click="createTarget"
              >
                {{ t('model.materialization.create_target') }}
              </el-button>
              <MonitorExecutionsButton module="model" task-type="logical_table_materialization" :source-task-id="tableId" scope="task" />
              <el-button v-if="canDecommissionTarget" type="danger" plain @click="openDecommissionDialog">
                {{ t('model.materialization.decommission') }}
              </el-button>
            </div>
          </div>
        </template>

        <el-alert
          class="physical-target-help"
          type="info"
          :title="t('model.materialization.help')"
          :closable="false"
          show-icon
        />

        <el-descriptions class="physical-target-summary" :column="2" border>
          <el-descriptions-item :label="t('model.materialization.config_status')">
            <el-tag :type="hasConfiguredPhysicalTarget ? 'success' : 'info'">
              {{ t(hasConfiguredPhysicalTarget ? 'model.materialization.configured' : 'model.materialization.not_configured') }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('model.materialization.engine')">
            <template v-if="physicalTargetEngine">
              {{ physicalTargetEngine.name }}
              <span class="engine-type">({{ physicalTargetEngine.engine_type }})</span>
            </template>
            <span v-else>{{ hasConfiguredPhysicalTarget ? t('model.materialization.engine_unknown') : '-' }}</span>
          </el-descriptions-item>
          <el-descriptions-item :label="t('model.materialization.target_location')">
            {{ formatLocatorDisplayPath(materializationForm.target_parent_locator) || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('model.materialization.target_name')">
            {{ materializationForm.target_name || '-' }}
          </el-descriptions-item>
        </el-descriptions>

        <el-form :model="materializationForm" label-width="110px">
          <el-row :gutter="16">
            <el-col :xs="24" :lg="16">
              <el-form-item :label="t('model.materialization.target_location')">
                <ResourceTreePicker
                  v-if="canEdit"
                  v-model="targetParentSelection"
                  api-base-url="/api/v1/meta"
                  mode="node"
                  :engine-filter="isSupportedPhysicalTargetEngine"
                  :selectable-filter="isSchemaSelection"
                  :initial-locator="materializationForm.target_parent_locator"
                  :show-selection-summary="true"
                  :show-count="false"
                  tree-height="260px"
                  @select="handleTargetParentSelect"
                />
                <el-input
                  v-else
                  :model-value="formatLocatorDisplayPath(materializationForm.target_parent_locator)"
                  :placeholder="t('model.materialization.not_configured')"
                  disabled
                />
              </el-form-item>
            </el-col>
            <el-col :xs="24" :lg="8">
              <el-form-item :label="t('model.materialization.target_name')">
                <el-input v-model="materializationForm.target_name" :disabled="!canEdit" :placeholder="t('model.materialization.target_name_placeholder')" />
              </el-form-item>
            </el-col>
          </el-row>
        </el-form>
      </el-card>
    </el-tab-pane>
    <el-tab-pane name="concept-mappings" :label="t('model.concept_mapping.tab')">
      <ConceptMappingEditor
        v-if="detailRouteState.tab === 'concept-mappings'"
        :key="`${tableId}-${table.version}`"
        :table="table"
        :fields="fields"
        :editable="canEdit"
        @dirty-change="conceptMappingDirty = $event"
        @update-version="table.version = $event"
      />
    </el-tab-pane>
    <el-tab-pane v-if="table.table_type === 'fact' || table.table_type === 'dimension'" name="relations" :label="t(table.table_type === 'fact' ? 'model.table_relation.title' : 'model.table_relation.incoming')">
      <TableRelationEditor v-if="detailRouteState.tab === 'relations'" :key="`${tableId}-${detailRouteState.relationId || ''}`" :table="table" :fields="fields" :editable="canEdit" :relation-id="detailRouteState.relationId" :confirm-discard="confirmDiscardState" @dirty-change="relationDirty = $event" @update-version="table.version = $event" @relation-removed="clearRemovedRelation" />
    </el-tab-pane>
    </el-tabs>

    <!-- 字段对话框 -->
    <el-dialog
      v-model="fieldDialogVisible"
      :before-close="closeFieldDialog"
      :close-on-click-modal="false"
      class="addp-dialog"
      :title="editingField ? t('model.field.edit') : t('model.field.add')"
      width="min(580px, calc(100vw - 32px))"
    >
      <el-form ref="fieldFormRef" :model="fieldForm" :rules="fieldRules" label-width="110px">
        <el-form-item :label="t('model.field.display_name')" prop="name">
          <el-input v-model="fieldForm.name" maxlength="200" :placeholder="t('model.field.display_name_placeholder')" />
        </el-form-item>
        <el-form-item :label="t('model.field.column_name')" prop="column_name">
          <el-input v-model="fieldForm.column_name" maxlength="200" :placeholder="t('model.field.column_name_placeholder')" />
        </el-form-item>
        <el-row :gutter="16">
          <el-col :xs="24" :md="12">
            <el-form-item :label="t('model.field.data_type')" prop="data_type">
              <el-select v-model="fieldForm.data_type" style="width:100%">
                <el-option label="string" value="string" />
                <el-option label="int" value="int" />
                <el-option label="bigint" value="bigint" />
                <el-option label="float" value="float" />
                <el-option label="decimal" value="decimal" />
                <el-option label="date" value="date" />
                <el-option label="datetime" value="datetime" />
                <el-option label="bool" value="bool" />
                <el-option label="json" value="json" />
                <el-option label="geometry" value="geometry" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :md="12">
            <el-form-item :label="t('model.field.length')">
              <el-input-number
                v-model="fieldForm.length"
                :min="1"
                :disabled="!['string'].includes(fieldForm.data_type)"
                style="width:100%"
              />
            </el-form-item>
          </el-col>
        </el-row>
        <!-- 字段角色 -->
        <el-form-item :label="t('model.field.field_role')">
          <el-select v-model="fieldForm.field_role" style="width:100%">
            <el-option :label="t('model.field.role_regular')" value="regular" />
            <el-option v-if="form.table_type === 'fact'" :label="t('model.field.role_measure_additive')" value="measure_additive" />
            <el-option v-if="form.table_type === 'fact'" :label="t('model.field.role_measure_semi')" value="measure_semi" />
            <el-option v-if="form.table_type === 'fact'" :label="t('model.field.role_measure_non')" value="measure_non" />
            <el-option v-if="form.table_type === 'fact'" :label="t('model.field.role_dimension_fk')" value="dimension_fk" />
            <el-option v-if="form.table_type === 'fact'" :label="t('model.field.role_degenerate_dim')" value="degenerate_dim" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('model.field.element')">
          <el-select
            v-model="fieldForm.element_id"
            :placeholder="t('model.field.element_placeholder')"
            clearable
            filterable
            style="width:100%"
            @change="handleElementChange"
          >
            <el-option
              v-for="e in elementOptions"
              :key="e.id"
              :label="e.label"
              :value="e.id"
              :disabled="e.disabled"
            />
          </el-select>
        </el-form-item>
        <el-row :gutter="8">
          <el-col :span="12">
            <el-form-item :label="t('model.field.is_pk')">
              <el-switch v-model="fieldForm.is_pk" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item :label="t('model.field.nullable')">
              <el-switch v-model="fieldForm.nullable" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item :label="t('model.field.default_value')">
          <el-input v-model="fieldForm.default_value" :placeholder="t('model.field.default_value_placeholder')" />
        </el-form-item>
        <el-form-item :label="t('model.field.description')">
          <el-input v-model="fieldForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="closeFieldDialog">{{ t('model.common.cancel') }}</el-button>
        <el-button type="primary" @click="handleFieldSubmit" :loading="fieldSubmitting">
          {{ editingField ? t('model.common.save') : t('model.common.add') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 指标实现对话框 -->


    <!-- 建表语句预览对话框 -->
    <DDLPreviewDialog v-model="ddlDialogVisible" :ddl="ddlContent" />

    <el-dialog
      v-model="decommissionDialogVisible"
      class="addp-dialog"
      :title="t('model.materialization.decommission_title')"
      width="min(620px, calc(100vw - 32px))"
    >
      <el-alert
        type="error"
        :title="t('model.materialization.decommission_warning')"
        :closable="false"
        show-icon
      />
      <el-descriptions class="decommission-target" :column="1" border>
        <el-descriptions-item :label="t('model.materialization.engine')">
          {{ decommissionTarget.engineName || t('model.materialization.engine_unknown') }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('model.materialization.target_location')">
          {{ decommissionTarget.locationName }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('model.materialization.target_name')">
          {{ decommissionTarget.targetName }}
        </el-descriptions-item>
      </el-descriptions>
      <el-form label-position="top">
        <el-form-item :label="t('model.materialization.confirm_label', { target: decommissionTarget.confirmation })">
          <el-input
            v-model="decommissionConfirmation"
            autocomplete="off"
            :placeholder="decommissionTarget.confirmation"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="decommissionDialogVisible = false">{{ t('model.common.cancel') }}</el-button>
        <el-button
          type="danger"
          :loading="decommissioning"
          :disabled="decommissionConfirmation !== decommissionTarget.confirmation"
          @click="handleDecommission"
        >
          {{ t('model.materialization.confirm_decommission') }}
        </el-button>
      </template>
    </el-dialog>
    </template>
  </div>
</template>

<script setup>
import { BusinessDomainSelect, buildBusinessDomainOptions } from '@common-ui'
import { ref, reactive, computed, watch } from 'vue'
import { useRoute, useRouter, onBeforeRouteUpdate } from 'vue-router'
import { MonitorExecutionsButton, ResourceTreePicker, listResourceTreeEngines, parseLocator, parseLocatorSafe, isLocatorEqual, formatLocatorDisplayPath, useConsolePageDescriptor } from '@common-ui'
import { ElMessage } from 'element-plus'
import { ArrowLeft, Plus, Refresh, View } from '@element-plus/icons-vue'
import { logicalTableAPI, domainAPI, elementAPI, dwLayerAPI } from '../api/model'
import DDLPreviewDialog from '../components/DDLPreviewDialog.vue'
import DimensionHierarchyEditor from '../components/DimensionHierarchyEditor.vue'
import TableRelationEditor from '../components/TableRelationEditor.vue'
import ConceptMappingEditor from '../components/ConceptMappingEditor.vue'
import MetricImplementationLinks from '../components/MetricImplementationLinks.vue'
import LogicalTableConstraints from '../components/LogicalTableConstraints.vue'
import FrozenElementLink from '../components/FrozenElementLink.vue'
import { useI18n } from 'vue-i18n'
import { confirmReturnToDraft } from '../utils/lifecycleActions'
import { navigateModelRoute } from '../utils/moduleNavigation'
import { useAuthStore } from '../store/auth'
import { resolveLogicalTableListRouteState, resolveLogicalTableDetailRouteState } from '../utils/routeState'
import { getModelErrorMessage } from '../utils/apiError'
import {
  applyModelElementToField,
  buildModelElementOptions,
  getModelElementName,
  loadModelElementReferences,
  buildDDLPreviewRequest,
  buildLogicalFieldUpdateRequest,
  buildLogicalTableUpdateRequest,
  canPerformDraftAction,
  isEditableDraft,
  resolvePositiveRouteId,
  snapshotUnsavedState
} from '../utils/modelDetailState'
import { useUnsavedChanges } from '../composables/useUnsavedChanges'

const { t } = useI18n()

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const tableId = computed(() => resolvePositiveRouteId(route.params.id))

const backToList = () => navigateModelRoute(router, {
  path: '/logical-tables',
  query: resolveLogicalTableListRouteState(route.query).query
}, { history: 'replace' })

const saving = ref(false)
const pageLoading = ref(false)
const pageError = ref('')
const referenceError = ref('')
const fieldLoading = ref(false)
const fieldSubmitting = ref(false)
const fieldDialogVisible = ref(false)
const ddlDialogVisible = ref(false)
const decommissionDialogVisible = ref(false)
const decommissionConfirmation = ref('')
const decommissioning = ref(false)
const editingField = ref(null)
const fieldFormRef = ref(null)

const table = ref({})
const relationDirty = ref(false)
const conceptMappingDirty = ref(false)
const detailRouteState = computed(() => resolveLogicalTableDetailRouteState(route.query, table.value.table_type))
const changeDetailTab = tab => {
  if (tab === detailRouteState.value.tab) return
  const query = resolveLogicalTableDetailRouteState({ ...route.query, tab, relation_id: null }, table.value.table_type).query
  navigateModelRoute(router, { path: route.path, query }, { history: 'replace' })
}
const clearRemovedRelation = id => {
  if (detailRouteState.value.relationId !== id) return
  const query = { ...detailRouteState.value.query }
  delete query.relation_id
  navigateModelRoute(router, { path: route.path, query }, { history: 'replace' })
}

useConsolePageDescriptor(router, 'modeling', {
  title: computed(() => t('model.logical_table.recentVisitTitle')),
  subject: computed(() => table.value?.name || ''),
  ready: computed(() => Boolean(table.value?.name))
})
const canEdit = computed(() => isEditableDraft(table.value.status, authStore.hasPermission('model.logical_model.update')))
const canCreateField = computed(() => canPerformDraftAction(
  table.value.status,
  authStore.hasPermission('model.logical_model.create')
))
const canDeleteField = computed(() => canPerformDraftAction(
  table.value.status,
  authStore.hasPermission('model.logical_model.delete')
))
const form = reactive({
  name: '', domain_id: null, table_type: 'entity', layer: '',
  grain_description: '', scd_type: 0, description: ''
})
const materializationForm = reactive({
  target_parent_locator: '', target_name: ''
})
const targetParentSelection = ref(null)
const physicalTargetEngines = ref([])
const fields = ref([])
const domains = ref([])
const layers = ref([])
const elements = ref([])
const elementRevisions = ref(new Map())
const elementOptions = computed(() => buildModelElementOptions(elements.value, fieldForm.element_id))
const getElementName = row => row.element_id
  ? getModelElementName(row, elements.value, elementRevisions.value) || t('model.common.element_unavailable')
  : '-'
const ddlContent = ref('')

const isSupportedPhysicalTargetEngine = engine =>
  ['postgresql', 'postgres', 'postgis'].includes(String(engine?.engine_type || '').toLowerCase())

const currentTargetLocator = computed(() => parseLocatorSafe(materializationForm.target_parent_locator))
const hasConfiguredPhysicalTarget = computed(() => Boolean(
  String(materializationForm.target_parent_locator || '').trim() &&
  String(materializationForm.target_name || '').trim()
))
const physicalTargetEngine = computed(() => physicalTargetEngines.value.find(
  engine => Number(engine.id) === Number(currentTargetLocator.value.engineId)
))

const decommissionTarget = computed(() => {
  const materialization = table.value?.materialization || {}
  const locator = materialization.target_parent_locator || ''
  let parsed = {}
  try {
    parsed = parseLocator(locator)
  } catch {
    parsed = {}
  }
  const locationName = parsed.path?.join('/') || ''
  const targetName = materialization.target_name || ''
  const engine = physicalTargetEngines.value.find(item => Number(item.id) === Number(parsed.engineId))
  return {
    locator,
    locationName,
    targetName,
    engineName: engine?.name || '',
    confirmation: locationName && targetName ? `${locationName}.${targetName}` : ''
  }
})
const canDecommissionTarget = computed(() =>
  table.value.status === 'approved' &&
  authStore.hasPermission('model.materialized_target.delete') &&
  Boolean(decommissionTarget.value.locator && decommissionTarget.value.targetName)
)
const canClearMaterialization = computed(() => canEdit.value && Boolean(
  String(materializationForm.target_parent_locator || '').trim() ||
  String(materializationForm.target_name || '').trim()
))

const fieldForm = reactive({
  name: '', column_name: '', data_type: 'string', length: null,
  nullable: true, is_pk: false,
  default_value: '', element_id: null, description: '',
  field_role: 'regular', sort_order: 0
})
const fieldRules = {
  name: [{ required: true, message: t('model.field.name_required'), trigger: 'blur' }],
  column_name: [{ required: true, message: t('model.field.column_required'), trigger: 'blur' }],
  data_type: [{ required: true, message: t('model.field.type_required'), trigger: 'change' }]
}

const fieldBaseline = ref('')
const fieldDirty = computed(() => fieldDialogVisible.value && snapshotUnsavedState(fieldForm) !== fieldBaseline.value)
const unsavedState = computed(() => ({
  form: { ...form },
  materialization: buildDDLPreviewRequest(materializationForm).materialization
}))
const { isDirty, markSaved, confirmDiscardChanges, confirmDiscardState } = useUnsavedChanges({
  state: unsavedState, t, additionalDirty: computed(() => fieldDirty.value || relationDirty.value || conceptMappingDirty.value)
})
onBeforeRouteUpdate((to, from) => {
  if (to.params.id === from.params.id && (to.query.tab !== from.query.tab || to.query.relation_id !== from.query.relation_id)) {
    return confirmDiscardState(relationDirty.value || conceptMappingDirty.value)
  }
  return true
})
const closeFieldDialog = async () => {
  if (await confirmDiscardState(fieldDirty.value)) fieldDialogVisible.value = false
}


const statusTagType = (s) => ({ draft: 'info', approved: 'success' }[s] ?? 'info')
const statusLabel = (s) => ({
  draft: t('model.common.status_draft'),
  approved: t('model.common.status_approved'),
}[s] ?? s)

const fieldRoleTagType = (role) => {
  const map = {
    regular: 'info',
    measure_additive: 'success',
    measure_semi: 'warning',
    measure_non: 'danger',
    dimension_fk: 'primary',
    degenerate_dim: ''
  }
  return map[role] ?? 'info'
}

const fieldRoleLabel = (role) => {
  const map = {
    regular: t('model.field.role_label_regular'),
    measure_additive: t('model.field.role_label_additive'),
    measure_semi: t('model.field.role_label_semi'),
    measure_non: t('model.field.role_label_non'),
    dimension_fk: t('model.field.role_label_fk'),
    degenerate_dim: t('model.field.role_label_degenerate')
  }
  return (map[role] ?? role) || t('model.field.role_label_regular')
}

const applyTable = resource => {
  table.value = resource || {}
  Object.assign(form, {
    name: table.value.name,
    domain_id: table.value.domain_id,
    table_type: table.value.table_type,
    layer: table.value.layer || '',
    grain_description: table.value.grain_description || '',
    scd_type: table.value.scd_type ?? 0,
    description: table.value.description || ''
  })

  const mat = table.value.materialization || {}
  Object.assign(materializationForm, {
    target_parent_locator: mat.target_parent_locator || '',
    target_name: mat.target_name || ''
  })
  targetParentSelection.value = null
}

const isSchemaSelection = (node, { locator }) => node?.type === 'schema' && locator.type === 'schema' && !locator.itemId

const handleTargetParentSelect = selection => {
  if (!canEdit.value || !selection?.identity?.locator) return
  const next = parseLocatorSafe(selection.identity.locator)
  if (next.type !== 'schema' || next.itemId) return
  const current = parseLocatorSafe(materializationForm.target_parent_locator)
  if (!isLocatorEqual(current, next)) materializationForm.target_parent_locator = selection.identity.locator
}

const clearMaterializationConfig = () => {
  targetParentSelection.value = null
  Object.assign(materializationForm, {
    target_parent_locator: '',
    target_name: ''
  })
}

const loadTable = async () => applyTable(await logicalTableAPI.get(tableId.value))

const loadFields = async () => {
  fieldLoading.value = true
  try {
    const res = await logicalTableAPI.getFields(tableId.value)
    fields.value = res || []
  } finally {
    fieldLoading.value = false
  }
}

const handleRefresh = async () => {
  if (await confirmDiscardChanges()) await loadPage()
}

const handleSave = async () => {
  if (!canEdit.value) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  saving.value = true
  try {
    const updateData = buildLogicalTableUpdateRequest(form, table.value, materializationForm)
    const updated = await logicalTableAPI.update(tableId.value, updateData)
    applyTable(updated)
    markSaved()
    ElMessage.success(t('model.common.save_success'))
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.save_failed'))
  } finally {
    saving.value = false
  }
}

const handleApprove = async () => {
  if (!authStore.hasPermission('model.logical_model.update')) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  if (isDirty.value) {
    ElMessage.warning(t('model.common.save_before_action'))
    return
  }
  try {
    await logicalTableAPI.approve(tableId.value, table.value.version)
    await loadPage()
    ElMessage.success(t('model.common.approve_success'))
  }
  catch (err) { ElMessage.error(getModelErrorMessage(err, t, 'model.common.op_failed')) }
}

const creatingTarget = ref(false)
const createTarget = async () => {
  if (creatingTarget.value || isDirty.value) return
  creatingTarget.value = true
  try { await logicalTableAPI.createTarget(tableId.value, table.value.version); ElMessage.success(t('model.materialization.target_submitted')) }
  catch(error) { ElMessage.error(getModelErrorMessage(error, t, 'model.common.op_failed')) }
  finally { creatingTarget.value = false }
}
const reopening = ref(false)
const handleReopen = async () => {
  if (reopening.value) return
  if (!authStore.hasPermission('model.logical_model.update')) return
  reopening.value = true
  try {
    await confirmReturnToDraft(t)
    await logicalTableAPI.reopen(tableId.value, table.value.version)
    await loadPage()
    ElMessage.success(t('model.common.reopen_success'))
  } catch (err) {
    if (err === 'cancel' || err === 'close') return
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.op_failed'))
  } finally {
    reopening.value = false
  }
}

const handlePreviewDDL = async () => {
  if (!authStore.hasPermission('model.logical_model.read')) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  try {
    const res = await logicalTableAPI.previewDDL(tableId.value, buildDDLPreviewRequest(materializationForm))
    ddlContent.value = res.ddl || ''
    ddlDialogVisible.value = true
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.logical_table.ddl_failed'))
  }
}

const openDecommissionDialog = () => {
  if (!canDecommissionTarget.value) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  if (isDirty.value) {
    ElMessage.warning(t('model.common.save_before_action'))
    return
  }
  decommissionConfirmation.value = ''
  decommissionDialogVisible.value = true
}

const handleDecommission = async () => {
  if (decommissionConfirmation.value !== decommissionTarget.value.confirmation) return
  decommissioning.value = true
  try {
    await logicalTableAPI.decommissionMaterializedTarget(tableId.value, {
      version: table.value.version,
      target_parent_locator: decommissionTarget.value.locator,
      target_name: decommissionTarget.value.targetName
    })
    decommissionDialogVisible.value = false
    ElMessage.success(t('model.materialization.decommission_success'))
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.materialization.decommission_failed'))
  } finally {
    decommissioning.value = false
  }
}

const openFieldDialog = (field = null) => {
  editingField.value = field
  if (field) {
    Object.assign(fieldForm, {
      name: field.name,
      column_name: field.column_name,
      data_type: field.data_type,
      length: field.length ?? null,
      nullable: field.nullable,
      is_pk: field.is_pk,
      default_value: field.default_value || '',
      element_id: field.element_id ?? null,
      description: field.description || '',
      field_role: field.field_role || 'regular',
      sort_order: field.sort_order ?? 0
    })
  } else {
    Object.assign(fieldForm, {
      name: '', column_name: '', data_type: 'string', length: null,
      nullable: true, is_pk: false,
      default_value: '', element_id: null, description: '',
      field_role: 'regular', sort_order: 0
    })
  }
  fieldBaseline.value = snapshotUnsavedState(fieldForm)
  fieldDialogVisible.value = true
}

const handleElementChange = (elementId) => {
  applyModelElementToField(fieldForm, elements.value, elementId)
}

const handleFieldSubmit = async () => {
  const requiredPermission = editingField.value
    ? 'model.logical_model.update'
    : 'model.logical_model.create'
  if (!canPerformDraftAction(table.value.status, authStore.hasPermission(requiredPermission))) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  try {
    await fieldFormRef.value.validate()
  } catch {
    return
  }
  fieldSubmitting.value = true
  try {
    if (editingField.value) {
      const result = await logicalTableAPI.updateField(
        tableId.value,
        editingField.value.id,
        buildLogicalFieldUpdateRequest(fieldForm, table.value.version)
      )
      table.value.version = result.version
      ElMessage.success(t('model.common.update_success'))
    } else {
      const result = await logicalTableAPI.createField(tableId.value, { ...fieldForm, version: table.value.version })
      table.value.version = result.version
      ElMessage.success(t('model.common.add_success'))
    }
    fieldDialogVisible.value = false
    loadFields()
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.op_failed'))
  } finally {
    fieldSubmitting.value = false
  }
}

const deleteField = async (fieldId) => {
  if (!canDeleteField.value) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  try {
    const result = await logicalTableAPI.deleteField(tableId.value, fieldId, table.value.version)
    table.value.version = result.version
    ElMessage.success(t('model.common.delete_success'))
    loadFields()
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.delete_failed'))
  }
}

let loadGeneration = 0
const loadPage = async () => {
  const generation = ++loadGeneration
  pageLoading.value = true
  pageError.value = ''
  referenceError.value = ''
  fieldDialogVisible.value = false
  editingField.value = null
  table.value = {}
  fields.value = []
  elements.value = []
  elementRevisions.value = new Map()
  if (!tableId.value) {
    pageLoading.value = false
    pageError.value = t('model.common.invalid_detail_id')
    return
  }
  try {
    await loadTable()
    if (generation !== loadGeneration) return
    await loadFields()
    const [domainsResult, elementsResult, layersResult, enginesResult] = await Promise.allSettled([
      domainAPI.list(),
      loadModelElementReferences(elementAPI, fields.value),
      dwLayerAPI.list(),
      listResourceTreeEngines('/api/v1/meta', {
        engineFilter: isSupportedPhysicalTargetEngine
      })
    ])
    if (generation !== loadGeneration) return
    domains.value = domainsResult.status === 'fulfilled' ? buildBusinessDomainOptions(domainsResult.value || []) : []
    elements.value = elementsResult.status === 'fulfilled' ? elementsResult.value.elements : []
    elementRevisions.value = elementsResult.status === 'fulfilled' ? elementsResult.value.revisions : new Map()
    layers.value = layersResult.status === 'fulfilled' ? layersResult.value || [] : []
    physicalTargetEngines.value = enginesResult.status === 'fulfilled' ? enginesResult.value || [] : []
    if ([domainsResult, elementsResult, layersResult, enginesResult].some(result => result.status === 'rejected') || elementsResult.value?.unavailable) {
      referenceError.value = t('model.common.reference_data_unavailable')
    }
    markSaved()
  } catch (error) {
    if (generation === loadGeneration) pageError.value = getModelErrorMessage(error, t, 'model.common.load_failed')
  } finally {
    if (generation === loadGeneration) pageLoading.value = false
  }
}

watch(() => route.params.id, loadPage, { immediate: true })
watch(() => detailRouteState.value.tab, tab => {
  if (tab !== 'relations') relationDirty.value = false
  if (tab !== 'concept-mappings') conceptMappingDirty.value = false
})
watch([() => route.query, () => pageLoading.value], () => {
  if (pageLoading.value || pageError.value || !table.value.table_type) return
  const state = detailRouteState.value
  if (state.changed) navigateModelRoute(router, { path: route.path, query: state.query }, { history: 'replace' })
})
</script>

<style scoped>
.logical-table-detail {
  padding: 20px;
}

.structural-constraints-section { margin-top: 16px; }

.detail-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 16px;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 10px;
}

.header-right {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.reference-warning {
  margin-bottom: 16px;
}

.table-name {
  font-size: 18px;
  font-weight: 600;
}

.info-card {
  margin-bottom: 0;
}

.card-title {
  font-weight: 600;
}

.card-header-with-action {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-header-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
}

.text-muted {
  color: var(--addp-text-tertiary);
}

.revision-tag {
  margin-left: 6px;
}

.source-field-tag {
  margin: 2px 4px 2px 0;
}

.decommission-target {
  margin: 16px 0;
}

.physical-target-help {
  margin-bottom: 16px;
}

.physical-target-summary {
  margin-bottom: 20px;
}

.engine-type {
  color: var(--addp-text-tertiary);
  margin-left: 4px;
}

@media (max-width: 767px) {
  .logical-table-detail {
    padding: 12px;
  }

  .header-left,
  .header-right {
    width: 100%;
  }

  .header-right :deep(.el-button) {
    flex: 1 1 auto;
    margin-left: 0;
  }
}
</style>
