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
        <el-button v-if="canEdit" type="primary" @click="handleSave" :loading="saving">{{ t('model.common.save') }}</el-button>
        <el-button v-if="table.status === 'draft' && authStore.hasPermission('model.logical_model.update')" type="success" @click="handleApprove">
          {{ t('model.common.approve') }}
        </el-button>
        <el-button v-if="table.status === 'approved' && authStore.hasPermission('model.logical_model.update')" @click="handleReopen">
          {{ t('model.common.reopen') }}
        </el-button>
        <el-button v-if="authStore.hasPermission('model.logical_model.read')" type="success" @click="handlePreviewDDL">
          <el-icon><View /></el-icon>
          {{ t('model.logical_table.preview_ddl') }}
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
      v-if="referenceError"
      class="reference-warning"
      type="warning"
      :title="referenceError"
      show-icon
      :closable="false"
    />

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
                  <el-select v-model="form.domain_id" :disabled="!canEdit" clearable style="width:100%">
                    <el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" />
                  </el-select>
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
              <!-- 维度表专属：SCD 类型 -->
              <el-col v-if="form.table_type === 'dimension'" :xs="24" :sm="12" :md="8">
                <el-form-item :label="t('model.logical_table.scd_type')">
                  <el-select v-model="form.scd_type" :disabled="!canEdit" style="width:100%">
                    <el-option :label="t('model.logical_table.scd_0')" :value="0" />
                    <el-option :label="t('model.logical_table.scd_1')" :value="1" />
                    <el-option :label="t('model.logical_table.scd_2')" :value="2" />
                    <el-option :label="t('model.logical_table.scd_3')" :value="3" />
                  </el-select>
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
                <span>{{ getElementName(row.element_id) || '-' }}</span>
                <el-tag v-if="row.element_revision_id" type="info" size="small" class="revision-tag">
                  {{ t('model.field.element_revision') }} #{{ row.element_revision_id }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('model.field.constraints')" width="140">
              <template #default="{ row }">
                <el-tag v-if="row.is_pk" type="warning" size="small">PK</el-tag>
                <el-tag v-if="row.is_partition" type="success" size="small">{{ t('model.field.is_partition') }}</el-tag>
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

      <!-- 指标实现（仅事实表） -->
      <el-col v-if="form.table_type === 'fact'" :span="24" style="margin-top:16px">
        <el-card shadow="never">
          <template #header>
            <div class="card-header-with-action">
              <span class="card-title">{{ t('model.metric.title') }}</span>
              <el-button v-if="canEdit && authStore.hasPermission('model.logical_model.update')" type="primary" size="small" @click="openMetricDialog()">
                <el-icon><Plus /></el-icon>
                {{ t('model.metric.add') }}
              </el-button>
            </div>
          </template>
          <el-table :data="metricImplementations" v-loading="metricLoading" stripe>
            <el-table-column :label="t('model.metric.definition_name')" min-width="160">
              <template #default="{ row }">
                {{ metricNameMap[row.metric_definition_id] || `指标#${row.metric_definition_id}` }}
              </template>
            </el-table-column>
            <el-table-column :label="t('model.metric.implementation_name')" prop="name" min-width="150" />
            <el-table-column :label="t('model.metric.grain')" prop="grain" min-width="150" show-overflow-tooltip />
            <el-table-column :label="t('model.metric.source_fields')" min-width="180">
              <template #default="{ row }">
                <el-tag v-for="fieldId in sourceFieldIDs(row)" :key="fieldId" size="small" class="source-field-tag">
                  {{ fieldNameMap[fieldId] || `字段#${fieldId}` }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('model.metric.engine')" min-width="100"><template #default="{ row }">{{ row.expression_config?.engine || '—' }}</template></el-table-column>
            <el-table-column :label="t('model.metric.status')" width="100"><template #default="{ row }"><el-tag :type="row.status === 'active' ? 'success' : 'info'" size="small">{{ t(`model.metric.status_${row.status}`) }}</el-tag></template></el-table-column>
            <el-table-column :label="t('model.metric.note')" prop="note" show-overflow-tooltip />
            <el-table-column :label="t('model.metric.actions')" width="130" fixed="right">
              <template #default="{ row }">
                <el-button v-if="canEdit && authStore.hasPermission('model.logical_model.update')" link type="primary" @click="openMetricDialog(row)">{{ t('model.common.edit') }}</el-button>
                <el-popconfirm v-if="canEdit && authStore.hasPermission('model.logical_model.update')" :title="t('model.metric.delete_confirm')" @confirm="deleteMetricImplementation(row.id)">
                  <template #reference>
                    <el-button link type="danger">{{ t('model.common.delete') }}</el-button>
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>

      <!-- 物化配置 -->
      <el-col :span="24" style="margin-top:16px">
        <el-card shadow="never">
          <template #header>
            <div class="card-header-with-action">
              <span class="card-title">{{ t('model.materialization.title') }}</span>
              <div v-if="canClearMaterialization || canDecommissionTarget" class="card-header-actions">
                <el-button
                  v-if="canClearMaterialization"
                  plain
                  @click="clearMaterializationConfig"
                >
                  {{ t('model.materialization.clear_config') }}
                </el-button>
                <el-button
                  v-if="canDecommissionTarget"
                  type="danger"
                  plain
                  @click="openDecommissionDialog"
                >
                  {{ t('model.materialization.decommission') }}
                </el-button>
              </div>
            </div>
          </template>
          <el-form :model="materializationForm" label-width="110px">
            <el-row :gutter="16">
              <el-col :xs="24" :sm="12" :md="8">
                <el-form-item :label="t('model.materialization.target_schema')">
                  <ResourceTreePicker
                    v-model="targetParentSelection"
                    api-base-url="/api/v1/meta"
                    mode="node"
                    :engine-families="['tabular']"
                    :selectable-filter="isSchemaSelection"
                    :initial-locator="materializationForm.target_parent_locator"
                    :show-selection-summary="false"
                    :show-count="false"
                    tree-height="260px"
                    @update:model-value="handleTargetParentSelect"
                    @select="handleTargetParentSelect"
                  />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12" :md="8">
                <el-form-item :label="t('model.materialization.target_name')">
                  <el-input v-model="materializationForm.target_name" :disabled="!canEdit" :placeholder="t('model.materialization.target_name_placeholder')" />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12" :md="8">
                <el-form-item :label="t('model.materialization.partition_by')">
                  <el-select v-model="materializationForm.partition_by" :disabled="!canEdit" :placeholder="t('model.common.optional')" clearable style="width:100%">
                    <el-option v-for="f in fields" :key="f.id" :label="f.column_name" :value="f.column_name" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12" :md="8">
                <el-form-item :label="t('model.materialization.partition_type')">
                  <el-select v-model="materializationForm.partition_type" :disabled="!canEdit" placeholder="RANGE" style="width:100%">
                    <el-option label="RANGE" value="range" />
                    <el-option label="LIST" value="list" />
                    <el-option label="HASH" value="hash" />
                  </el-select>
                </el-form-item>
              </el-col>
            </el-row>
          </el-form>
        </el-card>
      </el-col>
    </el-row>

    <!-- 字段对话框 -->
    <el-dialog
      v-model="fieldDialogVisible"
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
                <el-option label="text" value="text" />
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
              v-for="e in elements"
              :key="e.id"
              :label="`${e.name} (${e.code})`"
              :value="e.id"
            />
          </el-select>
        </el-form-item>
        <el-row :gutter="8">
          <el-col :span="8">
            <el-form-item :label="t('model.field.is_pk')">
              <el-switch v-model="fieldForm.is_pk" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item :label="t('model.field.nullable')">
              <el-switch v-model="fieldForm.nullable" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item :label="t('model.field.is_partition')">
              <el-switch v-model="fieldForm.is_partition" />
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
        <el-button @click="fieldDialogVisible = false">{{ t('model.common.cancel') }}</el-button>
        <el-button type="primary" @click="handleFieldSubmit" :loading="fieldSubmitting">
          {{ editingField ? t('model.common.save') : t('model.common.add') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 指标实现对话框 -->
    <el-dialog v-model="metricDialogVisible" class="addp-dialog" :title="editingMetricImplementation ? t('model.metric.edit') : t('model.metric.add')" width="min(760px, calc(100vw - 32px))">
      <el-form :model="metricForm" label-width="120px">
        <el-form-item :label="t('model.metric.definition_name')" required>
          <el-select
            v-model="metricForm.metric_definition_id"
            filterable
            :placeholder="t('model.metric.select_placeholder')"
            style="width:100%"
            @focus="loadAvailableMetrics"
            @change="selectMetricDefinition"
          >
            <el-option
              v-for="m in availableMetrics"
              :key="m.id"
              :label="`${m.current_revision?.name || m.code} (${m.code}, R${m.current_revision?.revision_no || '-'})`"
              :value="m.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('model.metric.implementation_name')" required><el-input v-model="metricForm.name" maxlength="200" /></el-form-item>
        <el-form-item :label="t('model.metric.grain')" required><el-input v-model="metricForm.grain" type="textarea" :rows="2" :placeholder="t('model.metric.grain_placeholder')" /></el-form-item>
        <el-form-item :label="t('model.metric.source_fields')" required>
          <el-select v-model="metricForm.field_ids" multiple filterable :placeholder="t('model.metric.field_placeholder')" style="width:100%">
            <el-option
              v-for="f in measureFields"
              :key="f.id"
              :label="`${f.name} (${f.column_name})`"
              :value="f.id"
            />
          </el-select>
        </el-form-item>
        <el-row :gutter="12"><el-col :span="12"><el-form-item :label="t('model.metric.engine')" required><el-input v-model="metricForm.engine" placeholder="sql" /></el-form-item></el-col><el-col :span="12"><el-form-item :label="t('model.metric.status')" required><el-select v-model="metricForm.status" style="width:100%"><el-option :label="t('model.metric.status_active')" value="active" /><el-option :label="t('model.metric.status_disabled')" value="disabled" /></el-select></el-form-item></el-col></el-row>
        <el-form-item :label="t('model.metric.expression')" required><el-input v-model="metricForm.expression" type="textarea" :rows="4" :placeholder="t('model.metric.expression_placeholder')" /></el-form-item>
        <el-form-item :label="t('model.metric.note')">
          <el-input v-model="metricForm.note" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="metricDialogVisible = false">{{ t('model.common.cancel') }}</el-button>
        <el-button type="primary" @click="saveMetricImplementation" :loading="metricSubmitting">{{ t('model.common.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- DDL 预览对话框 -->
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
        <el-descriptions-item :label="t('model.materialization.target_schema')">
          {{ decommissionTarget.schemaName }}
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
import { ref, reactive, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ResourceTreePicker, listResourceTreeEngines, parseLocator, useConsolePageDescriptor } from '@common-ui'
import { ElMessage } from 'element-plus'
import { ArrowLeft, Plus, Refresh, View } from '@element-plus/icons-vue'
import { logicalTableAPI, domainAPI, elementAPI, standardMetricAPI, dwLayerAPI } from '../api/model'
import DDLPreviewDialog from '../components/DDLPreviewDialog.vue'
import DimensionHierarchyEditor from '../components/DimensionHierarchyEditor.vue'
import { useI18n } from 'vue-i18n'
import { navigateModelRoute } from '../utils/moduleNavigation'
import { useAuthStore } from '../store/auth'
import { resolveLogicalTableListRouteState } from '../utils/routeState'
import { getModelErrorMessage } from '../utils/apiError'
import {
  buildDDLPreviewRequest,
  buildLogicalFieldUpdateRequest,
  buildLogicalTableUpdateRequest,
  canPerformDraftAction,
  isEditableDraft,
  resolvePositiveRouteId
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
const decommissionEngineName = ref('')
const editingField = ref(null)
const fieldFormRef = ref(null)

// 指标实现相关
const metricLoading = ref(false)
const metricDialogVisible = ref(false)
const metricSubmitting = ref(false)
const metricImplementations = ref([])
const availableMetrics = ref([])
const editingMetricImplementation = ref(null)

const table = ref({})
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
  target_parent_locator: '', target_name: '', partition_by: '', partition_type: 'range'
})
const targetParentSelection = ref(null)
const fields = ref([])
const domains = ref([])
const layers = ref([])
const elements = ref([])
const getElementName = id => elements.value.find(element => element.id === id)?.name
const ddlContent = ref('')

const decommissionTarget = computed(() => {
  const materialization = table.value?.materialization || {}
  const locator = materialization.target_parent_locator || ''
  let parsed = {}
  try {
    parsed = parseLocator(locator)
  } catch {
    parsed = {}
  }
  const schemaName = parsed.path?.[parsed.path.length - 1] || ''
  const targetName = materialization.target_name || ''
  return {
    locator,
    schemaName,
    targetName,
    engineId: parsed.engineId || 0,
    engineName: decommissionEngineName.value,
    confirmation: schemaName && targetName ? `${schemaName}.${targetName}` : ''
  }
})
const canDecommissionTarget = computed(() =>
  authStore.hasPermission('model.materialized_target.delete') &&
  Boolean(decommissionTarget.value.locator && decommissionTarget.value.targetName)
)
const canClearMaterialization = computed(() => canEdit.value && Boolean(
  String(materializationForm.target_parent_locator || '').trim() ||
  String(materializationForm.target_name || '').trim() ||
  String(materializationForm.partition_by || '').trim()
))

const blankMetricImplementation = () => ({
  metric_definition_id: null,
  metric_definition_revision_id: null,
  name: '',
  grain: '',
  field_ids: [],
  engine: 'sql',
  expression: '',
  status: 'active',
  note: ''
})
const metricForm = reactive(blankMetricImplementation())

// 度量字段（field_role 为 measure_* 的字段）
const measureFields = computed(() =>
  fields.value.filter(f => f.field_role && f.field_role.startsWith('measure_'))
)

// 指标名称映射（id -> name）
const metricNameMap = computed(() => {
  const map = {}
  availableMetrics.value.forEach(m => { map[m.id] = m.current_revision?.name || m.code })
  return map
})

// 字段名称映射（id -> name）
const fieldNameMap = computed(() => {
  const map = {}
  fields.value.forEach(f => { map[f.id] = `${f.name}(${f.column_name})` })
  return map
})

const fieldForm = reactive({
  name: '', column_name: '', data_type: 'string', length: null,
  nullable: true, is_pk: false, is_partition: false,
  default_value: '', element_id: null, description: '',
  field_role: 'regular', sort_order: 0
})
const fieldRules = {
  name: [{ required: true, message: t('model.field.name_required'), trigger: 'blur' }],
  column_name: [{ required: true, message: t('model.field.column_required'), trigger: 'blur' }],
  data_type: [{ required: true, message: t('model.field.type_required'), trigger: 'change' }]
}

const unsavedState = computed(() => ({
  form: { ...form },
  materialization: { ...materializationForm },
  field_draft: fieldDialogVisible.value ? { ...fieldForm } : null,
  metric_draft: metricDialogVisible.value ? { ...metricForm } : null
}))
const { isDirty, markSaved, confirmDiscardChanges } = useUnsavedChanges({ state: unsavedState, t })

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
    target_name: mat.target_name || '',
    partition_by: mat.partition_by || '',
    partition_type: mat.partition_type || 'range',
  })
  targetParentSelection.value = null
}

const isSchemaSelection = (node, { locator }) => Boolean(canEdit.value) && node?.type === 'schema' && locator.type === 'schema' && !locator.itemId

const handleTargetParentSelect = selection => {
  materializationForm.target_parent_locator = selection?.identity?.locator || ''
}

const clearMaterializationConfig = () => {
  targetParentSelection.value = null
  Object.assign(materializationForm, {
    target_parent_locator: '',
    target_name: '',
    partition_by: '',
    partition_type: 'range',
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

const loadMetrics = async () => {
  if (form.table_type !== 'fact') return
  metricLoading.value = true
  try {
    const res = await logicalTableAPI.listMetricImplementations(tableId.value)
    metricImplementations.value = res || []
  } finally {
    metricLoading.value = false
  }
}

const loadAvailableMetrics = async () => {
  if (availableMetrics.value.length > 0) return
  try {
    const res = await standardMetricAPI.listAll()
    availableMetrics.value = res
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.metric.load_failed'))
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
    const updated = await logicalTableAPI.approve(tableId.value, table.value.version)
    applyTable(updated)
    markSaved()
    ElMessage.success(t('model.common.approve_success'))
  }
  catch (err) { ElMessage.error(getModelErrorMessage(err, t, 'model.common.op_failed')) }
}

const handleReopen = async () => {
  if (!authStore.hasPermission('model.logical_model.update')) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  try {
    const updated = await logicalTableAPI.reopen(tableId.value, table.value.version)
    applyTable(updated)
    markSaved()
    ElMessage.success(t('model.common.reopen_success'))
  }
  catch (err) { ElMessage.error(getModelErrorMessage(err, t, 'model.common.op_failed')) }
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

const openDecommissionDialog = async () => {
  if (!canDecommissionTarget.value) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  if (isDirty.value) {
    ElMessage.warning(t('model.common.save_before_action'))
    return
  }
  decommissionConfirmation.value = ''
  decommissionEngineName.value = ''
  decommissionDialogVisible.value = true
  try {
    const engines = await listResourceTreeEngines('/api/v1/meta', { engineFamilies: ['tabular'] })
    decommissionEngineName.value = engines.find(engine => Number(engine.id) === decommissionTarget.value.engineId)?.name || ''
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.materialization.engine_load_failed'))
  }
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
      is_partition: field.is_partition,
      default_value: field.default_value || '',
      element_id: field.element_id ?? null,
      description: field.description || '',
      field_role: field.field_role || 'regular',
      sort_order: field.sort_order ?? 0
    })
  } else {
    Object.assign(fieldForm, {
      name: '', column_name: '', data_type: 'string', length: null,
      nullable: true, is_pk: false, is_partition: false,
      default_value: '', element_id: null, description: '',
      field_role: 'regular', sort_order: 0
    })
  }
  fieldDialogVisible.value = true
}

const handleElementChange = (elementId) => {
  if (!elementId) return
  const el = elements.value.find(e => e.id === elementId)
  if (el) {
    fieldForm.name = el.name
    fieldForm.data_type = el.data_type
    if (el.length) fieldForm.length = el.length
  }
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

const sourceFieldIDs = row => Array.isArray(row?.source_config?.field_ids) ? row.source_config.field_ids : []

const selectMetricDefinition = definitionID => {
  const selected = availableMetrics.value.find(item => item.id === definitionID)
  metricForm.metric_definition_revision_id = selected?.current_revision?.id || null
  if (!metricForm.name) metricForm.name = selected?.current_revision?.name || selected?.code || ''
}

const openMetricDialog = (implementation = null) => {
  editingMetricImplementation.value = implementation
  Object.assign(metricForm, blankMetricImplementation())
  if (implementation) {
    Object.assign(metricForm, {
      metric_definition_id: implementation.metric_definition_id,
      metric_definition_revision_id: implementation.metric_definition_revision_id,
      name: implementation.name,
      grain: implementation.grain,
      field_ids: sourceFieldIDs(implementation),
      engine: implementation.expression_config?.engine || 'sql',
      expression: implementation.expression_config?.expression || '',
      status: implementation.status,
      note: implementation.note || ''
    })
  }
  metricDialogVisible.value = true
  loadAvailableMetrics()
}

const saveMetricImplementation = async () => {
  if (!canEdit.value || !authStore.hasPermission('model.logical_model.update')) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  if (!metricForm.metric_definition_id || !metricForm.metric_definition_revision_id) {
    ElMessage.warning(t('model.metric.select_required'))
    return
  }
  if (!metricForm.name.trim() || !metricForm.grain.trim() || !metricForm.field_ids.length || !metricForm.engine.trim() || !metricForm.expression.trim()) {
    ElMessage.warning(t('model.metric.required_fields'))
    return
  }
  metricSubmitting.value = true
  try {
    const payload = {
      version: table.value.version,
      metric_definition_id: metricForm.metric_definition_id,
      metric_definition_revision_id: metricForm.metric_definition_revision_id,
      name: metricForm.name.trim(),
      grain: metricForm.grain.trim(),
      source_config: { field_ids: metricForm.field_ids },
      dimension_config: {},
      filter_config: {},
      expression_config: { engine: metricForm.engine.trim(), expression: metricForm.expression.trim() },
      status: metricForm.status,
      note: metricForm.note.trim()
    }
    const result = editingMetricImplementation.value
      ? await logicalTableAPI.updateMetricImplementation(tableId.value, editingMetricImplementation.value.id, payload)
      : await logicalTableAPI.createMetricImplementation(tableId.value, payload)
    table.value.version = result.version
    ElMessage.success(t(editingMetricImplementation.value ? 'model.common.update_success' : 'model.common.create_success'))
    metricDialogVisible.value = false
    loadMetrics()
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.metric.save_failed'))
  } finally {
    metricSubmitting.value = false
  }
}

const deleteMetricImplementation = async (implementationId) => {
  if (!canEdit.value || !authStore.hasPermission('model.logical_model.update')) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  try {
    const result = await logicalTableAPI.deleteMetricImplementation(tableId.value, implementationId, table.value.version)
    table.value.version = result.version
    ElMessage.success(t('model.common.delete_success'))
    loadMetrics()
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.op_failed'))
  }
}

let loadGeneration = 0
const loadPage = async () => {
  const generation = ++loadGeneration
  pageLoading.value = true
  pageError.value = ''
  referenceError.value = ''
  fieldDialogVisible.value = false
  metricDialogVisible.value = false
  editingField.value = null
  table.value = {}
  fields.value = []
  metricImplementations.value = []
  if (!tableId.value) {
    pageLoading.value = false
    pageError.value = t('model.common.invalid_detail_id')
    return
  }
  try {
    await loadTable()
    if (generation !== loadGeneration) return
    await Promise.all([loadFields(), loadMetrics(), loadAvailableMetrics()])
    const [domainsResult, elementsResult, layersResult] = await Promise.allSettled([
      domainAPI.list(), elementAPI.listAll(), dwLayerAPI.list()
    ])
    if (generation !== loadGeneration) return
    domains.value = domainsResult.status === 'fulfilled' ? domainsResult.value || [] : []
    elements.value = elementsResult.status === 'fulfilled' ? elementsResult.value || [] : []
    layers.value = layersResult.status === 'fulfilled' ? layersResult.value || [] : []
    if ([domainsResult, elementsResult, layersResult].some(result => result.status === 'rejected')) {
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
</script>

<style scoped>
.logical-table-detail {
  padding: 20px;
}

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
