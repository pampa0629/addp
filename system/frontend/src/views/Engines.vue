<template>

    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ t('system.engine.title') }}</span>
          <div class="header-buttons">
            <el-button type="primary" :icon="Plus" @click="showAddStorageDialog">{{ t('system.engine.addStorage') }}</el-button>
            <el-button type="warning" :icon="Plus" @click="showAddExtensionDialog">{{ t('system.engine.addExtension') }}</el-button>
          </div>
        </div>
      </template>

      <!-- 能力过滤栏 -->
      <div class="filter-bar">
        <span class="filter-label">{{ t('system.engine.filter') }}</span>
        <el-checkbox-group v-model="selectedCategories" @change="handleFilterChange">
          <el-checkbox value="storage">{{ t('system.engine.filterStorage') }}</el-checkbox>
          <el-checkbox value="compute">{{ t('system.engine.filterCompute') }}</el-checkbox>
          <el-checkbox value="general">{{ t('system.engine.filterGeneral') }}</el-checkbox>
          <el-checkbox value="extension">{{ t('system.engine.filterExtension') }}</el-checkbox>
          <el-checkbox value="builtin">{{ t('system.engine.filterBuiltin') }}</el-checkbox>
        </el-checkbox-group>
      </div>

      <el-table :data="engines" v-loading="loading" stripe :row-class-name="tableRowClassName">
        <!-- ID -->
        <el-table-column prop="id" :label="t('system.engine.columns.id')" width="80" />

        <!-- 名称 -->
        <el-table-column prop="name" :label="t('system.engine.columns.name')" min-width="150" />

        <!-- 类型 -->
        <el-table-column prop="engine_type" :label="t('system.engine.columns.type')" width="150">
          <template #default="{ row }">
            <el-tag :type="getEngineTypeColor(row.engine_type)">
              {{ getEngineTypeLabel(row.engine_type) }}
            </el-tag>
          </template>
        </el-table-column>

        <!-- 最近连接检测 -->
        <el-table-column :label="t('system.engine.columns.connection')" width="140" align="center">
          <template #default="{ row }">
            <el-tooltip
              :content="getConnectionTooltip(row)"
              placement="top"
            >
              <span class="connection-status-cell">
                <span
                  class="connection-status-icon"
                  :class="getConnectionStatusClass(row.connection_status)"
                ></span>
                <span class="connection-status-label">{{ getConnectionStatusLabel(row.connection_status) }}</span>
              </span>
            </el-tooltip>
          </template>
        </el-table-column>

        <!-- 启用状态 -->
        <el-table-column :label="t('system.engine.columns.status')" width="110">
          <template #default="{ row }">
            <el-tooltip :disabled="!row.deletion_error" :content="row.deletion_error" placement="top">
              <el-tag :type="getLifecycleTagType(row.lifecycle_state)">
                {{ getLifecycleLabel(row.lifecycle_state) }}
              </el-tag>
            </el-tooltip>
          </template>
        </el-table-column>

        <!-- 能力标签 -->
        <el-table-column :label="t('system.engine.columns.capabilities')" min-width="220">
          <template #default="{ row }">
            <el-tag
              v-for="tag in getCapabilitySummaryTags(row)"
              :key="tag.id"
              size="small"
              effect="plain"
              :type="getCapabilityStatusTagType(tag.status)"
              style="margin: 2px"
            >
              {{ getCapabilityViewText(tag) }}
            </el-tag>
          </template>
        </el-table-column>

        <!-- 引擎来源 -->
        <el-table-column :label="t('system.engine.columns.category')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.engine_origin === 'general' ? 'success' : 'warning'" size="small">
              {{ row.engine_origin === 'general' ? t('system.engine.category.general') : t('system.engine.category.extension') }}
            </el-tag>
          </template>
        </el-table-column>

        <!-- 注册/内置标识 -->
        <el-table-column :label="t('system.engine.columns.builtin')" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.is_builtin" type="info" size="small" effect="plain">
              {{ t('system.engine.builtin.builtin') }}
            </el-tag>
            <el-tag v-else type="success" size="small" effect="light">
              {{ t('system.engine.builtin.registered') }}
            </el-tag>
          </template>
        </el-table-column>

        <!-- 创建时间 -->
        <el-table-column :label="t('system.engine.columns.createdAt')" width="160">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>

        <!-- 操作列 -->
        <el-table-column :label="t('system.engine.columns.actions')" width="340" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" :disabled="row.lifecycle_state === 'deleting'" @click="testConnection(row)">{{ t('system.engine.actions.test') }}</el-button>
            <el-button size="small" @click="viewEngineDetails(row)">{{ t('system.engine.actions.detail') }}</el-button>
            <el-button
              size="small"
              type="warning"
              :icon="Edit"
              :disabled="!canEditEngine(row)"
              @click="editEngine(row)"
            >
              {{ t('system.engine.actions.edit') }}
            </el-button>
            <el-button
              size="small"
              type="danger"
              :icon="Delete"
              :disabled="row.is_builtin"
              @click="deleteEngine(row)"
            >
              {{ row.lifecycle_state === 'deleting' ? t('system.engine.actions.retryDelete') : t('system.engine.actions.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :page-sizes="[10, 20, 50, 100]"
        :total="total"
        layout="total, sizes, prev, pager, next"
        style="margin-top: 20px; justify-content: flex-end"
        @size-change="handlePageSizeChange"
      />

    </el-card>

    <!-- 新增/编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="980px"
      @close="resetForm"
    >
      <div class="storage-layout">
        <aside class="engine-type-sidebar">
          <div class="sidebar-title">{{ t('system.engine.registerPanel.title') }}</div>
          <div class="sidebar-subtitle">{{ t('system.engine.registerPanel.subtitle') }}</div>
          <div class="engine-type-list">
            <el-tooltip
              v-for="item in visibleStorageEngineTypeOptions"
              :key="item.value"
              :content="item.desc"
              placement="right"
            >
              <button
                type="button"
                class="engine-type-item"
                :class="{
                  'is-active': form.engine_type === item.value,
                  'is-disabled': isEdit
                }"
                :disabled="isEdit"
                @click="selectStorageEngineType(item.value)"
              >
                <span class="engine-type-icon">{{ item.icon }}</span>
                <span class="engine-type-name">{{ item.label }}</span>
              </button>
            </el-tooltip>
          </div>
          <div v-if="isEdit" class="sidebar-hint">
            {{ t('system.engine.registerPanel.editLockedHint') }}
          </div>
        </aside>

        <section class="storage-form-panel">
          <StorageEngineForm
            ref="storageFormRef"
            v-model="form"
            :is-edit="isEdit"
            :show-type-selector="false"
          />

          <el-collapse
            v-if="showSpatialWorkspacePanel"
            v-model="spatialWorkspaceCollapse"
            class="spatial-workspace-collapse"
          >
            <el-collapse-item
              v-for="workspace in superMapWorkspaces"
              :key="workspaceKey(workspace)"
              :name="workspaceKey(workspace)"
            >
              <template #title>
                <span class="spatial-workspace-title">
                  {{ t(`${workspaceProductKey(workspace)}.title`) }}
                  <el-tag size="small" effect="plain" :type="spatialWorkspaceStateTagType(workspace)">
                    {{ spatialWorkspaceStateText(workspace) }}
                  </el-tag>
                </span>
              </template>

              <el-alert
                class="spatial-workspace-alert"
                :title="t(`${workspaceProductKey(workspace)}.warning`)"
                type="warning"
                show-icon
                :closable="false"
              />

              <div class="spatial-workspace-body">
                <div class="spatial-workspace-meta">
                  <el-tag size="small" effect="plain">SuperMap</el-tag>
                  <el-tag size="small" effect="plain">{{ t(`${workspaceProductKey(workspace)}.title`) }}</el-tag>
                  <el-tag
                    v-if="workspace?.bound_runtime_engine_id"
                    size="small"
                    effect="plain"
                  >
                    {{ t('system.engine.spatialWorkspace.runtime', { id: workspace.bound_runtime_engine_id }) }}
                  </el-tag>
                  <el-tag
                    v-if="workspace?.risk_level"
                    size="small"
                    effect="plain"
                    type="warning"
                  >
                    {{ t('system.engine.spatialWorkspace.risk', { level: spatialWorkspaceRiskText(workspace) }) }}
                  </el-tag>
                </div>

                <el-button
                  type="danger"
                  :loading="enablingSpatialWorkspace"
                  :disabled="!canEnableSuperMapWorkspace(workspace)"
                  @click="enableSuperMapSpatialWorkspace(workspace)"
                >
                  {{ t(`${workspaceProductKey(workspace)}.enable`) }}
                </el-button>
              </div>
            </el-collapse-item>
          </el-collapse>
        </section>
      </div>

      <template #footer>
        <el-button @click="dialogVisible = false">{{ t('system.engine.actions.cancel') }}</el-button>
        <el-button
          type="warning"
          :loading="testing"
          @click="testBeforeCreate"
        >
          {{ t('system.engine.actions.testConnection') }}
        </el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">{{ t('system.engine.actions.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- 扩展引擎注册弹窗 -->
    <el-dialog
      v-model="extensionDialogVisible"
      :title="t('system.engine.dialog.addExtension')"
      width="760px"
      @close="resetExtensionForm"
    >
      <el-form
        ref="extensionFormRef"
        :model="extensionForm"
        label-width="150px"
        class="extension-form"
      >
        <div class="extension-example-bar">
          <div class="extension-example-info">
            <span>{{ t('system.engine.extensionForm.exampleHint') }}</span>
            <el-tag
              v-if="extensionRuntimeStatus !== 'idle'"
              size="small"
              effect="plain"
              :type="extensionRuntimeStatusTagType"
            >
              {{ extensionRuntimeStatusText }}
            </el-tag>
          </div>
          <div class="extension-example-actions">
            <el-button size="small" type="primary" plain @click="fillSuperMapWorkflowExample">
              {{ t('system.engine.extensionForm.useSuperMapExample') }}
            </el-button>
            <el-button size="small" type="info" plain @click="fillMathWorkflowExample">
              {{ t('system.engine.extensionForm.useMathExample') }}
            </el-button>
            <el-button
              size="small"
              type="primary"
              plain
              :loading="extensionRuntimeChecking"
              @click="checkExtensionRuntimeStatus"
            >
              {{ t('system.engine.extensionForm.checkRuntime') }}
            </el-button>
          </div>
        </div>

        <el-form-item :label="t('system.engine.extensionForm.engineType')" required>
          <el-input
            v-model="extensionForm.engine_type"
            :placeholder="t('system.engine.extensionForm.engineTypePlaceholder')"
          />
        </el-form-item>

        <el-form-item :label="t('system.engine.extensionForm.name')" required>
          <el-input v-model="extensionForm.name" />
        </el-form-item>

        <el-form-item :label="t('system.engine.extensionForm.runtimeProtocol')">
          <el-input :model-value="extensionRuntimeProtocol" disabled />
        </el-form-item>

        <el-form-item :label="t('system.engine.extensionForm.description')">
          <el-input v-model="extensionForm.description" type="textarea" :rows="2" />
        </el-form-item>

        <el-form-item :label="t('system.engine.extensionForm.protocol')" required>
          <el-select v-model="extensionForm.protocol" style="width: 180px">
            <el-option label="http" value="http" />
            <el-option label="https" value="https" />
          </el-select>
        </el-form-item>

        <el-form-item :label="t('system.engine.extensionForm.host')" required>
          <el-input v-model="extensionForm.host" />
        </el-form-item>

        <el-form-item :label="t('system.engine.extensionForm.port')" required>
          <el-input-number v-model="extensionForm.port" :min="1" :max="65535" controls-position="right" />
        </el-form-item>

        <el-form-item :label="t('system.engine.extensionForm.capabilities')">
          <el-input
            v-model="extensionCapabilitiesText"
            type="textarea"
            :rows="8"
            :placeholder="t('system.engine.extensionForm.capabilitiesPlaceholder')"
          />
        </el-form-item>

        <el-alert
          v-if="extensionProbeResult"
          type="success"
          :closable="false"
          show-icon
          class="extension-probe-alert"
        >
          <template #title>
            {{ extensionProbeResult }}
          </template>
        </el-alert>
      </el-form>

      <template #footer>
        <el-button @click="extensionDialogVisible = false">{{ t('system.engine.actions.cancel') }}</el-button>
        <el-button
          type="warning"
          :loading="extensionTesting"
          :disabled="extensionSubmitting"
          @click="testExtensionBeforeCreate"
        >
          {{ t('system.engine.actions.testConnection') }}
        </el-button>
        <el-button
          type="primary"
          :loading="extensionSubmitting"
          :disabled="extensionTesting"
          @click="submitExtensionForm"
        >
          {{ t('system.engine.actions.save') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 引擎详情弹窗 -->
    <el-dialog
      v-model="detailsVisible"
      :title="t('system.engine.dialog.details', { name: selectedEngine?.name || '' })"
      width="920px"
      destroy-on-close
      @closed="handleDetailsClosed"
    >
      <div v-loading="detailsLoading" style="min-height: 300px">
        <el-empty v-if="detailError" :description="detailError" />
        <el-tabs v-else-if="selectedEngine" v-model="detailTab" type="border-card" @tab-change="selectDetailTab">
          <!-- 基本信息标签页 -->
          <el-tab-pane name="basic" :label="t('system.engine.dialog.detailTabs.basic')">
            <el-descriptions :column="2" border>
              <el-descriptions-item :label="t('system.engine.dialog.basicInfo.id')">{{ selectedEngine.id }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.engine.dialog.basicInfo.name')">{{ selectedEngine.name }}</el-descriptions-item>
              <el-descriptions-item :label="t('system.engine.dialog.basicInfo.engineType')">
                <el-tag :type="getEngineTypeColor(selectedEngine.engine_type)">
                  {{ getEngineTypeLabel(selectedEngine.engine_type) }}
                </el-tag>
              </el-descriptions-item>
              <el-descriptions-item :label="t('system.engine.dialog.basicInfo.category')">
                <el-tag :type="selectedEngine.engine_origin === 'general' ? 'success' : 'warning'">
                  {{ selectedEngine.engine_origin === 'general' ? t('system.engine.dialog.basicInfo.generalEngine') : t('system.engine.dialog.basicInfo.extensionEngine') }}
                </el-tag>
              </el-descriptions-item>
              <el-descriptions-item :label="t('system.engine.dialog.basicInfo.registration')">
                <el-tag v-if="selectedEngine.is_builtin" type="info">{{ t('system.engine.dialog.basicInfo.builtinEngine') }}</el-tag>
                <el-tag v-else type="success">{{ t('system.engine.dialog.basicInfo.userRegistered') }}</el-tag>
              </el-descriptions-item>
              <el-descriptions-item :label="t('system.engine.dialog.basicInfo.status')">
                <el-tag :type="getLifecycleTagType(selectedEngine.lifecycle_state)">
                  {{ getLifecycleLabel(selectedEngine.lifecycle_state) }}
                </el-tag>
              </el-descriptions-item>
              <el-descriptions-item v-if="selectedEngine.deletion_error" :label="t('system.engine.dialog.basicInfo.deletionError')" :span="2">
                {{ selectedEngine.deletion_error }}
              </el-descriptions-item>
              <el-descriptions-item :label="t('system.engine.dialog.basicInfo.createdAt')" :span="2">
                {{ formatDate(selectedEngine.created_at) }}
              </el-descriptions-item>
              <el-descriptions-item :label="t('system.engine.dialog.basicInfo.updatedAt')" :span="2">
                {{ formatDate(selectedEngine.updated_at) }}
              </el-descriptions-item>
              <el-descriptions-item :label="t('system.engine.dialog.basicInfo.description')" :span="2">
                {{ selectedEngine.description || t('system.engine.dialog.basicInfo.none') }}
              </el-descriptions-item>
            </el-descriptions>
          </el-tab-pane>

          <!-- 连接配置标签页 -->
          <el-tab-pane v-if="selectedEngine.connection_info && Object.keys(selectedEngine.connection_info).length > 0" name="connection" :label="t('system.engine.dialog.detailTabs.connection')">
            <el-descriptions :column="1" border>
              <el-descriptions-item
                v-for="[key, value] in sortedConnectionInfo"
                :key="key"
                :label="key"
              >
                {{ value }}
              </el-descriptions-item>
            </el-descriptions>
          </el-tab-pane>

          <!-- 能力声明标签页 -->
          <el-tab-pane v-if="hasSelectedCapabilitiesView" name="capabilities" :label="t('system.engine.dialog.detailTabs.capabilities')">
            <div class="capability-detail">
              <div class="capability-toolbar">
                <div class="capability-summary">
                  <el-tag
                    v-for="badge in selectedCapabilitiesView.summary"
                    :key="badge.id"
                    size="small"
                    effect="plain"
                    :type="getCapabilityStatusTagType(badge.status)"
                  >
                    {{ getCapabilityViewText(badge) }}
                  </el-tag>
                </div>
                <el-button size="small" @click="jsonViewVisible = true">
                  {{ t('system.engine.capabilityView.actions.viewJson') }}
                </el-button>
              </div>

              <el-empty
                v-if="selectedCapabilitiesView.sections.length === 0"
                :description="t('system.engine.capabilities.none')"
              />

              <div
                v-for="section in selectedCapabilitiesView.sections"
                :key="section.id"
                class="capability-card"
              >
                <div class="capability-card-header">
                  <div>
                    <div class="capability-section-title">{{ translateCapabilityKey(section.title_key, section.id) }}</div>
                    <div v-if="section.description_key" class="capability-section-desc">
                      {{ translateCapabilityKey(section.description_key) }}
                    </div>
                  </div>
                  <el-tag size="small" effect="plain" :type="getCapabilityStatusTagType(section.status)">
                    {{ getCapabilityStatusLabel(section.status) }}
                  </el-tag>
                </div>

                <div v-if="section.path?.length" class="capability-path">
                  <template v-for="(node, index) in section.path" :key="node.id || index">
                    <div class="capability-path-node">
                      <span>{{ getCapabilityPathLabel(node) }}</span>
                      <el-tag
                        v-for="tag in node.tags || []"
                        :key="tag.id"
                        size="small"
                        effect="plain"
                      >
                        {{ getCapabilityTagText(tag) }}
                      </el-tag>
                    </div>
                    <span v-if="index < section.path.length - 1" class="capability-path-arrow">→</span>
                  </template>
                </div>

                <div v-if="section.items?.length" class="capability-items">
                  <div
                    v-for="item in section.items"
                    :key="item.id"
                    class="capability-item"
                  >
                    <div class="capability-item-main">
                      <span class="capability-item-label">{{ getCapabilityItemLabel(item) }}</span>
                      <el-tag size="small" effect="plain" :type="getCapabilityStatusTagType(item.status)">
                        {{ getCapabilityStatusLabel(item.status) }}
                      </el-tag>
                    </div>
                    <div v-if="item.reason_key" class="capability-item-reason">
                      {{ translateCapabilityKey(item.reason_key) }}
                    </div>
                    <div v-if="getCapabilityItemValue(item)" class="capability-item-value">
                      {{ getCapabilityItemValue(item) }}
                    </div>
                    <div v-if="item.tags?.length" class="capability-item-tags">
                      <el-tag
                        v-for="tag in item.tags"
                        :key="tag.id"
                        size="small"
                        effect="plain"
                      >
                        {{ getCapabilityTagText(tag) }}
                      </el-tag>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </el-tab-pane>

        </el-tabs>
      </div>
      <template #footer>
        <el-button @click="closeEngineDetails">{{ t('system.engine.dialog.close') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="deletionDialogVisible"
      :title="t('system.engine.deletionAssessment.title', { name: deletionEngine?.name || '' })"
      width="820px"
      destroy-on-close
      @closed="resetDeletionDialog"
    >
      <el-alert
        v-if="deletionAssessmentError"
        type="error"
        :title="deletionAssessmentError"
        show-icon
        :closable="false"
        class="deletion-alert"
      />
      <el-alert
        v-else-if="deletionImpact.running > 0"
        type="warning"
        :title="t('system.engine.deletionAssessment.runningBlocked', { count: deletionImpact.running })"
        show-icon
        :closable="false"
        class="deletion-alert"
      />

      <div class="deletion-toolbar">
        <span class="deletion-toolbar-label">{{ t('system.engine.deletionAssessment.externalPolicy') }}</span>
        <el-radio-group v-model="deletionExternalPolicy" :disabled="deletionAssessing || deletionSubmitting" @change="runDeletionAssessment">
          <el-radio-button value="delete">{{ t('system.engine.deletionAssessment.deleteExternal') }}</el-radio-button>
          <el-radio-button value="abandon">{{ t('system.engine.deletionAssessment.keepExternal') }}</el-radio-button>
        </el-radio-group>
        <el-button :loading="deletionAssessing" :disabled="deletionSubmitting" @click="runDeletionAssessment">
          {{ t('system.engine.deletionAssessment.refresh') }}
        </el-button>
      </div>

      <div v-loading="deletionAssessing" class="deletion-content">
        <div class="impact-summary">
          <div v-for="item in deletionImpactCards" :key="item.key" class="impact-summary-item">
            <span class="impact-summary-value">{{ item.value }}</span>
            <span class="impact-summary-label">{{ item.label }}</span>
          </div>
        </div>

        <el-table :data="deletionModuleRows" size="small" border empty-text="-">
          <el-table-column :label="t('system.engine.deletionAssessment.module')" min-width="130">
            <template #default="{ row }">{{ cleanupModuleLabel(row.module) }}</template>
          </el-table-column>
          <el-table-column :label="t('system.engine.deletionAssessment.status')" width="110">
            <template #default="{ row }">
              <el-tag size="small" :type="cleanupModuleStatusType(row.status)">
                {{ cleanupModuleStatusLabel(row.status) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="rebindable" :label="t('system.engine.deletionAssessment.rebindable')" width="90" align="right" />
          <el-table-column prop="willDisable" :label="t('system.engine.deletionAssessment.willDisable')" width="90" align="right" />
          <el-table-column prop="willDelete" :label="t('system.engine.deletionAssessment.willDelete')" width="90" align="right" />
          <el-table-column prop="running" :label="t('system.engine.deletionAssessment.running')" width="80" align="right" />
          <el-table-column prop="externalArtifact" :label="t('system.engine.deletionAssessment.externalArtifact')" width="90" align="right" />
          <el-table-column :label="t('system.engine.deletionAssessment.manage')" width="80" align="center">
            <template #default="{ row }">
              <el-link v-if="row.managementPath" :href="row.managementPath" target="_top" type="primary">
                {{ t('system.engine.deletionAssessment.open') }}
              </el-link>
              <span v-else>-</span>
            </template>
          </el-table-column>
        </el-table>

        <div class="deletion-confirmation">
          <div class="deletion-confirmation-label">
            {{ t('system.engine.deletionAssessment.confirmName', { name: deletionEngine?.name || '' }) }}
          </div>
          <el-input
            v-model="deletionConfirmation"
            :placeholder="deletionEngine?.name || ''"
            :disabled="!deletionAssessmentReady || deletionSubmitting"
          />
        </div>
      </div>

      <template #footer>
        <el-button @click="deletionDialogVisible = false">{{ t('system.engine.actions.cancel') }}</el-button>
        <el-button
          type="danger"
          :loading="deletionSubmitting"
          :disabled="!canConfirmEngineDeletion"
          @click="confirmEngineDeletion"
        >
          {{ t('system.engine.actions.cleanupAndDelete') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="jsonViewVisible"
      :title="t('system.engine.capabilityView.actions.viewJson')"
      width="720px"
      append-to-body
      destroy-on-close
    >
      <el-tree
        class="capability-json-tree"
        :data="selectedCapabilitiesView.json_view"
        node-key="key"
        default-expand-all
      >
        <template #default="{ data }">
          <span class="capability-json-node">
            <span class="capability-json-key">{{ data.key }}</span>
            <span v-if="data.value !== undefined && data.value !== ''" class="capability-json-value">
              {{ data.value }}
            </span>
          </span>
        </template>
      </el-tree>
    </el-dialog>
</template>

<script setup>
import { ref, onMounted, onUnmounted, computed, watch } from 'vue'
import { enginesAPI } from '../api/engines'
import { Plus, Edit, Delete } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { StorageEngineForm, requestConsoleBridge } from '@common-ui'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { useConsolePageDescriptor } from '@common-ui'
import { paginateEngines } from '../utils/engineList'
import { switchStorageEngineType } from '../utils/engineForm'
import { navigateSystemRoute } from '../utils/moduleNavigation'
import { resolveEngineDetailRouteState } from '../utils/routeState'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const allEngines = ref([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(10)
const total = computed(() => allEngines.value.length)
const engines = computed(() => paginateEngines(allEngines.value, currentPage.value, pageSize.value))
const deletionDialogVisible = ref(false)
const deletionEngine = ref(null)
const deletionAssessment = ref(null)
const deletionAssessmentError = ref('')
const deletionAssessmentID = ref('')
const deletionExternalPolicy = ref('delete')
const deletionConfirmation = ref('')
const deletionAssessing = ref(false)
const deletionSubmitting = ref(false)
let deletionAssessmentGeneration = 0

// 能力过滤
const selectedCategories = ref(['storage', 'compute', 'general', 'extension', 'builtin']) // 默认显示全部引擎

const selectedEngineCapabilityGroup = ref('')

// 资源表单对话框
const dialogVisible = ref(false)
const storageFormRef = ref(null)
const testing = ref(false)
const submitting = ref(false)
const isEdit = ref(false)
const editId = ref(null)
const editingEngine = ref(null)
const spatialWorkspaceCollapse = ref([])
const enablingSpatialWorkspace = ref(false)

// 扩展引擎注册对话框
const extensionDialogVisible = ref(false)
const extensionFormRef = ref(null)
const extensionSubmitting = ref(false)
const extensionTesting = ref(false)
const extensionRuntimeChecking = ref(false)
const extensionRuntimeStatus = ref('idle')
const extensionRuntimeStatusText = ref('')
const extensionProbeResult = ref('')
const extensionCapabilitiesText = ref('')
const extensionRuntimeProtocol = 'addp.workflow/v1'
const extensionForm = ref({
  engine_type: '',
  name: '',
  description: '',
  protocol: 'http',
  host: 'localhost',
  port: 8100
})

const mathWorkflowExample = {
  engine_type: 'math_workflow',
  protocol: 'http',
  host: 'localhost',
  port: 8089
}

const superMapWorkflowExample = {
  engine_type: 'supermap_workflow',
  protocol: 'http',
  host: 'localhost',
  port: 8103
}

const extensionRuntimeStatusTagType = computed(() => {
  const typeMap = {
    online: 'success',
    offline: 'danger',
    checking: 'warning'
  }
  return typeMap[extensionRuntimeStatus.value] || 'info'
})

// 引擎详情弹窗相关
const detailsVisible = ref(false)
const selectedEngine = ref(null)
useConsolePageDescriptor(router, 'system', {
  title: computed(() => t('system.engine.recentDetailTitle')),
  subject: computed(() => selectedEngine.value?.name || ''),
  ready: computed(() => Boolean(route.params.id && selectedEngine.value?.name))
})
const detailsLoading = ref(false)
const detailError = ref('')
const detailTab = ref('basic')
const jsonViewVisible = ref(false)

const form = ref({
  engine_type: '',
  name: '',
  description: '',
	lifecycle_state: 'active',
  connection_info: {}
})

const ENGINE_SCAN_POLICY_CHANNEL = 'engine-scan-policy'

const splitEngineAndScanPayload = (value) => {
  const { scan_config, ...enginePayload } = value || {}
  return {
    enginePayload,
    scanConfig: scan_config || null
  }
}

const defaultImmediateScanConfig = () => ({
  enabled: true,
  immediate_scan: true,
  immediate_depth: 'basic',
  scheduled_scan: false,
  schedule_mode: 'cron',
  cron_expression: '',
  schedule_time: '00:00',
  schedule_value: []
})

const normalizeScanConfig = (scanConfig) => {
  if (!scanConfig) return null
  const enabled = Boolean(scanConfig.immediate_scan || scanConfig.scheduled_scan)
  return {
    ...scanConfig,
    enabled
  }
}

const engineFromResponse = (response) => response?.data || response

const requestConsoleEngineScanPolicy = (payload) => {
  if (window.parent === window) {
    const scanConfig = normalizeScanConfig(payload.scanConfig)
    if (scanConfig?.enabled) {
      return Promise.reject(new Error('请通过 Console 入口维护元数据扫描计划'))
    }
    return Promise.resolve({})
  }

  return requestConsoleBridge(ENGINE_SCAN_POLICY_CHANNEL, payload, {
    source: 'addp-system',
    timeoutMessage: 'Console 扫描计划编排请求超时'
  })
}

const loadEngineScanConfig = async (engineId) => {
  try {
    const result = await requestConsoleEngineScanPolicy({
      action: 'load',
      engineId
    })
    return result.scanConfig || null
  } catch (error) {
    console.warn('load engine scan task failed', error)
    return null
  }
}

const syncEngineScanPolicy = async (engine, scanConfig, shouldTriggerImmediate) => {
  const normalized = normalizeScanConfig(scanConfig)
  if (!engine?.id) {
    return
  }
  await requestConsoleEngineScanPolicy({
    action: 'sync',
    engine: {
      id: engine.id,
      name: engine.name
    },
    scanConfig: normalized,
    triggerImmediate: shouldTriggerImmediate
  })
}

const syncEngineScanPolicyAfterSave = async (engine, scanConfig) => {
  try {
    await syncEngineScanPolicy(engine, scanConfig, Boolean(scanConfig?.immediate_scan))
    return true
  } catch (error) {
    const message = error.response?.data?.error || error.message || t('system.engine.msg.opFailed')
    ElMessage.warning(t('system.engine.msg.scanPolicySyncFailed', { error: message }))
    return false
  }
}

const dialogTitle = computed(() => {
  if (isEdit.value) return t('system.engine.dialog.edit')
  return t('system.engine.dialog.addStorage')
})

const storageEngineTypeOptions = computed(() => ([
  {
    value: 'postgresql',
    icon: '🐘',
    label: 'PostgreSQL',
    desc: t('system.engine.registerPanel.types.postgresql')
  },
  {
    value: 'oracle',
    icon: 'O',
    label: 'Oracle Database',
    desc: t('system.engine.registerPanel.types.oracle')
  },
  {
    value: 'mysql',
    icon: '🐬',
    label: 'MySQL',
    desc: t('system.engine.registerPanel.types.mysql')
  },
  {
    value: 'kafka',
    icon: '📨',
    label: 'Apache Kafka',
    desc: t('system.engine.registerPanel.types.kafka')
  },
  {
    value: 'doris',
    icon: '🟠',
    label: 'Apache Doris',
    desc: t('system.engine.registerPanel.types.doris')
  },
  {
    value: 'clickhouse',
    icon: '⚡',
    label: 'ClickHouse',
    desc: t('system.engine.registerPanel.types.clickhouse')
  },
  {
    value: 'mongodb',
    icon: '🍃',
    label: 'MongoDB',
    desc: t('system.engine.registerPanel.types.mongodb')
  },
  {
    value: 'minio',
    icon: '🪣',
    label: 'MinIO',
    desc: t('system.engine.registerPanel.types.minio')
  },
  {
    value: 'neo4j',
    icon: '🕸️',
    label: 'Neo4j',
    desc: t('system.engine.registerPanel.types.neo4j')
  },
  {
    value: 'nfs',
    icon: '📁',
    label: t('system.engine.typeNfs'),
    desc: t('system.engine.registerPanel.types.nfs')
  },
  {
    value: 'spark',
    icon: '✨',
    label: 'Apache Spark',
    desc: t('system.engine.registerPanel.types.spark')
  }
]))

const visibleStorageEngineTypeOptions = computed(() => {
  if (!isEdit.value) {
    return storageEngineTypeOptions.value
  }

  return storageEngineTypeOptions.value.filter(item => item.value === form.value.engine_type)
})

const selectedCapabilitiesView = computed(() => {
  const view = selectedEngine.value?.capabilities_view
  if (view && typeof view === 'object') {
    return {
      summary: Array.isArray(view.summary) ? view.summary : [],
      sections: Array.isArray(view.sections) ? view.sections : [],
      json_view: Array.isArray(view.json_view) ? view.json_view : []
    }
  }

  return {
    summary: [],
    sections: [],
    json_view: []
  }
})

const hasSelectedCapabilitiesView = computed(() => {
  const view = selectedCapabilitiesView.value
  return view.summary.length > 0 || view.sections.length > 0 || view.json_view.length > 0
})

const superMapWorkspaces = computed(() => findSuperMapSpatialWorkspaces(editingEngine.value))

const showSpatialWorkspacePanel = computed(() => {
  return Boolean(
    isEdit.value &&
    form.value.engine_type === 'postgresql' &&
    superMapWorkspaces.value.length > 0
  )
})

const workspaceProductKey = (workspace) => {
  return String(workspace?.kind || '').toLowerCase() === 'sdx_postgresql'
    ? 'system.engine.spatialWorkspace.postgresql'
    : 'system.engine.spatialWorkspace.postgis'
}

const workspaceKey = (workspace) => `supermap-${String(workspace?.kind || 'unknown').toLowerCase()}`

const canEnableSuperMapWorkspace = (workspace) => {
  const kind = String(workspace?.kind || '').toLowerCase()
  return Boolean(workspace?.can_enable && (kind === 'sdx_postgis' || workspace?.bound_runtime_engine_id))
}

const spatialWorkspaceStateText = (workspace) => {
  const state = workspace?.state || 'unknown'
  return translateCapabilityKey(`system.engine.capabilityView.values.${capabilityKeySegment(state)}`, state)
}

const spatialWorkspaceRiskText = (workspace) => {
  const risk = workspace?.risk_level || 'unknown'
  return translateCapabilityKey(`system.engine.capabilityView.values.${capabilityKeySegment(risk)}`, risk)
}

const spatialWorkspaceStateTagType = (workspace) => {
  const state = String(workspace?.state || '').toLowerCase()
  if (state === 'detected' || state === 'enabled') return 'success'
  if (state === 'not_detected') return 'warning'
  return 'warning'
}

// 对连接配置字段进行排序显示
const sortedConnectionInfo = computed(() => {
  if (!selectedEngine.value?.connection_info) {
    return []
  }

  const fieldOrder = ['host', 'port', 'database', 'user', 'password', 'sslmode']
  const connectionInfo = selectedEngine.value.connection_info
  const entries = Object.entries(connectionInfo)

  const sorted = entries.sort((a, b) => {
    const [keyA] = a
    const [keyB] = b
    const indexA = fieldOrder.indexOf(keyA)
    const indexB = fieldOrder.indexOf(keyB)

    if (indexA !== -1 && indexB === -1) return -1
    if (indexA === -1 && indexB !== -1) return 1
    if (indexA !== -1 && indexB !== -1) return indexA - indexB
    return keyA.localeCompare(keyB)
  })

  return sorted
})

const normalizeCapabilitiesView = (view) => {
  if (!view || typeof view !== 'object') {
    return { summary: [], sections: [], json_view: [] }
  }
  return {
    summary: Array.isArray(view.summary) ? view.summary : [],
    sections: Array.isArray(view.sections) ? view.sections : [],
    json_view: Array.isArray(view.json_view) ? view.json_view : []
  }
}

const normalizeCapabilitiesObject = (capabilities) => {
  if (!capabilities) return null
  if (typeof capabilities === 'string') {
    try {
      return JSON.parse(capabilities)
    } catch {
      return null
    }
  }
  return typeof capabilities === 'object' ? capabilities : null
}

const spatialWorkspacesFromEngine = (engine) => {
  const capabilities = normalizeCapabilitiesObject(engine?.capabilities)
  const workspaces = capabilities?.extensions?.spatial_workspaces
  return Array.isArray(workspaces) ? workspaces : []
}

const findSuperMapSpatialWorkspaces = (engine) => {
  return spatialWorkspacesFromEngine(engine).filter(workspace => (
    String(workspace?.ecosystem || '').toLowerCase() === 'supermap' &&
    ['sdx_postgis', 'sdx_postgresql'].includes(String(workspace?.kind || '').toLowerCase())
  ))
}

const getCapabilitySummaryTags = (engine) => {
  const summary = normalizeCapabilitiesView(engine.capabilities_view).summary
  return summary.length > 0
    ? summary
    : [{ id: 'none', label_key: 'system.engine.capabilities.none', status: 'unknown' }]
}

const handleFilterChange = () => {
  currentPage.value = 1
  loadEngines()
}

const handlePageSizeChange = () => {
  currentPage.value = 1
}

const getEngineTypeLabel = (type, engine = null) => {
  if (engine?.name && engine.engine_type === type) {
    return type
  }
  return humanizeCapabilityValue(type)
}

const getEngineTypeColor = (type) => {
  return type ? 'info' : 'info'
}

const canEditEngine = (row) => {
	return !row.is_builtin && row.engine_origin === 'general' && row.lifecycle_state !== 'deleting'
}

const getLifecycleTagType = (state) => ({
	active: 'success',
	disabled: 'info',
	deleting: 'warning'
}[state] || 'info')

const getLifecycleLabel = (state) => t(`system.engine.status.${state || 'disabled'}`)

const formatDate = (dateString) => {
  return new Date(dateString).toLocaleString()
}

const translateCapabilityKey = (key, fallback = '') => {
  if (!key) return humanizeCapabilityValue(fallback)
  const translated = t(key)
  if (translated && translated !== key) return translated
  return humanizeCapabilityValue(fallback || key.split('.').pop())
}

const humanizeCapabilityValue = (value) => {
  if (value === undefined || value === null || value === '') return '-'
  return String(value)
    .replace(/[_-]+/g, ' ')
    .replace(/\b\w/g, char => char.toUpperCase())
}

const getCapabilityStatusTagType = (status) => {
  const typeMap = {
    available: 'success',
    engine_unavailable: 'info',
    installed: 'success',
    not_installed: 'info'
  }
  return typeMap[status] || 'info'
}

const getCapabilityStatusLabel = (status) => {
  const key = `system.engine.capabilityView.status.${status || 'unknown'}`
  return translateCapabilityKey(key, status || 'unknown')
}

const getCapabilityViewText = (item) => {
  if (!item) return '-'
  const base = item.value_key
    ? translateCapabilityKey(item.value_key, item.value)
    : translateCapabilityKey(item.label_key, item.value || item.id)
  if (item.value && item.value_key && base !== item.value) {
    return base
  }
  return item.value && !item.value_key ? `${base}: ${item.value}` : base
}

const getCapabilityPathLabel = (node) => {
  if (!node) return '-'
  return translateCapabilityKey(node.label_key, node.value || node.id)
}

const getCapabilityItemLabel = (item) => {
  return translateCapabilityKey(item.label_key, item.value || item.id)
}

const getCapabilityItemValue = (item) => {
  if (!item) return ''
  if (item.value_key) return translateCapabilityKey(item.value_key, item.value)
  return item.value || ''
}

const translateCapabilityTagValue = (tag) => {
  if (!tag?.value || !tag?.id) return tag?.value || ''
  const prefix = String(tag.id).endsWith(`_${tag.value}`)
    ? String(tag.id).slice(0, -String(tag.value).length - 1)
    : ''
  if (prefix) {
    const key = `system.engine.capabilityView.${capabilityKeyNamespace(prefix)}.${capabilityKeySegment(tag.value)}`
    const translated = t(key)
    if (translated && translated !== key) return translated
  }
  if (String(tag.id) === 'default_language') {
    const key = `system.engine.capabilityView.language.${capabilityKeySegment(tag.value)}`
    const translated = t(key)
    if (translated && translated !== key) return translated
  }
  return tag.value
}

const capabilityKeyNamespace = (prefix) => {
  return String(prefix).replace(/_([a-z])/g, (_, char) => char.toUpperCase())
}

const capabilityKeySegment = (value) => {
  return String(value).replace(/[_-]+([a-zA-Z0-9])/g, (_, char) => char.toUpperCase())
}

const getCapabilityTagText = (tag) => {
  if (!tag) return '-'
  if (tag.label_key) {
    const label = translateCapabilityKey(tag.label_key, tag.value || tag.id)
    if (tag.value && tag.label_key.includes('.values.')) {
      return `${label}: ${translateCapabilityTagValue(tag)}`
    }
    return label
  }
  return translateCapabilityTagValue(tag) || humanizeCapabilityValue(tag.id)
}

// 获取最近检测结果标签
const getConnectionStatusLabel = (status) => {
  const labelMap = {
    'online': t('system.engine.connection.online'),
    'offline': t('system.engine.connection.offline'),
    'unknown': t('system.engine.connection.unknown'),
    'checking': t('system.engine.connection.checking')
  }
  return labelMap[status] || t('system.engine.connection.notChecked')
}

// 获取最近检测结果图标 CSS class
const getConnectionStatusClass = (status) => {
  const classMap = {
    'online': 'status-online',
    'offline': 'status-offline',
    'unknown': 'status-unknown',
    'checking': 'status-checking'
  }
  return classMap[status] || 'status-unknown'
}

// 获取最近检测结果提示信息
const getConnectionTooltip = (row) => {
  if (!row.connection_status) return t('system.engine.connection.notChecked')

  let tooltip = t('system.engine.connection.statusLine', { status: getConnectionStatusLabel(row.connection_status) })

  if (row.last_check_at) {
    tooltip += `\n${t('system.engine.connection.lastCheck', { time: formatDate(row.last_check_at) })}`
  }

  if (row.check_message) {
    tooltip += `\n${t('system.engine.connection.detail', { msg: row.check_message })}`
  }

  return tooltip
}

const loadEngines = async () => {
  loading.value = true
  try {
    const capabilityGroups = ['storage', 'compute'].filter(value => selectedCategories.value.includes(value))
    const engineOrigins = ['general', 'extension'].filter(value => selectedCategories.value.includes(value))
    if (capabilityGroups.length === 0 && engineOrigins.length === 0) {
      allEngines.value = []
      return
    }

    const response = await enginesAPI.list({
      capabilityGroups,
      engineOrigins,
      lifecycleStates: ['active', 'disabled', 'deleting'],
      includeBuiltin: selectedCategories.value.includes('builtin')
    })
    if (!Array.isArray(response)) {
      throw new TypeError('System engine list response must be an array')
    }
    allEngines.value = response
    const lastPage = Math.max(1, Math.ceil(response.length / pageSize.value))
    currentPage.value = Math.min(currentPage.value, lastPage)
  } catch (error) {
    ElMessage.error(t('system.engine.msg.loadFailed'))
    console.error(error)
  } finally {
    loading.value = false
  }
}

const selectStorageEngineType = (engineType) => {
  if (isEdit.value) return
  if (form.value.engine_type === engineType) return

  form.value = switchStorageEngineType(form.value, engineType)
}

const showAddStorageDialog = () => {
  isEdit.value = false
  editId.value = null
  selectedEngineCapabilityGroup.value = 'storage'
  resetForm()
  form.value = {
    ...form.value,
    engine_type: 'postgresql'
  }
  dialogVisible.value = true
}

const showAddExtensionDialog = () => {
  resetExtensionForm()
  extensionDialogVisible.value = true
}

const fillMathWorkflowExample = () => {
  extensionForm.value = {
    ...mathWorkflowExample,
    name: t('system.engine.extensionForm.mathExampleName'),
    description: t('system.engine.extensionForm.mathExampleDescription')
  }
  extensionCapabilitiesText.value = ''
  extensionProbeResult.value = ''
  resetExtensionRuntimeStatus()
}

const fillSuperMapWorkflowExample = () => {
  extensionForm.value = {
    ...superMapWorkflowExample,
    name: t('system.engine.extensionForm.superMapExampleName'),
    description: t('system.engine.extensionForm.superMapExampleDescription')
  }
  extensionCapabilitiesText.value = ''
  extensionProbeResult.value = ''
  resetExtensionRuntimeStatus()
}

const parseExtensionCapabilities = () => {
  const text = extensionCapabilitiesText.value.trim()
  if (!text) return null

  try {
    return JSON.parse(text)
  } catch (error) {
    ElMessage.error(t('system.engine.msg.capabilitiesJsonInvalid'))
    return false
  }
}

const buildDefaultWorkflowCapabilities = (engineType) => ({
  schema_version: 'engine.capabilities/v1',
  engine_type: engineType,
  engine_family: 'workflow',
  compute: {
    workflow: {
      supported: true,
      runtime_api: extensionRuntimeProtocol,
      dynamic_operators: true,
      supported_operator_mode: ['workflow', 'direct']
    }
  }
})

const extensionCreateSuccessMessage = (probe) => {
  const operatorsCount = Number(probe?.operators_count)
  if (Number.isFinite(operatorsCount)) {
    return t('system.engine.msg.extensionCreateSuccessWithProbe', { count: operatorsCount })
  }
  return t('system.engine.msg.createSuccess')
}

const extensionProbeSuccessMessage = (probe) => {
  const operatorsCount = Number(probe?.operators_count)
  if (Number.isFinite(operatorsCount)) {
    return t('system.engine.msg.extensionProbeSuccessWithOperators', { count: operatorsCount })
  }
  return t('system.engine.msg.extensionProbeSuccess')
}

const buildExtensionPayload = () => {
  const engineType = String(extensionForm.value.engine_type || '').trim()
  const name = String(extensionForm.value.name || '').trim()
  const host = String(extensionForm.value.host || '').trim()
  const protocol = String(extensionForm.value.protocol || 'http').trim()
  const port = Number(extensionForm.value.port)

  if (!engineType || !name || !host || !protocol || !Number.isInteger(port) || port <= 0) {
    ElMessage.warning(t('system.engine.msg.fillRequired'))
    return null
  }

  const capabilities = parseExtensionCapabilities()
  if (capabilities === false) {
    return null
  }

  const payload = {
    engine_type: engineType,
    engine_origin: 'extension',
    name,
    description: extensionForm.value.description,
    connection_info: {
      protocol,
      host,
      port
    }
  }
  payload.capabilities = capabilities || buildDefaultWorkflowCapabilities(engineType)
  return payload
}

const probeExtensionPayload = async (payload) => {
  const probeResponse = await enginesAPI.testConnectionBeforeCreate(payload)
  if (!probeResponse?.success) {
    throw new Error(probeResponse?.error || probeResponse?.message || t('system.engine.msg.opFailed'))
  }
  return probeResponse
}

const testExtensionBeforeCreate = async () => {
  const payload = buildExtensionPayload()
  if (!payload) return

  extensionTesting.value = true
  try {
    const probeResponse = await probeExtensionPayload(payload)
    extensionProbeResult.value = extensionProbeSuccessMessage(probeResponse.probe)
    setExtensionRuntimeOnline(probeResponse.probe)
    ElMessage.success(extensionProbeResult.value)
  } catch (error) {
    extensionProbeResult.value = ''
    setExtensionRuntimeOffline(error)
    ElMessage.error(t('system.engine.msg.testFailed', { error: error.response?.data?.error || error.message }))
  } finally {
    extensionTesting.value = false
  }
}

const setExtensionRuntimeOnline = (probe) => {
  extensionRuntimeStatus.value = 'online'
  extensionRuntimeStatusText.value = extensionProbeSuccessMessage(probe)
}

const setExtensionRuntimeOffline = (error) => {
  extensionRuntimeStatus.value = 'offline'
  const message = error?.response?.data?.error || error?.message || t('system.engine.msg.opFailed')
  extensionRuntimeStatusText.value = t('system.engine.extensionForm.runtimeOffline', { error: message })
}

const resetExtensionRuntimeStatus = () => {
  extensionRuntimeStatus.value = 'idle'
  extensionRuntimeStatusText.value = ''
}

const checkExtensionRuntimeStatus = async () => {
  const payload = buildExtensionPayload()
  if (!payload) return

  extensionRuntimeChecking.value = true
  extensionRuntimeStatus.value = 'checking'
  extensionRuntimeStatusText.value = t('system.engine.extensionForm.runtimeChecking')
  try {
    const probeResponse = await probeExtensionPayload(payload)
    setExtensionRuntimeOnline(probeResponse.probe)
  } catch (error) {
    setExtensionRuntimeOffline(error)
  } finally {
    extensionRuntimeChecking.value = false
  }
}

const submitExtensionForm = async () => {
  const payload = buildExtensionPayload()
  if (!payload) return

  extensionSubmitting.value = true
  try {
    const probeResponse = await probeExtensionPayload(payload)
    await enginesAPI.create(payload)
    ElMessage.success(extensionCreateSuccessMessage(probeResponse.probe))
    extensionDialogVisible.value = false
    await loadEngines()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || error.message || t('system.engine.msg.opFailed'))
  } finally {
    extensionSubmitting.value = false
  }
}

const editEngine = async (row) => {
  if (!canEditEngine(row)) {
    ElMessage.warning(t('system.engine.msg.extensionEditUnsupported'))
    return
  }

  isEdit.value = true
  editId.value = row.id
  editingEngine.value = row
  spatialWorkspaceCollapse.value = []

  selectedEngineCapabilityGroup.value = 'storage'

  const scanConfig = await loadEngineScanConfig(row.id)
  form.value = {
    engine_type: row.engine_type,
    name: row.name,
    description: row.description,
		lifecycle_state: row.lifecycle_state,
    connection_info: { ...row.connection_info },
    ...(scanConfig ? { scan_config: scanConfig } : {})
  }

  dialogVisible.value = true
}

const enableSuperMapSpatialWorkspace = async (workspace) => {
  if (!canEnableSuperMapWorkspace(workspace)) {
    ElMessage.warning(t(`${workspaceProductKey(workspace)}.unavailable`))
    return
  }

  try {
    await ElMessageBox.confirm(
      t(`${workspaceProductKey(workspace)}.confirmMessage`),
      t(`${workspaceProductKey(workspace)}.confirmTitle`),
      {
        confirmButtonText: t(`${workspaceProductKey(workspace)}.enable`),
        cancelButtonText: t('system.engine.actions.cancel'),
        type: 'error'
      }
    )
  } catch {
    return
  }

  enablingSpatialWorkspace.value = true
  try {
    const response = await enginesAPI.enableSpatialWorkspace(editId.value, workspace.ecosystem, workspace.kind)
    const updatedEngine = engineFromResponse(response)
    if (updatedEngine) {
      editingEngine.value = updatedEngine
    }
    ElMessage.success(t(`${workspaceProductKey(workspace)}.enableSuccess`))
    await loadEngines()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || error.message || t('system.engine.msg.opFailed'))
  } finally {
    enablingSpatialWorkspace.value = false
  }
}

const testBeforeCreate = async () => {
  const formRef = storageFormRef.value
  const valid = await formRef?.validate()
  if (!valid) {
    ElMessage.warning(t('system.engine.msg.fillRequired'))
    return
  }

  testing.value = true
  try {
    const response = isEdit.value
      ? await enginesAPI.testExistingConnection(editId.value, splitEngineAndScanPayload(form.value).enginePayload)
      : await enginesAPI.testConnection(splitEngineAndScanPayload(form.value).enginePayload)

    if (response.success) {
      ElMessage.success(t('system.engine.msg.testSuccess'))
    } else {
      ElMessage.error(t('system.engine.msg.testFailed', { error: response.error || response.message }))
    }
  } catch (error) {
    ElMessage.error(t('system.engine.msg.testFailed', { error: error.response?.data?.error || error.message }))
  } finally {
    testing.value = false
    if (isEdit.value) {
      await loadEngines()
    }
  }
}

const testConnection = async (row) => {
  try {
    const response = await enginesAPI.testExistingConnection(row.id)
    if (response.success) {
      ElMessage.success(t('system.engine.msg.testSuccess'))
      await loadEngines()
    } else {
      ElMessage.error(t('system.engine.msg.testFailed', { error: response.error || response.message }))
      await loadEngines()
    }
  } catch (error) {
    ElMessage.error(t('system.engine.msg.testFailed', { error: error.response?.data?.error || error.message }))
    await loadEngines()
  }
}

const submitForm = async () => {
  const formRef = storageFormRef.value
  const valid = await formRef?.validate()
  if (!valid) return

  submitting.value = true
  try {
    const { enginePayload, scanConfig } = splitEngineAndScanPayload(form.value)
    let submitData = { ...enginePayload }

    if (isEdit.value) {
      const response = await enginesAPI.update(editId.value, submitData)
      await syncEngineScanPolicyAfterSave(engineFromResponse(response) || { id: editId.value, name: submitData.name }, scanConfig || defaultImmediateScanConfig())
      ElMessage.success(t('system.engine.msg.updateSuccess'))
    } else {
      const response = await enginesAPI.create(submitData)
      await syncEngineScanPolicyAfterSave(engineFromResponse(response), scanConfig || defaultImmediateScanConfig())
      ElMessage.success(t('system.engine.msg.createSuccess'))
    }
    dialogVisible.value = false
    loadEngines()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || error.message || t('system.engine.msg.opFailed'))
  } finally {
    submitting.value = false
  }
}

const deleteEngine = async (row) => {
  if (row.is_builtin) {
    ElMessage.warning(t('system.engine.msg.builtinCannotDelete'))
    return
  }

	deletionEngine.value = row
	deletionExternalPolicy.value = 'delete'
	deletionConfirmation.value = ''
	deletionDialogVisible.value = true
	await runDeletionAssessment()
}

const unwrapAPIData = (value) => value?.data || value

const waitForDeletionAssessment = async (engineID, assessmentID, generation) => {
	for (let attempt = 0; attempt < 45; attempt += 1) {
		if (generation !== deletionAssessmentGeneration || !deletionDialogVisible.value) return
		const response = await enginesAPI.getDeletionAssessment(engineID, assessmentID)
		const assessment = unwrapAPIData(response)
		deletionAssessment.value = assessment
		if (['completed', 'completed_with_errors', 'failed', 'timeout'].includes(assessment?.status)) {
			if (assessment.status !== 'completed') {
				deletionAssessmentError.value = t('system.engine.deletionAssessment.failedStatus', { status: assessment.status })
			}
			return
		}
		await new Promise(resolve => window.setTimeout(resolve, 1000))
	}
	throw new Error(t('system.engine.deletionAssessment.timeout'))
}

const runDeletionAssessment = async () => {
	if (!deletionEngine.value?.id) return
	const generation = ++deletionAssessmentGeneration
	deletionAssessing.value = true
	deletionAssessment.value = null
	deletionAssessmentID.value = ''
	deletionAssessmentError.value = ''
	deletionConfirmation.value = ''
	try {
		const response = await enginesAPI.createDeletionAssessment(deletionEngine.value.id, {
			external_artifact_policy: deletionExternalPolicy.value
		})
		const assessmentID = unwrapAPIData(response)?.assessment_id
		if (!assessmentID) throw new Error(t('system.engine.deletionAssessment.invalidResponse'))
		deletionAssessmentID.value = assessmentID
		await waitForDeletionAssessment(deletionEngine.value.id, assessmentID, generation)
	} catch (error) {
		if (generation !== deletionAssessmentGeneration) return
		deletionAssessmentError.value = error.response?.data?.error || error.message || t('system.engine.msg.opFailed')
	} finally {
		if (generation === deletionAssessmentGeneration) deletionAssessing.value = false
	}
}

const deletionImpact = computed(() => {
	const summary = deletionAssessment.value?.summary?.impact
	if (summary) return summary
	return deletionModuleRows.value.reduce((total, row) => ({
		rebindable: total.rebindable + row.rebindable,
		will_disable: total.will_disable + row.willDisable,
		will_delete: total.will_delete + row.willDelete,
		running: total.running + row.running,
		external_artifact: total.external_artifact + row.externalArtifact
	}), { rebindable: 0, will_disable: 0, will_delete: 0, running: 0, external_artifact: 0 })
})

const deletionModuleRows = computed(() => {
	const assessment = deletionAssessment.value
	const expectedModules = assessment?.task?.expected_modules || []
	const results = assessment?.results || {}
	return expectedModules.map(module => {
		const result = results[module] || {}
		const summary = result.impact?.summary || {}
		return {
			module,
			status: result.status || assessment?.progress?.modules?.[module] || 'pending',
			rebindable: summary.rebindable || 0,
			willDisable: summary.will_disable || 0,
			willDelete: summary.will_delete || 0,
			running: summary.running || 0,
			externalArtifact: summary.external_artifact || 0,
			managementPath: result.impact?.management_path || ''
		}
	})
})

const deletionImpactCards = computed(() => ([
	{ key: 'rebindable', label: t('system.engine.deletionAssessment.rebindable'), value: deletionImpact.value.rebindable || 0 },
	{ key: 'willDisable', label: t('system.engine.deletionAssessment.willDisable'), value: deletionImpact.value.will_disable || 0 },
	{ key: 'willDelete', label: t('system.engine.deletionAssessment.willDelete'), value: deletionImpact.value.will_delete || 0 },
	{ key: 'running', label: t('system.engine.deletionAssessment.running'), value: deletionImpact.value.running || 0 },
	{ key: 'externalArtifact', label: t('system.engine.deletionAssessment.externalArtifact'), value: deletionImpact.value.external_artifact || 0 }
]))

const deletionAssessmentReady = computed(() => (
	deletionAssessment.value?.status === 'completed' &&
	!deletionAssessmentError.value &&
	(deletionImpact.value.running || 0) === 0 &&
	deletionModuleRows.value.every(row => row.status === 'success')
))

const canConfirmEngineDeletion = computed(() => (
	deletionAssessmentReady.value &&
	deletionConfirmation.value === deletionEngine.value?.name &&
	!deletionSubmitting.value
))

const cleanupModuleLabel = (module) => {
	const key = `system.cleanup.modules.names.${module}`
	const label = t(key)
	return label === key ? module : label
}

const cleanupModuleStatusLabel = (status) => {
	const key = `system.engine.deletionAssessment.statuses.${status || 'pending'}`
	const label = t(key)
	return label === key ? status : label
}

const cleanupModuleStatusType = (status) => ({
	success: 'success',
	failed: 'danger',
	partial_success: 'danger',
	timeout: 'danger',
	running: 'warning',
	pending: 'info'
}[status] || 'info')

const confirmEngineDeletion = async () => {
	if (!canConfirmEngineDeletion.value) return
	deletionSubmitting.value = true
	try {
		await enginesAPI.delete(deletionEngine.value.id, {
			assessment_id: deletionAssessmentID.value,
			confirmation_token: deletionConfirmation.value,
			external_artifact_policy: deletionExternalPolicy.value
		})
		ElMessage.success(t('system.engine.msg.deleteStarted'))
		deletionDialogVisible.value = false
		await loadEngines()
	} catch (error) {
		deletionAssessmentError.value = error.response?.data?.error || error.message || t('system.engine.msg.opFailed')
	} finally {
		deletionSubmitting.value = false
	}
}

const resetDeletionDialog = () => {
	deletionAssessmentGeneration += 1
	deletionEngine.value = null
	deletionAssessment.value = null
	deletionAssessmentError.value = ''
	deletionAssessmentID.value = ''
	deletionExternalPolicy.value = 'delete'
	deletionConfirmation.value = ''
	deletionAssessing.value = false
}

const availableDetailTabs = computed(() => {
  const tabs = ['basic']
  if (selectedEngine.value?.connection_info && Object.keys(selectedEngine.value.connection_info).length > 0) {
    tabs.push('connection')
  }
  if (hasSelectedCapabilitiesView.value) tabs.push('capabilities')
  return tabs
})

const restoreDetailTab = async () => {
  if (!route.params.id || !selectedEngine.value) return
  const routeState = resolveEngineDetailRouteState(availableDetailTabs.value, route.query)
  detailTab.value = routeState.activeTab
  if (routeState.changed) {
    await navigateSystemRoute(router, {
      name: 'EngineDetail',
      params: { id: String(route.params.id) },
      query: routeState.query
    }, { history: 'replace' })
  }
}

const loadEngineDetails = async (engineID) => {
  detailsLoading.value = true
  detailsVisible.value = true
  detailError.value = ''
  detailTab.value = 'basic'
  jsonViewVisible.value = false
  selectedEngine.value = null

  try {
    const response = await enginesAPI.getById(engineID)
    selectedEngine.value = response
    await restoreDetailTab()
  } catch (error) {
    detailError.value = t('system.engine.msg.detailFailed')
    console.error(error)
  } finally {
    detailsLoading.value = false
  }
}

const restoreEngineDetails = async () => {
  const engineID = String(route.params.id || '').trim()
  if (!engineID) {
    detailsVisible.value = false
    selectedEngine.value = null
    detailError.value = ''
    return
  }
  if (String(selectedEngine.value?.id || '') !== engineID) {
    await loadEngineDetails(engineID)
    return
  }
  detailsVisible.value = true
  await restoreDetailTab()
}

const viewEngineDetails = async (row) => {
  await navigateSystemRoute(router, {
    name: 'EngineDetail',
    params: { id: String(row.id) }
  })
}

const selectDetailTab = async (tab) => {
  const tabName = String(tab || '')
  if (!route.params.id || !availableDetailTabs.value.includes(tabName)) return
  detailTab.value = tabName
  await navigateSystemRoute(router, {
    name: 'EngineDetail',
    params: { id: String(route.params.id) },
    query: tabName === 'basic' ? {} : { tab: tabName }
  }, { history: 'replace' })
}

const closeEngineDetails = () => {
  detailsVisible.value = false
}

const handleDetailsClosed = async () => {
  if (!route.params.id) return
  await navigateSystemRoute(router, { name: 'Engines' }, { history: 'replace' })
}

// 表格行样式
const tableRowClassName = ({ row }) => {
  return row.is_builtin ? 'builtin-engine-row' : ''
}

const resetForm = () => {
	form.value = {
    engine_type: '',
    name: '',
    description: '',
		lifecycle_state: 'active',
    connection_info: {}
  }
  editingEngine.value = null
  spatialWorkspaceCollapse.value = []
  enablingSpatialWorkspace.value = false
  storageFormRef.value?.reset()
}

const resetExtensionForm = () => {
  extensionForm.value = {
    engine_type: '',
    name: '',
    description: '',
    protocol: 'http',
    host: 'localhost',
    port: 8100
  }
  extensionCapabilitiesText.value = ''
  extensionProbeResult.value = ''
  resetExtensionRuntimeStatus()
  extensionFormRef.value?.clearValidate?.()
}

watch(extensionForm, () => {
  extensionProbeResult.value = ''
  resetExtensionRuntimeStatus()
}, { deep: true })

watch(extensionCapabilitiesText, () => {
  extensionProbeResult.value = ''
  resetExtensionRuntimeStatus()
})

let deletionPollTimer = null

onMounted(() => {
		loadEngines()
		restoreEngineDetails()
	deletionPollTimer = window.setInterval(() => {
		if (allEngines.value.some(engine => engine.lifecycle_state === 'deleting')) {
			loadEngines()
		}
	}, 3000)
})

watch(() => [route.params.id, route.query.tab], restoreEngineDetails)

onUnmounted(() => {
	if (deletionPollTimer) window.clearInterval(deletionPollTimer)
})
</script>

<style scoped>
.deletion-alert {
  margin-bottom: 16px;
}

.deletion-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}

.deletion-toolbar-label {
  color: var(--addp-text-secondary);
}

.deletion-content {
  min-height: 260px;
}

.impact-summary {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  border: 1px solid var(--addp-border-color);
  margin-bottom: 16px;
}

.impact-summary-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  padding: 12px 8px;
  border-right: 1px solid var(--addp-border-color-light);
}

.impact-summary-item:last-child {
  border-right: 0;
}

.impact-summary-value {
  color: var(--addp-text-primary);
  font-size: 20px;
  font-weight: 600;
}

.impact-summary-label {
  color: var(--addp-text-secondary);
  font-size: 12px;
  text-align: center;
}

.deletion-confirmation {
  margin-top: 18px;
}

.deletion-confirmation-label {
  margin-bottom: 8px;
  color: var(--addp-text-primary);
}

@media (max-width: 900px) {
  .impact-summary {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .impact-summary-item {
    border-bottom: 1px solid var(--addp-border-color-light);
  }
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
}

.header-buttons {
  display: flex;
  gap: 10px;
}

/* 过滤栏样式 */
.filter-bar {
  display: flex;
  align-items: center;
  padding: 16px 0;
  margin-bottom: 16px;
  border-bottom: 1px solid var(--addp-border-color-light);
}

.filter-label {
  font-weight: 500;
  margin-right: 16px;
  color: var(--addp-text-secondary);
}

/* 通用存储引擎注册双栏布局 */
.storage-layout {
  display: flex;
  gap: 12px;
  align-items: flex-start;
}

.engine-type-sidebar {
  width: 220px;
  flex-shrink: 0;
  border: 1px solid var(--addp-border-color);
  border-radius: 10px;
  background: var(--addp-bg-secondary);
  padding: 12px;
  display: flex;
  flex-direction: column;
}

.sidebar-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.sidebar-subtitle {
  margin-top: 4px;
  margin-bottom: 8px;
  font-size: 12px;
  color: var(--addp-text-secondary);
}

.engine-type-list {
  max-height: 70vh;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-right: 2px;
}

.engine-type-item {
  width: 100%;
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  background: var(--addp-bg-primary);
  color: inherit;
  text-align: center;
  padding: 10px 8px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  cursor: pointer;
  transition: border-color 0.2s ease, background-color 0.2s ease, transform 0.2s ease;
}

.engine-type-item:hover {
  border-color: var(--el-color-primary);
  transform: translateY(-1px);
}

.engine-type-item.is-active {
  border-color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}

.engine-type-item.is-disabled {
  cursor: not-allowed;
  opacity: 0.85;
}

.engine-type-item.is-disabled:hover {
  transform: none;
}

.engine-type-icon {
  font-size: 40px;
  line-height: 1;
  width: 56px;
  text-align: center;
  flex-shrink: 0;
}

.engine-type-name {
  font-size: 12px;
  font-weight: 600;
  color: var(--addp-text-primary);
  line-height: 1.2;
}

.sidebar-hint {
  margin-top: 10px;
  font-size: 12px;
  color: var(--el-color-warning);
}

.storage-form-panel {
  flex: 1;
  min-width: 0;
}

.spatial-workspace-collapse {
  margin-top: 12px;
  border: 1px solid var(--addp-border-color-light);
  border-radius: 8px;
  overflow: hidden;
}

.spatial-workspace-title {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  font-size: 13px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.spatial-workspace-alert {
  margin-bottom: 12px;
}

.spatial-workspace-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.spatial-workspace-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.extension-form {
  padding-right: 12px;
}

.extension-example-bar {
  margin-left: 150px;
  margin-bottom: 16px;
  width: calc(100% - 150px);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.extension-example-info,
.extension-example-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.extension-example-info {
  min-width: 0;
  flex-wrap: wrap;
}

.extension-example-actions {
  flex-shrink: 0;
}

.extension-probe-alert {
  margin-left: 150px;
  width: calc(100% - 150px);
}

.capability-detail {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.capability-toolbar {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.capability-summary {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  min-width: 0;
}

.capability-card {
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  background: var(--addp-bg-primary);
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.capability-card-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.capability-section-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.capability-section-desc,
.capability-item-reason,
.capability-item-value {
  margin-top: 4px;
  font-size: 12px;
  color: var(--addp-text-secondary);
  line-height: 1.5;
}

.capability-path {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  padding: 10px;
  border-radius: 6px;
  background: var(--addp-bg-secondary);
}

.capability-path-node {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  min-height: 28px;
  padding: 4px 8px;
  border: 1px solid var(--addp-border-color-light);
  border-radius: 6px;
  color: var(--addp-text-primary);
  background: var(--addp-bg-primary);
  font-size: 12px;
}

.capability-path-arrow {
  color: var(--addp-text-tertiary);
}

.capability-items {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 10px;
}

.capability-item {
  min-width: 0;
  padding: 10px;
  border: 1px solid var(--addp-border-color-light);
  border-radius: 6px;
  background: var(--addp-bg-secondary);
}

.capability-item-main {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 8px;
}

.capability-item-label {
  font-size: 13px;
  font-weight: 600;
  color: var(--addp-text-primary);
  line-height: 1.5;
}

.capability-item-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
  margin-top: 8px;
}

.capability-json-tree {
  max-height: 60vh;
  overflow: auto;
  padding: 8px;
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  background: var(--addp-bg-primary);
}

.capability-json-node {
  display: inline-flex;
  align-items: baseline;
  gap: 8px;
  min-width: 0;
  font-size: 12px;
}

.capability-json-key {
  font-weight: 600;
  color: var(--addp-text-primary);
}

.capability-json-value {
  color: var(--addp-text-secondary);
  word-break: break-all;
}

/* 内置引擎行样式 */
:deep(.builtin-engine-row) {
  background-color: var(--addp-bg-secondary);
}

:deep(.builtin-engine-row:hover) {
  background-color: var(--addp-bg-primary) !important;
}

/* 最近检测结果图标样式 */
.connection-status-icon {
  display: inline-block;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  cursor: help;
  transition: all 0.3s;
}

.connection-status-cell {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  white-space: nowrap;
}

.connection-status-label {
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.connection-status-icon:hover {
  transform: scale(1.2);
}

.status-online {
  background-color: var(--el-color-success);
  box-shadow: 0 0 6px rgba(103, 194, 58, 0.6);
}

.status-offline {
  background-color: var(--el-color-danger);
  box-shadow: 0 0 6px rgba(245, 108, 108, 0.6);
}

.status-unknown {
  background-color: var(--addp-text-tertiary);
  box-shadow: 0 0 6px rgba(144, 147, 153, 0.6);
}

.status-checking {
  background-color: var(--el-color-warning);
  box-shadow: 0 0 6px rgba(230, 162, 60, 0.6);
  animation: pulse 1.5s ease-in-out infinite;
}

@keyframes pulse {
  0%, 100% {
    opacity: 1;
  }
  50% {
    opacity: 0.5;
  }
}

/* 引擎类型选择对话框样式 */
.engine-type-selection {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
  padding: 20px 0;
}

.type-card {
  cursor: pointer;
  transition: all 0.3s;
  text-align: center;
  padding: 20px;
}

.type-card:hover {
  transform: translateY(-5px);
  border-color: var(--el-color-primary);
}

.type-icon {
  font-size: 48px;
  margin-bottom: 16px;
}

.type-card h3 {
  margin: 16px 0 8px;
  font-size: 18px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.type-card p {
  margin: 0 0 16px;
  font-size: 14px;
  color: var(--addp-text-secondary);
  line-height: 1.5;
}

.type-card ul {
  list-style: none;
  padding: 0;
  margin: 0;
  font-size: 13px;
  color: var(--addp-text-tertiary);
}

.type-card ul li {
  padding: 4px 0;
}
</style>
