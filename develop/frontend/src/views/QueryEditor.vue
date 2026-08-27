<template>
  <div class="query-workbench">
    <header class="workbench-toolbar">
      <div class="toolbar-primary">
        <el-button
          v-if="isCompact"
          circle
          :aria-label="t('develop.query.dataResources')"
          @click="catalogDrawerVisible = true"
        >
          <el-icon><Menu /></el-icon>
        </el-button>
        <h2>{{ currentTaskName || t('develop.query.title') }}</h2>
        <el-select
          ref="queryEngineSelectRef"
          :model-value="selectedQueryTarget"
          class="engine-select"
          :placeholder="t('develop.query.selectDataSource')"
          :disabled="executing || loadingSampleQuery || switchingQueryTarget || savingForEngineSwitch"
          @change="requestQueryTargetChange"
          @visible-change="handleQueryEngineDropdownVisible"
        >
          <el-option
            v-if="selectedEngineUnavailable"
            :label="t('develop.query.engineUnavailable', { id: selectedEngineId })"
            :value="selectedQueryTarget"
            disabled
          />
          <el-option
            v-for="target in queryTargets"
            :key="target.value"
            :label="target.label"
            :value="target.value"
            :disabled="!target.available"
          >
            <span>{{ target.name }}</span>
            <span class="engine-type">{{ target.typeLabel }} · {{ target.statusLabel }}</span>
          </el-option>
        </el-select>
        <el-tag v-if="currentQueryLanguage" size="small" effect="plain">
          {{ currentQueryLanguage.toUpperCase() }}
        </el-tag>
      </div>

      <div class="toolbar-actions">
        <el-tooltip :content="t('develop.query.testConnection')">
          <el-button
            circle
            :loading="testingConnection"
            :disabled="!selectedTarget || switchingQueryTarget || savingForEngineSwitch"
            :aria-label="t('develop.query.testConnection')"
            @click="handleTestConnection"
          >
            <el-icon><Connection /></el-icon>
          </el-button>
        </el-tooltip>
        <el-tooltip :content="t('develop.query.generateQueryTemplate')">
          <el-button
            circle
            :loading="loadingSampleQuery"
            :disabled="!selectedTarget || executing || switchingQueryTarget || savingForEngineSwitch"
            :aria-label="t('develop.query.generateQueryTemplate')"
            @click="generateQueryTemplate"
          >
            <el-icon><DocumentAdd /></el-icon>
          </el-button>
        </el-tooltip>
        <el-tooltip :content="t('develop.query.format')">
          <el-button
            circle
            :disabled="!formatterLanguage || !queryContent || executing || switchingQueryTarget || savingForEngineSwitch"
            :aria-label="t('develop.query.format')"
            @click="formatQuery"
          >
            <el-icon><Operation /></el-icon>
          </el-button>
        </el-tooltip>
        <el-button
          :disabled="!selectedTarget || !queryContent.trim() || !relationTaskValid || executing || switchingQueryTarget || savingForEngineSwitch"
          @click="handlePersistQueryTask"
        >
          <el-icon><FolderAdd /></el-icon>
          {{ currentTaskId ? t('develop.query.updateTask') : t('develop.query.saveAsTask') }}
        </el-button>
        <el-button
          type="primary"
          :loading="executing"
          :disabled="loadingSampleQuery || !selectedTarget || !queryContent.trim() || hasRelationInputs || switchingQueryTarget || savingForEngineSwitch"
          @click="executeQuery"
        >
          <el-icon><VideoPlay /></el-icon>
          {{ selectedText ? t('develop.query.executeSelection') : t('develop.query.execute') }}
        </el-button>
      </div>
    </header>

    <main class="workbench-body">
      <aside v-if="!isCompact" class="catalog-panel" :style="{ width: `${catalogWidth}px` }">
        <div class="catalog-heading">
          <span>{{ t('develop.query.dataResources') }}</span>
          <el-tooltip :content="t('develop.query.generateQueryTemplate')">
            <el-button
              circle
              size="small"
              :disabled="!selectedTarget || loadingSampleQuery || executing || switchingQueryTarget || savingForEngineSwitch"
              :aria-label="t('develop.query.generateQueryTemplate')"
              @click="generateQueryTemplate"
            >
              <el-icon><DocumentAdd /></el-icon>
            </el-button>
          </el-tooltip>
        </div>
        <el-empty v-if="catalogEnginesLoading" :description="t('develop.query.loadingFederatedCatalog')" :image-size="64" />
        <ResourceTreePicker
          v-else-if="catalogTreeEngineIds.length"
          v-model="catalogSelection"
          class="catalog-tree"
          :engine-id="catalogTreeEngineValue"
          :engine-multiple="catalogTreeEngineMultiple"
          :initial-locator="initialCatalogLocator"
          :show-engine-selector="false"
          :show-selection-summary="false"
          :show-count="false"
          :title="''"
          mode="any"
          :selectable-filter="isQueryCatalogSelectionAllowed"
          tree-height="100%"
          @node-click="rememberCatalogNode"
          @node-expand="rememberCatalogNode"
          @select="rememberCatalogSelection"
          @node-dblclick="insertCatalogItemAtCursor"
        />
        <el-empty v-else :description="catalogEmptyDescription" :image-size="64" />
      </aside>

      <div
        v-if="!isCompact"
        class="resize-handle vertical"
        role="separator"
        tabindex="0"
        aria-orientation="vertical"
        @mousedown="startCatalogResize"
        @keydown="handleCatalogResizeKeydown"
      />

      <section class="query-surface">
        <div class="editor-panel" :style="{ height: `${editorHeight}px` }">
          <div class="panel-heading">
            <span><el-icon><Edit /></el-icon>{{ t('develop.query.editorTitle') }}</span>
            <div class="editor-heading-actions">
              <span v-if="isDirty" class="dirty-indicator">{{ t('develop.query.unsaved') }}</span>
              <el-tag
                v-if="queryAnalysis"
                size="small"
                effect="plain"
                :type="queryAnalysis.allowed ? (queryAnalysis.risk_level === 'high' ? 'warning' : 'success') : 'danger'"
              >
                {{ t(`develop.query.effect.${queryAnalysis.effect}`) }}
              </el-tag>
              <el-button
                text
                size="small"
                :disabled="executing || loadingSampleQuery || switchingQueryTarget || savingForEngineSwitch"
                @click="triggerCompletion"
              >
                <el-icon><List /></el-icon>
                {{ t('develop.query.showCompletions') }}
              </el-button>
              <el-button
                text
                size="small"
                :type="hasUnresolvedParameters ? 'warning' : 'default'"
                :disabled="!queryParametersSupported || executing || switchingQueryTarget || savingForEngineSwitch"
                @click="parameterDrawerVisible = !parameterDrawerVisible"
              >
                <el-icon><Key /></el-icon>
                {{ t('develop.query.queryParameters') }}<span v-if="queryParameters.length"> ({{ queryParameters.length }})</span>
              </el-button>
            </div>
          </div>
          <div v-loading="loadingSampleQuery" class="editor-content" :aria-busy="loadingSampleQuery">
            <div class="relation-input-config">
              <div class="relation-input-heading">
                <span>{{ t('develop.query.relationInputs') }}</span>
                <span class="relation-input-hint">{{ t('develop.query.relationInputsHint') }}</span>
              </div>
              <el-select
                v-model="relationInputs"
                multiple
                filterable
                allow-create
                default-first-option
                :placeholder="t('develop.query.relationInputsPlaceholder')"
                :disabled="executing || switchingQueryTarget || savingForEngineSwitch"
                @change="normalizeRelationInputSelection"
              />
              <el-alert
                v-if="hasRelationInputs"
                :type="relationTaskValid ? 'info' : 'error'"
                :closable="false"
                :title="relationTaskValid ? t('develop.query.relationExecutionHint') : relationTaskError"
              />
            </div>
            <el-alert
              v-if="queryDiagnostics.length"
              class="query-diagnostic-alert"
              :type="hasBlockingDiagnostics ? 'error' : 'warning'"
              :title="t('develop.query.queryDiagnostics')"
              :closable="false"
              show-icon
            >
              <ul class="query-diagnostic-list">
                <li v-for="(diagnostic, index) in queryDiagnostics" :key="`${diagnostic.code}-${diagnostic.name || diagnostic.field || index}`">
                  <span>{{ queryDiagnosticMessage(diagnostic) }}</span>
                  <el-button
                    v-if="diagnostic.replacement && Number.isInteger(diagnostic.start) && Number.isInteger(diagnostic.end)"
                    link
                    type="primary"
                    size="small"
                    @click="applyQueryDiagnosticFix(diagnostic)"
                  >
                    {{ t('develop.query.applyDiagnosticFix') }}
                  </el-button>
                </li>
              </ul>
            </el-alert>
            <MonacoEditor
              ref="editorRef"
              v-model="queryContent"
              :language="monacoLanguage"
              :completions="completionSuggestions"
              theme="vs-dark"
              @execute="executeQuery"
              @selection-change="selectedText = $event"
            />
          </div>
        </div>

        <div
          class="resize-handle horizontal"
          role="separator"
          tabindex="0"
          aria-orientation="horizontal"
          @mousedown="startEditorResize"
          @keydown="handleEditorResizeKeydown"
        />

        <div class="result-panel">
          <div class="panel-heading">
            <span><el-icon><List /></el-icon>{{ t('develop.query.resultTitle') }}</span>
            <div class="result-actions">
              <el-radio-group v-if="hasGraphData" v-model="resultViewMode" size="small">
                <el-radio-button value="table">{{ t('develop.query.tableView') }}</el-radio-button>
                <el-radio-button value="graph">{{ t('develop.query.graphView') }}</el-radio-button>
              </el-radio-group>
              <el-tooltip v-if="executionResult && !executing" :content="t('develop.query.clearResult')">
                <el-button circle size="small" :aria-label="t('develop.query.clearResult')" @click="clearResult">
                  <el-icon><Close /></el-icon>
                </el-button>
              </el-tooltip>
            </div>
          </div>
          <div class="result-content">
            <QueryResult
              :result="executionResult"
              :custom-content="resultViewMode === 'graph' && hasGraphData"
            >
              <GraphResultView
                v-if="resultViewMode === 'graph' && hasGraphData"
                class="graph-result-view"
                :graph-data="executionResult.graph_data"
              />
            </QueryResult>
          </div>
        </div>
      </section>

      <component
        :is="isCompact ? 'el-drawer' : 'aside'"
        v-if="parameterDrawerVisible"
        v-model="parameterDrawerVisible"
        class="parameter-panel"
        :class="{ 'parameter-panel-dock': !isCompact }"
        :title="t('develop.query.queryParameters')"
        direction="rtl"
        size="min(92vw, 560px)"
        :modal="isCompact"
        :close-on-click-modal="false"
      >
        <div v-if="!isCompact" class="parameter-panel-heading">
          <span><el-icon><Key /></el-icon>{{ t('develop.query.queryParameters') }}</span>
          <el-button circle text size="small" :aria-label="t('develop.query.closeQueryParameters')" @click="parameterDrawerVisible = false">
            <el-icon><Close /></el-icon>
          </el-button>
        </div>
        <el-alert
          v-if="hasUnresolvedParameters || hasUnusedParameters"
          class="parameter-sync-alert"
          type="warning"
          :title="parameterSyncMessage"
          :closable="false"
          show-icon
        />
        <div class="parameter-toolbar">
          <el-button plain :disabled="!queryParametersSupported || !queryContent.trim()" @click="extractQueryParameters">
            <el-icon><MagicStick /></el-icon>
            {{ t('develop.query.extractQueryParameters') }}
          </el-button>
          <el-button plain :disabled="!queryParametersSupported || !selectedText" @click="parameterizeSelectedText">
            <el-icon><Edit /></el-icon>
            {{ t('develop.query.parameterizeSelection') }}
          </el-button>
          <el-button type="primary" plain :disabled="!queryParametersSupported" @click="addQueryParameter">
            <el-icon><Plus /></el-icon>
            {{ t('develop.query.addQueryParameter') }}
          </el-button>
        </div>
        <div class="parameter-list">
          <div v-for="(parameter, index) in queryParameters" :key="parameter.id" class="parameter-item">
            <div class="parameter-item-heading">
              <strong>{{ parameter.name || t('develop.query.unnamedParameter') }}</strong>
              <div class="parameter-item-actions">
                <el-tooltip :content="t('develop.query.insertParameterReference')">
                  <el-button
                    circle
                    size="small"
                    :disabled="!parameter.name"
                    :aria-label="t('develop.query.insertParameterReference')"
                    @click="insertQueryParameterReference(parameter)"
                  >
                    <el-icon><Position /></el-icon>
                  </el-button>
                </el-tooltip>
                <el-tooltip :content="t('develop.query.removeQueryParameter')">
                  <el-button
                    circle
                    size="small"
                    type="danger"
                    plain
                    :aria-label="t('develop.query.removeQueryParameter')"
                    @click="removeQueryParameter(index)"
                  >
                    <el-icon><Delete /></el-icon>
                  </el-button>
                </el-tooltip>
              </div>
            </div>
            <div class="parameter-grid">
              <el-form-item :label="t('develop.query.parameterName')" :error="queryParameterNameError(parameter, index)">
                <el-input v-model="parameter.name" maxlength="64" />
              </el-form-item>
              <el-form-item :label="t('develop.query.parameterType')">
                <el-select v-model="parameter.type" @change="resetQueryParameterDefault(parameter)">
                  <el-option
                    v-for="type in queryParameterTypes"
                    :key="type"
                    :label="t(`develop.query.parameterTypes.${type}`)"
                    :value="type"
                  />
                </el-select>
              </el-form-item>
              <el-form-item :label="t('develop.query.parameterDefault')" :error="queryParameterDefaultError(parameter)">
                <el-switch v-if="parameter.type === 'boolean'" v-model="parameter.default" />
                <el-input-number
                  v-else-if="parameter.type === 'integer' || parameter.type === 'number'"
                  v-model="parameter.default"
                  :precision="parameter.type === 'integer' ? 0 : undefined"
                  :step="parameter.type === 'integer' ? 1 : 0.1"
                  controls-position="right"
                />
                <el-input v-else v-model="parameter.default" />
              </el-form-item>
              <el-form-item :label="t('develop.query.parameterTitle')">
                <el-input v-model="parameter.title" maxlength="100" />
              </el-form-item>
              <el-form-item class="parameter-description" :label="t('develop.query.parameterDescription')">
                <el-input v-model="parameter.description" type="textarea" :rows="2" maxlength="300" />
              </el-form-item>
            </div>
          </div>
          <el-empty v-if="queryParameters.length === 0" :description="t('develop.query.noQueryParameters')" :image-size="56" />
        </div>
      </component>
    </main>

    <el-drawer
      v-if="isCompact"
      v-model="catalogDrawerVisible"
      :title="t('develop.query.dataResources')"
      direction="ltr"
      size="min(88vw, 380px)"
    >
      <div class="drawer-catalog-actions">
        <el-button
          type="primary"
          :disabled="!selectedTarget || loadingSampleQuery || executing || switchingQueryTarget || savingForEngineSwitch"
          @click="generateQueryTemplate"
        >
          <el-icon><DocumentAdd /></el-icon>
          {{ t('develop.query.generateQueryTemplate') }}
        </el-button>
      </div>
      <el-empty v-if="catalogEnginesLoading" :description="t('develop.query.loadingFederatedCatalog')" :image-size="64" />
      <ResourceTreePicker
        v-else-if="catalogTreeEngineIds.length"
        v-model="catalogSelection"
        :engine-id="catalogTreeEngineValue"
        :engine-multiple="catalogTreeEngineMultiple"
        :initial-locator="initialCatalogLocator"
        :show-engine-selector="false"
        :show-selection-summary="false"
        :show-count="false"
        :title="''"
        mode="any"
        :selectable-filter="isQueryCatalogSelectionAllowed"
        tree-height="calc(100vh - 150px)"
        @node-click="rememberCatalogNode"
        @node-expand="rememberCatalogNode"
        @select="rememberCatalogSelection"
        @node-dblclick="insertCatalogItemAtCursor"
      />
      <el-empty v-else :description="catalogEmptyDescription" :image-size="64" />
    </el-drawer>

    <el-dialog
      v-model="executionParameterDialogVisible"
      class="addp-dialog"
      :title="t('develop.query.executionParameters')"
      width="min(680px, calc(100vw - 24px))"
      :close-on-click-modal="false"
    >
      <ExecutionParameterForm
        ref="executionParameterFormRef"
        v-model="executionParameterOverrides"
        :contract="queryExecutionContract"
        :disabled="executing"
      />
      <template #footer>
        <el-button :disabled="executing" @click="executionParameterDialogVisible = false">{{ t('develop.query.cancel') }}</el-button>
        <el-button type="primary" :loading="executing" @click="submitQuery(executionParameterOverrides)">
          <el-icon><VideoPlay /></el-icon>
          {{ t('develop.query.execute') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="queryClarificationVisible"
      class="addp-dialog"
      :title="t('develop.query.clarificationTitle')"
      width="min(760px, calc(100vw - 24px))"
      :close-on-click-modal="!generatingQuery"
      :close-on-press-escape="!generatingQuery"
      :show-close="!generatingQuery"
    >
      <div class="query-resource-context">
        <el-tag size="small" effect="plain">{{ selectedTarget?.name }}</el-tag>
        <el-tag size="small" effect="plain">{{ currentQueryLanguage.toUpperCase() }}</el-tag>
      </div>
      <div class="query-resource-candidate-list">
        <section
          v-for="clarification in querySemanticClarifications"
          :key="clarification.key"
          class="query-resource-candidate-group"
        >
          <h3 v-if="clarification.control !== 'notice'">{{ clarification.prompt }}</h3>
          <el-radio-group
            v-if="clarification.control === 'single_choice'"
            v-model="queryClarificationAnswers[clarification.key]"
            class="query-resource-candidate-options"
          >
            <el-radio
              v-for="option in clarification.options"
              :key="option.value"
              :value="option.value"
              :disabled="generatingQuery"
              class="query-resource-candidate"
            >
              <span class="query-resource-candidate-content">
                <span class="query-resource-candidate-heading">{{ option.label }}</span>
                <span v-if="option.description" class="query-resource-candidate-facts">
                  {{ option.description }}
                </span>
              </span>
            </el-radio>
          </el-radio-group>
          <el-checkbox-group
            v-else-if="clarification.control === 'multiple_choice'"
            v-model="queryClarificationAnswers[clarification.key]"
            class="query-resource-candidate-options"
          >
            <el-checkbox
              v-for="option in clarification.options"
              :key="option.value"
              :value="option.value"
              :disabled="generatingQuery"
              class="query-resource-candidate"
            >
              <span class="query-resource-candidate-content">
                <span class="query-resource-candidate-heading">{{ option.label }}</span>
                <span v-if="option.description" class="query-resource-candidate-facts">
                  {{ option.description }}
                </span>
              </span>
            </el-checkbox>
          </el-checkbox-group>
          <el-input
            v-else-if="clarification.control === 'text'"
            v-model="queryClarificationAnswers[clarification.key]"
            type="textarea"
            :rows="3"
            :disabled="generatingQuery"
            :placeholder="t('develop.query.clarificationAnswerPlaceholder')"
          />
          <el-alert
            v-else-if="clarification.control === 'notice'"
            :title="clarification.prompt"
            type="warning"
            :closable="false"
            show-icon
          />
        </section>
        <section
          v-for="group in queryResourceCandidateGroups"
          :key="group.role"
          class="query-resource-candidate-group"
        >
          <h3>{{ group.role }}</h3>
          <el-radio-group
            v-model="selectedQueryResourceCandidatesByRole[group.role]"
            class="query-resource-candidate-options"
          >
            <el-radio
              v-for="candidate in group.candidates"
              :key="resourceCandidateKey(candidate)"
              :value="resourceCandidateKey(candidate)"
              :disabled="generatingQuery"
              class="query-resource-candidate"
            >
              <span class="query-resource-candidate-content">
                <span class="query-resource-candidate-heading">
                  <span>{{ queryResourceCandidateName(candidate) }}</span>
                  <el-tag v-if="candidate.recommended" size="small" type="success" effect="plain">
                    {{ t('develop.query.recommendedResource') }}
                  </el-tag>
                </span>
                <span v-if="queryResourceCandidateFacts(candidate)" class="query-resource-candidate-facts">
                  {{ queryResourceCandidateFacts(candidate) }}
                </span>
                <span
                  v-if="candidate.recommended && candidate.recommendation_reason"
                  class="query-resource-candidate-reason"
                >
                  {{ candidate.recommendation_reason }}
                </span>
              </span>
            </el-radio>
          </el-radio-group>
        </section>
      </div>
      <template #footer>
        <el-button :disabled="generatingQuery" @click="queryClarificationVisible = false">
          {{ t('develop.query.cancel') }}
        </el-button>
        <el-button
          v-if="queryClarificationCanContinue"
          type="primary"
          :loading="generatingQuery"
          :disabled="generatingQuery || !queryClarificationComplete"
          @click="confirmQueryClarifications"
        >
          {{ t('develop.query.confirmAndGenerate') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="queryEngineSwitchDialogVisible"
      class="addp-dialog"
      :title="t('develop.query.engineSwitchTitle')"
      width="min(520px, calc(100vw - 24px))"
      :close-on-click-modal="false"
      :close-on-press-escape="!switchingQueryTarget && !savingForEngineSwitch"
      :show-close="!switchingQueryTarget && !savingForEngineSwitch"
    >
      <el-alert
        :title="t('develop.query.engineSwitchMessage', { name: pendingQueryTargetInfo?.name || '-' })"
        type="warning"
        :closable="false"
        show-icon
      />
      <template #footer>
        <el-button :disabled="switchingQueryTarget || savingForEngineSwitch" @click="cancelQueryTargetChange">
          {{ t('develop.query.cancel') }}
        </el-button>
        <el-button type="danger" plain :loading="switchingQueryTarget" :disabled="savingForEngineSwitch" @click="clearAndSwitchQueryTarget">
          {{ t('develop.query.clearAndSwitch') }}
        </el-button>
        <el-button type="primary" :loading="savingForEngineSwitch" :disabled="switchingQueryTarget" @click="saveAndSwitchQueryTarget">
          {{ t('develop.query.saveAndClear') }}
        </el-button>
      </template>
    </el-dialog>

    <SaveQueryDialog
      v-model="showSaveDialog"
      :engine-id="selectedEngineId"
      :sql="queryContent"
      @update:model-value="handleSaveDialogVisibility"
      @saved="handleSaveTask"
    />
    <div class="query-ai-fab-wrapper">
      <transition name="query-ai-slide">
        <div v-if="queryAiOpen" class="query-ai-panel">
          <div class="query-ai-panel-header">
            <span>{{ t('develop.query.aiTitle') }}</span>
            <el-button
              circle
              text
              size="small"
              :aria-label="t('develop.query.closeAiAssistant')"
              :disabled="queryCopilotBusy"
              @click="queryAiOpen = false"
            >
              <el-icon><Close /></el-icon>
            </el-button>
          </div>
          <div class="query-ai-context">
            <el-tag size="small" effect="plain">{{ selectedTarget?.name || t('develop.query.noEngineSelected') }}</el-tag>
            <el-tag v-if="currentQueryLanguage" size="small" effect="plain">{{ currentQueryLanguage.toUpperCase() }}</el-tag>
          </div>
          <el-input
            ref="queryAiInputRef"
            v-model="queryAiPrompt"
            type="textarea"
            :rows="4"
            :placeholder="t('develop.query.aiPlaceholder')"
            :disabled="queryCopilotBusy"
            class="query-ai-input"
            @keydown.ctrl.enter="generateQueryWithCopilot"
            @keydown.meta.enter="generateQueryWithCopilot"
          />
          <div v-if="selectedQueryContext" class="query-ai-selected-resource">
            <span>{{ t('develop.query.selectedQueryContext') }}</span>
            <strong>{{ selectedQueryContext }}</strong>
          </div>
          <div class="query-ai-panel-footer">
            <el-button
              type="primary"
              size="small"
              :loading="generatingQuery"
              :disabled="queryCopilotBusy || !selectedTarget"
              @click="generateQueryWithCopilot"
            >
              {{ t('develop.query.generateQuery') }}
            </el-button>
          </div>
        </div>
      </transition>
      <el-tooltip :content="t('develop.query.aiTitle')">
        <el-button
          class="query-ai-fab"
          circle
          type="primary"
          :aria-label="t('develop.query.aiTitle')"
          :disabled="queryCopilotBusy"
          @click="toggleQueryAiPanel"
        >
          <el-icon><MagicStick /></el-icon>
        </el-button>
      </el-tooltip>
    </div>
    <StatusAnnouncer :message="announcement" />
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Close,
  Connection,
  DocumentAdd,
  Delete,
  Edit,
  FolderAdd,
  Key,
  List,
  MagicStick,
  Menu,
  Operation,
  Plus,
  Position,
  VideoPlay
} from '@element-plus/icons-vue'
import { format } from 'sql-formatter'
import {
  ResourceTreePicker,
  ExecutionParameterForm,
  StatusAnnouncer,
  getResourceFields,
  getResourceItemByCatalogPath,
  getResourceTreeNode,
  getResourceTreeAncestors,
  formatLocatorDisplayPath,
  engineSelectionState,
  isEngineSelectable,
  listResourceTreeEngines,
  parseLocator,
  useResizable,
  useConsolePageDescriptor
} from '@common-ui'
import { GraphResultView } from '@addp/common-frontend/graph'
import MonacoEditor from '../components/MonacoEditor.vue'
import QueryResult from '../components/QueryResult.vue'
import SaveQueryDialog from '../components/SaveQueryDialog.vue'
import { getSampleQuery, preflightQuery, saveQueryTask, testConnection, updateQueryTask } from '../api/query.js'
import { createExecution, getExecution } from '../api/execution.js'
import { listEngines } from '../api/engines.js'
import { getDevTask } from '../api/devTask.js'
import { generateQueryFromNL } from '../api/copilot.js'
import {
  buildDevelopTaskEditorLocation,
  developTaskIDFromRoute
} from '@/utils/developTaskRoute'
import { navigateDevelopTaskEditor } from '@/utils/developNavigation'
import { createLatestRequestCoordinator } from '@common-ui'
import {
  formatterLanguageForQuery,
  formatGeneratedQueryForEditor,
  formatMQLQuery,
  buildQueryExecutionContract,
  isTerminalExecutionStatus,
  monacoLanguageForQuery,
  queryParameterReference,
  queryCapabilityForEngine,
  queryResultFromExecution,
  extractQueryParameterReferences,
  parameterizeSelection,
  diagnoseQuery,
  isQueryInputResource,
  mqlCollectionReferences,
  matchMQLCollectionReferences,
  parseSQLSources
} from '@/utils/queryWorkbench.mjs'
import { resolveQueryGenerationResult } from '@/utils/queryGenerationResult.mjs'
import {
  normalizeRelationInputs,
  relationInputsValid as validateRelationInputs
} from '@/utils/relationInputContract.mjs'
import {
  confirmedResources,
  defaultResourceCandidatesByRole,
  groupResourceCandidates,
  hasSelectedResourceForEveryRole,
  resourceCandidateKey,
  resourceFact
} from '@addp/common-frontend'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const currentTaskId = ref(null)
const currentTaskName = ref('')
useConsolePageDescriptor(router, 'develop', {
  title: computed(() => t('develop.query.title')),
  subject: currentTaskName,
  ready: computed(() => Boolean(currentTaskId.value && currentTaskName.value))
})
const currentTask = ref(null)
const selectedQueryTarget = ref('')
const engines = ref([])
const queryContent = ref('')
const currentQueryLanguage = ref('')
const executionResult = ref(null)
const executing = ref(false)
const testingConnection = ref(false)
const loadingSampleQuery = ref(false)
const editorRef = ref(null)
const selectedText = ref('')
const showSaveDialog = ref(false)
const queryEngineSelectRef = ref(null)
const queryEngineSwitchDialogVisible = ref(false)
const pendingQueryTarget = ref('')
const switchingQueryTarget = ref(false)
const savingForEngineSwitch = ref(false)
const saveForEngineSwitch = ref(false)
const resultViewMode = ref('table')
const catalogSelection = ref(null)
const catalogSourceEngines = ref([])
const catalogEngineIds = ref([])
const catalogEnginesLoading = ref(false)
const targetLocator = ref('')
const initialCatalogLocator = ref('')
const catalogCompletions = ref([])
const fieldCompletions = ref([])
const fieldSourceContexts = ref([])
const queryAnalysis = ref(null)
const catalogDrawerVisible = ref(false)
const parameterDrawerVisible = ref(false)
const queryParameters = ref([])
const relationInputs = ref([])
const executionParameterDialogVisible = ref(false)
const executionParameterOverrides = ref({})
const executionParameterFormRef = ref(null)
const isCompact = ref(false)
const announcement = ref('')
const queryAiOpen = ref(false)
const queryAiPrompt = ref('')
const queryAiInputRef = ref(null)
const generatingQuery = ref(false)
const queryClarificationVisible = ref(false)
const queryClarifications = ref([])
const queryClarificationAnswers = ref({})
const queryAcceptedClarificationAnswers = ref({})
const queryClarificationResources = ref([])
const selectedQueryResourceCandidatesByRole = ref({})
const savedSnapshot = ref('')
const queryTaskRouteReady = ref(false)
const bypassUnsavedRouteConfirm = ref(false)
const sampleRequests = createLatestRequestCoordinator()
const fieldCompletionCache = new Map()
let executionRequestSequence = 0
let fieldRequestSequence = 0
let fieldSourcesRequestSequence = 0
let fieldSourcesDebounce = null
let catalogRequestSequence = 0
let catalogEngineRequestSequence = 0
let mediaQuery = null
let compactMediaListener = null
let applyingQueryTaskRoute = false

const {
  size: catalogWidth,
  startResize: startCatalogResize,
  handleResizeKeydown: handleCatalogResizeKeydown
} = useResizable(300, 240, 480, 'horizontal')
const {
  size: editorHeight,
  startResize: startEditorResize,
  handleResizeKeydown: handleEditorResizeKeydown
} = useResizable(390, 220, 720, 'vertical')

const queryTargets = computed(() => engines.value.map(engine => {
  const available = isEngineSelectable(engine)
  const statusLabel = t(`common.engineStatus.${engineSelectionState(engine)}`)
  return {
    value: `engine:${engine.id}`,
    name: engine.name,
    label: `${engine.name} (${engine.engine_type}) · ${statusLabel}`,
    typeLabel: engine.engine_type,
    statusLabel,
    available,
    engine
  }
}))

const completionSuggestions = computed(() => [
  ...fieldCompletions.value,
  ...catalogCompletions.value.filter(item => !fieldCompletions.value.some(field => field.insertText === item.insertText))
])
const selectedRegisteredTarget = computed(() => (
  queryTargets.value.find(target => target.value === selectedQueryTarget.value) || null
))
const selectedTarget = computed(() => (
  selectedRegisteredTarget.value?.available ? selectedRegisteredTarget.value : null
))
const pendingQueryTargetInfo = computed(() => (
  queryTargets.value.find(target => target.value === pendingQueryTarget.value) || null
))
const selectedEngineId = computed(() => {
  if (selectedTarget.value) return selectedTarget.value.engine.id
  const match = String(selectedQueryTarget.value).match(/^engine:(\d+)$/)
  return match ? Number(match[1]) : null
})
const selectedEngineUnavailable = computed(() => Boolean(selectedQueryTarget.value && !selectedRegisteredTarget.value))
const queryCopilotBusy = computed(() => (
  generatingQuery.value
  || executing.value
  || loadingSampleQuery.value
  || switchingQueryTarget.value
  || savingForEngineSwitch.value
))
const queryResourceCandidates = computed(() => queryClarifications.value.flatMap(clarification => (
  clarification.control === 'resource_choice' ? clarification.resourceCandidates : []
)))
const querySemanticClarifications = computed(() => queryClarifications.value.filter(
  clarification => clarification.control !== 'resource_choice'
))
const queryResourceCandidateGroups = computed(() => groupResourceCandidates(queryResourceCandidates.value))
const queryResourceSelectionComplete = computed(() => hasSelectedResourceForEveryRole(
  queryResourceCandidates.value,
  selectedQueryResourceCandidatesByRole.value
))
const queryClarificationComplete = computed(() => queryClarifications.value.every(clarification => {
  if (!clarification.required || clarification.control === 'notice') return true
  if (clarification.control === 'resource_choice') return queryResourceSelectionComplete.value
  const answer = queryClarificationAnswers.value[clarification.key]
  if (clarification.control === 'multiple_choice') return Array.isArray(answer) && answer.length > 0
  return typeof answer === 'string' && answer.trim().length > 0
}))
const queryClarificationCanContinue = computed(() => queryClarifications.value.some(
  clarification => clarification.control !== 'notice'
))
const selectedQueryContext = computed(() => {
  const selection = catalogSelection.value
  const locator = selection?.identity?.locator || targetLocator.value
  if (!locator) return ''
  try {
    const parsed = parseLocator(locator)
    return selection?.display?.path || parsed.path?.join('.') || locator
  } catch {
    return ''
  }
})
const selectedCapability = computed(() => queryCapabilityForEngine(selectedTarget.value?.engine))
const federatedQuery = computed(() => Boolean(selectedCapability.value.federation?.supported))
const catalogTreeEngineIds = computed(() => federatedQuery.value
  ? catalogEngineIds.value
  : (selectedEngineId.value ? [selectedEngineId.value] : []))
const catalogTreeEngineValue = computed(() => federatedQuery.value
  ? catalogTreeEngineIds.value
  : (selectedEngineId.value || null))
const catalogTreeEngineMultiple = computed(() => federatedQuery.value)
const catalogEmptyDescription = computed(() => {
  if (!selectedEngineId.value) return t('develop.query.selectDataSourceFirst')
  if (federatedQuery.value) return t('develop.query.noFederatedSourceEngines')
  return t('develop.query.selectDataSourceFirst')
})
const queryParametersSupported = computed(() => Boolean(
  selectedCapability.value.parameters?.supported &&
  selectedCapability.value.parameters.languages.includes(currentQueryLanguage.value)
))
const queryParameterTypes = computed(() => selectedCapability.value.parameters?.types || [])
const queryExecutionContract = computed(() => buildQueryExecutionContract(queryParameters.value))
const referencedParameterNames = computed(() => extractQueryParameterReferences(currentQueryLanguage.value, queryContent.value))
const definedParameterNames = computed(() => queryParameters.value.map(parameter => String(parameter?.name || '').trim()).filter(Boolean))
const hasUnresolvedParameters = computed(() => referencedParameterNames.value.some(name => !definedParameterNames.value.includes(name)))
const hasUnusedParameters = computed(() => definedParameterNames.value.some(name => !referencedParameterNames.value.includes(name)))
const hasRelationInputs = computed(() => relationInputs.value.length > 0)
const relationInputsValid = computed(() => validateRelationInputs(relationInputs.value))
const relationTaskValid = computed(() => !hasRelationInputs.value || (
  relationInputsValid.value &&
  currentQueryLanguage.value === 'sql' &&
  String(selectedTarget.value?.engine?.engine_type || '').toLowerCase().includes('postgres')
))
const relationTaskError = computed(() => {
  if (!relationInputsValid.value) return t('develop.query.relationInputsInvalid')
  if (currentQueryLanguage.value !== 'sql') return t('develop.query.relationSqlRequired')
  return t('develop.query.relationPostgresRequired')
})
const normalizeRelationInputSelection = () => {
  relationInputs.value = normalizeRelationInputs(relationInputs.value)
}
const parameterSyncMessage = computed(() => {
  if (hasUnresolvedParameters.value && hasUnusedParameters.value) return t('develop.query.parameterSyncBoth')
  if (hasUnresolvedParameters.value) return t('develop.query.parameterSyncMissing')
  return t('develop.query.parameterSyncUnused')
})
const queryDiagnostics = computed(() => {
  if (!queryContent.value.trim()) return []
  return diagnoseQuery({
    language: currentQueryLanguage.value,
    engineType: selectedTarget.value?.engine?.engine_type,
    query: queryContent.value,
    fields: fieldCompletions.value,
    fieldSources: currentQueryLanguage.value === 'sql' ? fieldSourceContexts.value : null,
    targetLocator: catalogSelection.value?.identity?.locator || targetLocator.value,
    referencedParameters: referencedParameterNames.value,
    definedParameters: definedParameterNames.value
  })
})
const hasBlockingDiagnostics = computed(() => queryDiagnostics.value.some(item => item.severity === 'error'))
const queryDiagnosticMessage = diagnostic => {
  const key = {
    query_empty: 'develop.query.diagnosticQueryEmpty',
    target_missing: 'develop.query.diagnosticTargetMissing',
    parameter_undefined: 'develop.query.diagnosticParameterUndefined',
    field_unknown: 'develop.query.diagnosticFieldUnknown',
    field_case_mismatch: 'develop.query.diagnosticFieldCaseMismatch',
    field_requires_quote: 'develop.query.diagnosticFieldRequiresQuote'
  }[diagnostic.code]
  return key ? t(key, diagnostic) : diagnostic.code
}
const triggerCompletion = () => editorRef.value?.triggerSuggest()
const applyQueryDiagnosticFix = diagnostic => {
  editorRef.value?.replaceOffsetRange(diagnostic.start, diagnostic.end, diagnostic.replacement)
}
const monacoLanguage = computed(() => monacoLanguageForQuery(currentQueryLanguage.value))
const formatterLanguage = computed(() => formatterLanguageForQuery(currentQueryLanguage.value))
const hasGraphData = computed(() => {
  const graph = executionResult.value?.graph_data
  return Boolean(graph?.nodes?.length || graph?.relationships?.length)
})
const currentSnapshot = computed(() => JSON.stringify({
  engine_id: selectedEngineId.value,
  language: currentQueryLanguage.value,
  query: queryContent.value,
  target_locator: catalogSelection.value?.identity?.locator || targetLocator.value || '',
  query_parameters: queryParameters.value.map(queryParameterPayload),
  relation_inputs: relationInputs.value
}))
const isDirty = computed(() => queryTaskRouteReady.value && savedSnapshot.value !== currentSnapshot.value)

const markSaved = () => {
  savedSnapshot.value = currentSnapshot.value
}

const loadEngines = async () => {
  try {
    const response = await listEngines()
    engines.value = Array.isArray(response) ? response : []
    if (!selectedQueryTarget.value) {
      selectedQueryTarget.value = queryTargets.value.find(target => target.available)?.value || ''
    }
  } catch (error) {
    engines.value = []
    ElMessage.error(t('develop.query.loadEnginesFailed') + (error.response?.data?.error || error.message))
  }
}

const handleQueryEngineDropdownVisible = visible => {
  if (visible) loadEngines()
}

const refreshCatalogEngines = async () => {
  const requestSequence = ++catalogEngineRequestSequence
  const engineID = selectedEngineId.value
  if (!engineID) {
    catalogSourceEngines.value = []
    catalogEngineIds.value = []
    catalogEnginesLoading.value = false
    return
  }
  if (!federatedQuery.value) {
    catalogSourceEngines.value = []
    catalogEngineIds.value = [engineID]
    catalogEnginesLoading.value = false
    return
  }
  catalogSourceEngines.value = []
  catalogEngineIds.value = []
  catalogEnginesLoading.value = true
  try {
    const capability = selectedCapability.value.federation
    const supportedTypes = new Set(capability.sourceEngineTypes)
    const sources = await listResourceTreeEngines('/api/v1/meta')
    if (requestSequence !== catalogEngineRequestSequence || engineID !== selectedEngineId.value) return
    catalogSourceEngines.value = sources
      .filter(source => Number(source.id) !== engineID && supportedTypes.has(String(source.engine_type || '').toLowerCase()))
    catalogEngineIds.value = catalogSourceEngines.value.map(source => Number(source.id)).filter(id => Number.isFinite(id) && id > 0)
  } catch (error) {
    if (requestSequence === catalogEngineRequestSequence) {
      catalogSourceEngines.value = []
      catalogEngineIds.value = []
      ElMessage.error(t('develop.query.loadFederatedCatalogFailed') + (error.response?.data?.error || error.message))
    }
  } finally {
    if (requestSequence === catalogEngineRequestSequence) catalogEnginesLoading.value = false
  }
}

const handleTestConnection = async () => {
  if (!selectedTarget.value) return
  testingConnection.value = true
  try {
    await testConnection(selectedEngineId.value)
    ElMessage.success(t('develop.query.testConnectionSuccess'))
  } catch (error) {
    ElMessage.error(t('develop.query.testConnectionFailed') + (error.response?.data?.error || error.message))
  } finally {
    testingConnection.value = false
  }
}

const loadSampleQuery = async ({ replace = false, locator = '' } = {}) => {
  if (!selectedTarget.value || loadingSampleQuery.value) return
  if ((queryContent.value.trim() || queryParameters.value.length > 0) && !replace) {
    try {
      await ElMessageBox.confirm(
        t('develop.query.replaceSampleConfirm'),
        t('develop.query.replaceSampleTitle'),
        {
          confirmButtonText: t('develop.query.replace'),
          cancelButtonText: t('develop.query.cancel'),
          type: 'warning',
          customClass: 'addp-message-box'
        }
      )
    } catch {
      return
    }
  }
  const request = sampleRequests.begin(`${selectedQueryTarget.value}:${locator}`)
  loadingSampleQuery.value = true
  try {
    const sample = await getSampleQuery(selectedEngineId.value, locator)
    if (!sampleRequests.isCurrent(request, `${selectedQueryTarget.value}:${locator}`)) return
    queryContent.value = sample.query
    queryParameters.value = []
    executionParameterOverrides.value = {}
    if (locator) targetLocator.value = locator
    currentQueryLanguage.value = String(sample.language || selectedCapability.value.defaultLanguage).toLowerCase()
    clearResult()
    announcement.value = t(locator ? 'develop.query.queryTemplateGenerated' : 'develop.query.sampleLoaded')
  } catch (error) {
    if (sampleRequests.isCurrent(request, `${selectedQueryTarget.value}:${locator}`)) {
      ElMessage.error(error.response?.data?.error || error.message)
    }
  } finally {
    if (sampleRequests.isCurrent(request, `${selectedQueryTarget.value}:${locator}`)) {
      loadingSampleQuery.value = false
    }
  }
}

async function requestQueryTargetChange(targetValue) {
  queryEngineSelectRef.value?.blur()
  if (executing.value || loadingSampleQuery.value || switchingQueryTarget.value || savingForEngineSwitch.value) return
  const target = queryTargets.value.find(item => item.value === targetValue)
  if (!target || targetValue === selectedQueryTarget.value) return
  if ((!queryContent.value.trim() && queryParameters.value.length === 0) || !isDirty.value) {
    await applyQueryTargetSwitch(targetValue)
    return
  }
  pendingQueryTarget.value = targetValue
  queryEngineSwitchDialogVisible.value = true
}

function cancelQueryTargetChange() {
  if (switchingQueryTarget.value || savingForEngineSwitch.value) return
  queryEngineSwitchDialogVisible.value = false
  pendingQueryTarget.value = ''
}

async function clearAndSwitchQueryTarget() {
  if (switchingQueryTarget.value || savingForEngineSwitch.value || !pendingQueryTarget.value) return
  await applyQueryTargetSwitch(pendingQueryTarget.value)
}

async function saveAndSwitchQueryTarget() {
  if (switchingQueryTarget.value || savingForEngineSwitch.value || !pendingQueryTarget.value) return
  if (!currentTaskId.value) {
    saveForEngineSwitch.value = true
    queryEngineSwitchDialogVisible.value = false
    showSaveDialog.value = true
    return
  }
  savingForEngineSwitch.value = true
  try {
    if (await persistCurrentQueryTask()) {
      await applyQueryTargetSwitch(pendingQueryTarget.value, { saved: true })
    }
  } finally {
    savingForEngineSwitch.value = false
  }
}

async function applyQueryTargetSwitch(targetValue, { saved = false } = {}) {
  const target = queryTargets.value.find(item => item.value === targetValue)
  if (!target) return
  switchingQueryTarget.value = true
  queryEngineSwitchDialogVisible.value = false
  pendingQueryTarget.value = ''
  executionRequestSequence += 1
  sampleRequests.invalidate()
  selectedQueryTarget.value = targetValue
  catalogSelection.value = null
  fieldRequestSequence += 1
  fieldSourcesRequestSequence += 1
  fieldCompletions.value = []
  fieldSourceContexts.value = []
  targetLocator.value = ''
  initialCatalogLocator.value = ''
  queryContent.value = ''
  queryParameters.value = []
  executionParameterOverrides.value = {}
  parameterDrawerVisible.value = false
  queryAiOpen.value = false
  queryClarificationVisible.value = false
  queryClarifications.value = []
  queryClarificationAnswers.value = {}
  queryAcceptedClarificationAnswers.value = {}
  queryClarificationResources.value = []
  selectedQueryResourceCandidatesByRole.value = {}
  currentQueryLanguage.value = queryCapabilityForEngine(target.engine).defaultLanguage
  clearResult()
  currentTaskId.value = null
  currentTaskName.value = ''
  currentTask.value = null
  try {
    applyingQueryTaskRoute = true
    bypassUnsavedRouteConfirm.value = true
    try {
      await navigateDevelopTaskEditor(router, 'query', '', { history: 'replace' })
    } finally {
      bypassUnsavedRouteConfirm.value = false
    }
    markSaved()
    ElMessage.success(t(
      saved ? 'develop.query.saveAndSwitchSuccess' : 'develop.query.engineSwitchSuccess',
      { name: target.name }
    ))
  } finally {
    applyingQueryTaskRoute = false
    switchingQueryTarget.value = false
  }
}

const queryParameterPayload = parameter => ({
  name: String(parameter?.name || '').trim(),
  type: parameter?.type,
  default: parameter?.default,
  ...(String(parameter?.title || '').trim() ? { title: String(parameter.title).trim() } : {}),
  ...(String(parameter?.description || '').trim() ? { description: String(parameter.description).trim() } : {})
})

const queryParameterEditorItem = (parameter, id) => ({
  id,
  name: parameter?.name || '',
  type: parameter?.type || 'string',
  default: parameter?.default,
  title: parameter?.title || '',
  description: parameter?.description || ''
})

const queryParameterNameError = (parameter, index) => {
  const name = String(parameter?.name || '').trim()
  if (!name) return t('develop.query.parameterNameRequired')
  if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(name)) return t('develop.query.parameterNameInvalid')
  if (queryParameters.value.some((item, itemIndex) => itemIndex !== index && String(item.name || '').trim() === name)) {
    return t('develop.query.parameterNameDuplicate')
  }
  return ''
}

const queryParameterDefaultError = parameter => {
  if (parameter?.type === 'string' && !String(parameter?.default ?? '').trim()) {
    return t('develop.query.parameterDefaultRequired')
  }
  return ''
}

const hasValidQueryParameters = () => queryParameters.value.every((parameter, index) => (
  !queryParameterNameError(parameter, index) && !queryParameterDefaultError(parameter)
))

const defaultValueForQueryParameterType = type => {
  if (type === 'boolean') return false
  if (type === 'integer' || type === 'number') return 0
  return ''
}

const addQueryParameter = () => {
  const type = queryParameterTypes.value[0] || 'string'
  queryParameters.value.push({
    id: `${Date.now()}-${queryParameters.value.length}`,
    name: '',
    type,
    default: defaultValueForQueryParameterType(type),
    title: '',
    description: ''
  })
}

const nextQueryParameterName = (base = 'parameter') => {
  const normalizedBase = String(base || 'parameter').replace(/[^A-Za-z0-9_]/g, '_').replace(/^[^A-Za-z_]+/, '') || 'parameter'
  const used = new Set(definedParameterNames.value)
  if (!used.has(normalizedBase)) return normalizedBase
  let index = 2
  while (used.has(`${normalizedBase}_${index}`)) index += 1
  return `${normalizedBase}_${index}`
}

const parameterizeSelectedText = async () => {
  if (!queryParametersSupported.value || !selectedText.value) return
  const suggested = nextQueryParameterName('value')
  const parsed = parameterizeSelection(currentQueryLanguage.value, selectedText.value, suggested)
  if (!parsed) {
    ElMessage.warning(t('develop.query.parameterizeSelectionInvalid'))
    return
  }
  try {
    const { value: requestedName } = await ElMessageBox.prompt(
      t('develop.query.parameterNamePrompt'),
      t('develop.query.parameterizeSelectionTitle'),
      {
        inputValue: suggested,
        confirmButtonText: t('develop.query.confirm'),
        cancelButtonText: t('develop.query.cancel'),
        inputPattern: /^[A-Za-z_][A-Za-z0-9_]*$/,
        inputErrorMessage: t('develop.query.parameterNameInvalid'),
        customClass: 'addp-message-box'
      }
    )
    const name = String(requestedName || '').trim()
    if (!name || definedParameterNames.value.includes(name)) {
      ElMessage.warning(t('develop.query.parameterNameDuplicate'))
      return
    }
    const parameter = parameterizeSelection(currentQueryLanguage.value, selectedText.value, name)
    if (!parameter) return
    editorRef.value?.insertText(parameter.reference)
    queryParameters.value.push({
      id: `${Date.now()}-${queryParameters.value.length}`,
      name,
      type: parameter.type,
      default: parameter.default,
      title: '',
      description: ''
    })
    parameterDrawerVisible.value = true
    ElMessage.success(t('develop.query.parameterizeSelectionSuccess'))
  } catch {
    // 用户取消参数命名时保持编辑器原内容不变。
  }
}

const extractQueryParameters = () => {
  const references = referencedParameterNames.value
  if (references.length === 0) {
    ElMessage.info(t('develop.query.noParameterReferences'))
    return
  }
  const existing = new Map(queryParameters.value.map(parameter => [String(parameter?.name || '').trim(), parameter]))
  references.forEach(name => {
    if (existing.has(name)) return
    const type = queryParameterTypes.value.includes('string') ? 'string' : (queryParameterTypes.value[0] || 'string')
    const parameter = {
      id: `${Date.now()}-${queryParameters.value.length}`,
      name,
      type,
      default: defaultValueForQueryParameterType(type),
      title: '',
      description: ''
    }
    queryParameters.value.push(parameter)
    existing.set(name, parameter)
  })
  parameterDrawerVisible.value = true
  ElMessage.success(t('develop.query.extractQueryParametersSuccess', { count: references.length }))
}

const removeQueryParameter = index => {
  queryParameters.value.splice(index, 1)
}

const resetQueryParameterDefault = parameter => {
  parameter.default = defaultValueForQueryParameterType(parameter.type)
}

const insertQueryParameterReference = parameter => {
  const reference = queryParameterReference(currentQueryLanguage.value, parameter.name)
  if (!reference) return
  editorRef.value?.insertText(reference)
  parameterDrawerVisible.value = false
}

const executeQuery = async () => {
  if (loadingSampleQuery.value || executing.value) return
  if (hasRelationInputs.value) {
    ElMessage.warning(t('develop.query.relationExecutionHint'))
    return
  }
  if (!selectedTarget.value) {
    ElMessage.warning(t('develop.query.selectDataSourceFirst'))
    return
  }
  const selected = editorRef.value?.getSelection()?.trim() || ''
  const query = selected || queryContent.value.trim()
  if (!query) {
    ElMessage.warning(t('develop.query.enterQueryFirst'))
    return
  }

  if (!hasValidQueryParameters()) {
    parameterDrawerVisible.value = true
    ElMessage.warning(t('develop.query.queryParametersInvalid'))
    return
  }
  if (queryParameters.value.length > 0) {
    executionParameterOverrides.value = {}
    executionParameterDialogVisible.value = true
    window.setTimeout(() => executionParameterFormRef.value?.focus(), 0)
    return
  }
  await submitQuery({})
}

const submitQuery = async (parameters = {}) => {
  if (loadingSampleQuery.value || executing.value) return
  const selected = editorRef.value?.getSelection()?.trim() || ''
  const query = selected || queryContent.value.trim()
  if (!selectedTarget.value || !query) return

  const requestSequence = ++executionRequestSequence
  executing.value = true
  executionParameterDialogVisible.value = false
  resultViewMode.value = 'table'
  try {
    const preflight = await preflightQuery({
      query_type: currentQueryLanguage.value,
      query,
      engine_id: selectedEngineId.value,
      target_locator: catalogSelection.value?.identity?.locator || targetLocator.value || undefined
    })
    if (requestSequence !== executionRequestSequence) return
    queryAnalysis.value = preflight
    if (!preflight.allowed) {
      ElMessage.error(t('develop.query.queryPermissionDenied'))
      announcement.value = t('develop.query.queryPermissionDenied')
      return
    }
    let queryConfirmationToken = ''
    if (preflight.requires_confirmation) {
      const warning = Array.isArray(preflight.warnings) && preflight.warnings.length
        ? `\n${preflight.warnings.map(queryWarningMessage).join('\n')}`
        : ''
      try {
        await ElMessageBox.confirm(
          `${t('develop.query.highRiskQueryMessage', { effect: t(`develop.query.effect.${preflight.effect}`) })}${warning}`,
          t('develop.query.highRiskQueryTitle'),
          {
            confirmButtonText: t('develop.query.confirmHighRisk'),
            cancelButtonText: t('develop.query.cancel'),
            confirmButtonClass: 'el-button--danger',
            type: 'warning',
            customClass: 'addp-message-box'
          }
        )
      } catch {
        return
      }
      queryConfirmationToken = preflight.confirmation_token || ''
    }
    const started = await createExecution({
      dev_type: 'query',
      trigger_type: 'manual',
      content: {
        query,
        query_type: currentQueryLanguage.value,
        target_locator: catalogSelection.value?.identity?.locator || targetLocator.value || undefined,
        query_parameters: queryParameters.value.map(queryParameterPayload)
      },
      execution_config: { engine_id: selectedEngineId.value },
      parameters,
      query_confirmation_token: queryConfirmationToken || undefined,
      timeout: 120
    })
    if (!started?.execution_id) throw new Error(t('develop.query.executionIdMissing'))
    executionResult.value = {
      status: 'pending', progress: 0, execution_id: started.execution_id,
      rows: [], columns: []
    }
    announcement.value = t('develop.query.executionSubmitted')

    while (requestSequence === executionRequestSequence) {
      const execution = await getExecution(started.execution_id)
      if (requestSequence !== executionRequestSequence) return
      executionResult.value = queryResultFromExecution(execution)
      if (hasGraphData.value) resultViewMode.value = 'graph'
      if (isTerminalExecutionStatus(execution.status)) {
        if (execution.status === 'success') {
          ElMessage.success(t('develop.query.executeSuccess'))
          if (executionResult.value.rows_count === 0) {
            ElMessage.warning(t('develop.query.executeSuccessNoRows'))
            announcement.value = t('develop.query.executeSuccessNoRows')
          } else {
            announcement.value = t('develop.query.executeSuccess')
          }
        } else {
          ElMessage.error(t('develop.query.executeFailed'))
          announcement.value = executionResult.value.error || t('develop.query.executeFailed')
        }
        break
      }
      await new Promise(resolve => window.setTimeout(resolve, 700))
    }
  } catch (error) {
    const responseError = error.response?.data
    const errorMessage = responseError?.details || responseError?.detail || responseError?.error || error.message
    executionResult.value = { success: false, status: 'failed', error: errorMessage, rows: [], columns: [] }
    ElMessage.error(t('develop.query.executeFailed'))
    announcement.value = errorMessage
  } finally {
    if (requestSequence === executionRequestSequence) executing.value = false
  }
}

const formatQuery = () => {
  if (!formatterLanguage.value || !queryContent.value) return
  try {
    queryContent.value = formatterLanguage.value === 'mql'
      ? formatMQLQuery(queryContent.value)
      : format(queryContent.value, {
          language: formatterLanguage.value,
          indent: '  ',
          keywordCase: 'upper',
          linesBetweenQueries: 2
        })
    ElMessage.success(t('develop.query.formatSuccess'))
  } catch (error) {
    ElMessage.error(t('develop.query.formatFailed') + error.message)
  }
}

const clearResult = () => {
  executionResult.value = null
  resultViewMode.value = 'table'
}

const queryWarningMessage = warning => ({
  target_unknown: t('develop.query.warningTargetUnknown'),
  target_required: t('develop.query.warningTargetRequired'),
  missing_where: t('develop.query.warningMissingWhere')
}[warning] || warning)

const fieldInsertionText = fieldName => {
  const text = String(fieldName || '').trim()
  if (!text) return ''
  const language = String(currentQueryLanguage.value || '').toLowerCase()
  if (language === 'mql') return text
  if (language === 'cypher') return quoteQueryIdentifier(text, '`')
  const engineType = String(selectedTarget.value?.engine?.engine_type || '').toLowerCase()
  return quoteQueryIdentifier(text, engineType === 'mysql' ? '`' : '"')
}

const loadFieldCompletions = async selection => {
  const requestSequence = ++fieldRequestSequence
  fieldCompletions.value = []
  const locator = selection?.identity?.locator || ''
  if (!locator) return
  let parsed
  try {
    parsed = parseLocator(locator)
  } catch {
    return
  }
  const itemId = selection?.identity?.item_id || parsed.itemId
  if (!itemId) return

  const cacheKey = `${parsed.engineId}:${itemId}`
  const cached = fieldCompletionCache.get(cacheKey)
  if (cached && cached.expiresAt > Date.now()) {
    fieldCompletions.value = cached.items.map(item => ({ ...item, insertText: fieldInsertionText(item.label) }))
    return
  }
  try {
    const fields = await getResourceFields('/api/v1/meta', { item_id: itemId })
    if (requestSequence !== fieldRequestSequence) return
    const items = fields
      .map(field => {
        const name = String(field?.name || '').trim()
        if (!name) return null
        const type = typeof field?.type === 'string' ? field.type : field?.type?.name || field?.native_type || ''
        return {
          label: name,
          insertText: fieldInsertionText(name),
          detail: [type, field?.comment].filter(Boolean).join(' · ') || t('develop.query.fieldCompletion'),
          kind: 'field'
        }
      })
      .filter(Boolean)
    fieldCompletionCache.set(cacheKey, { items, expiresAt: Date.now() + 60_000 })
    fieldCompletions.value = items
  } catch {
    // 字段元数据不可用时保留关键字和资源候选，不能阻断查询编辑。
  }
}

const fieldNamesFromMetadata = fields => fields
  .map(field => String(field?.name || '').trim())
  .filter(Boolean)

const fieldSourceFieldCache = new Map()
const catalogSourceEngineForId = engineID => catalogSourceEngines.value.find(engine => Number(engine.id) === Number(engineID)) || null
const federatedIdentifier = value => {
  let result = ''
  for (const char of String(value || '')) {
    if ((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
      (char >= '0' && char <= '9') || char === '_') {
      result += char
    } else {
      result += '_'
    }
  }
  if (!result) return 'engine'
  return /^[0-9]/.test(result) ? `_${result}` : result
}

const sourceEngineForSQLSource = source => {
  if (!federatedQuery.value) return selectedEngineId.value
  const sourceName = source?.path?.[0] || ''
  const sourceEngine = catalogSourceEngines.value.find(engine => (
    String(engine.name || '') === sourceName || federatedIdentifier(engine.name) === sourceName
  ))
  return sourceEngine ? Number(sourceEngine.id) : null
}

const loadFieldsForItem = async itemId => {
  const key = String(itemId || '')
  if (!key) return []
  if (fieldSourceFieldCache.has(key)) return fieldSourceFieldCache.get(key)
  const fields = await getResourceFields('/api/v1/meta', { item_id: itemId })
  const names = fieldNamesFromMetadata(fields)
  fieldSourceFieldCache.set(key, names)
  return names
}

const selectedItemIdForSource = source => {
  const selection = catalogSelection.value
  const locator = selection?.identity?.locator || targetLocator.value
  if (!locator) return null
  try {
    const parsed = parseLocator(locator)
    const sourceEngineID = sourceEngineForSQLSource(source)
    const sourcePath = federatedQuery.value ? source.path.slice(1) : source.path
    const selectedName = parsed.path.join('.').toLocaleLowerCase()
    if (Number(parsed.engineId) === Number(sourceEngineID) &&
      selectedName === sourcePath.join('.').toLocaleLowerCase()) {
      return selection?.identity?.item_id || parsed.itemId || null
    }
  } catch {
    return null
  }
  return null
}

const loadSQLFieldSources = async () => {
  const requestSequence = ++fieldSourcesRequestSequence
  if (currentQueryLanguage.value !== 'sql' || !selectedEngineId.value || !queryContent.value.trim()) {
    fieldSourceContexts.value = []
    return
  }
  const parsed = parseSQLSources(queryContent.value)
  if (!parsed.sources.length) {
    fieldSourceContexts.value = []
    return
  }
  const contexts = await Promise.all(parsed.sources.map(async source => {
    if (source.kind === 'cte') {
      return { name: source.name, alias: source.alias, fields: source.fields, known: source.fields.length > 0 }
    }
    try {
      const sourceEngineID = sourceEngineForSQLSource(source)
      if (!sourceEngineID) return { name: source.name, alias: source.alias, fields: [], known: false }
      let itemId = selectedItemIdForSource(source)
      if (!itemId) {
        const item = await getResourceItemByCatalogPath('/api/v1/meta', {
          engine_id: sourceEngineID,
          catalog_path: (federatedQuery.value ? source.path.slice(1) : source.path).join('/')
        })
        itemId = item?.id || item?.item_id
      }
      const fields = await loadFieldsForItem(itemId)
      return { name: source.name, alias: source.alias, fields, known: fields.length > 0 }
    } catch {
      return { name: source.name, alias: source.alias, fields: [], known: false }
    }
  }))
  if (requestSequence === fieldSourcesRequestSequence) fieldSourceContexts.value = contexts
}

const sqlSourceSignature = computed(() => {
  if (currentQueryLanguage.value !== 'sql') return ''
  return parseSQLSources(queryContent.value).sources
    .map(source => `${source.kind}:${source.name}:${source.alias}`)
    .join('|')
})

const rememberCatalogSelection = async (selection) => {
  const path = selection?.display?.path
  if (!path) return
  if (selection?.identity?.locator) targetLocator.value = selection.identity.locator
  const next = {
    label: selection.display.label || path,
    insertText: path,
    contextInsertText: queryTextForCatalogSegment(selection),
    detail: selection.display.type || ''
  }
  catalogCompletions.value = [next, ...catalogCompletions.value.filter(item => item.insertText !== path)].slice(0, 100)
  await loadFieldCompletions(selection)
}

const queryTextForCatalogSegment = (selection) => {
  const locator = selection?.identity?.locator
  if (!locator) return ''
  let parsed
  try {
    parsed = parseLocator(locator)
  } catch {
    return ''
  }
  if (federatedQuery.value) return queryTextForCatalogSelection(selection)
  const segment = parsed.path?.at(-1)
  if (!segment) return ''
  const engineType = String(selection.display?.engine_type || selectedTarget.value?.engine?.engine_type || '').toLowerCase()
  if (engineType === 'mongodb') return JSON.stringify(segment)
  if (engineType === 'neo4j' || selection.resource?.type === 'graph') return quoteQueryIdentifier(segment, '`')
  return quoteQueryIdentifier(segment, engineType === 'mysql' ? '`' : '"')
}

const rememberCatalogNode = async node => {
  const locator = node?.locator || ''
  if (!locator) return
  let parsed
  try {
    parsed = parseLocator(locator)
  } catch {
    return
  }
  if (!parsed.nodeId) return
  const requestSequence = ++catalogRequestSequence
  try {
    const result = await getResourceTreeNode('/api/v1/meta', parsed.engineId, locator)
    if (requestSequence !== catalogRequestSequence) return
    const engine = catalogSourceEngineForId(parsed.engineId) || selectedTarget.value?.engine
    const engineType = String(engine?.engine_type || '').toLowerCase()
    const children = (result?.children || [])
      .map(child => {
        const childLocator = child?.locator || ''
        let childParsed
        try {
          childParsed = parseLocator(childLocator)
        } catch {
          return null
        }
        if (!isQueryCatalogSelectionAllowed(child, { engine, locator: childParsed })) return null
        const childEngine = catalogSourceEngineForId(childParsed.engineId) || engine
        return {
          label: child.label || childParsed.path?.at(-1) || childLocator,
          insertText: formatLocatorDisplayPath(childLocator, { engineType: String(childEngine?.engine_type || engineType).toLowerCase() }),
          contextInsertText: queryTextForCatalogSegment({
            identity: { locator: childLocator },
            display: { engine_type: childEngine?.engine_type, engine_name: childEngine?.name },
            resource: { type: child.type }
          }),
          detail: child.type || '',
          kind: 'resource'
        }
      })
      .filter(Boolean)
    catalogCompletions.value = [
      ...children,
      ...catalogCompletions.value.filter(existing => !children.some(child => child.insertText === existing.insertText))
    ].slice(0, 100)
  } catch {
    // 节点子资源不可用时保留已有候选，不阻断编辑器。
  }
}

const isQueryCatalogSelectionAllowed = (node, { engine, locator } = {}) => {
  if (!locator?.engineId) return false
  const engineType = String(engine?.engine_type || selectedTarget.value?.engine?.engine_type || '').toLowerCase()
  if (engineType === 'mongodb') {
    return locator.type === 'database' || Boolean(locator.itemId)
  }
  return Boolean(locator.itemId)
}

const quoteQueryIdentifier = (value, quote) => {
  const text = String(value || '').trim()
  if (!text) return ''
  return `${quote}${text.replaceAll(quote, `${quote}${quote}`)}${quote}`
}

const queryTextForCatalogSelection = (selection) => {
  const locator = selection?.identity?.locator
  if (!locator) return ''
  let parsed
  try {
    parsed = parseLocator(locator)
  } catch {
    return selection.display?.path || ''
  }
  const segments = Array.isArray(parsed.path) ? parsed.path.filter(Boolean) : []
  if (segments.length === 0) return ''
  if (federatedQuery.value) {
    const sourceEngine = catalogSourceEngineForId(parsed.engineId) || selection.raw?.engine
    const sourceName = sourceEngine?.name || selection.display?.engine_name
    if (!sourceName) return ''
    return [federatedIdentifier(sourceName), ...segments.map(federatedIdentifier)].join('.')
  }
  const engineType = String(selectedTarget.value?.engine?.engine_type || '').toLowerCase()
  if (engineType === 'mongodb' || selection.resource?.type === 'collection') {
    return JSON.stringify(segments.at(-1))
  }
  if (engineType === 'neo4j' || selection.resource?.type === 'graph') {
    return quoteQueryIdentifier(segments.at(-1), '`')
  }
  const quote = engineType === 'mysql' ? '`' : '"'
  return segments.map(segment => quoteQueryIdentifier(segment, quote)).join('.')
}

const insertCatalogItemAtCursor = (selection) => {
  const text = queryTextForCatalogSelection(selection)
  if (!text) return
  rememberCatalogSelection(selection)
  editorRef.value?.insertText(text)
}

const generateQueryTemplate = async () => {
  const locator = catalogSelection.value?.identity?.locator || ''
  if (!locator && !federatedQuery.value) {
    ElMessage.warning(t('develop.query.selectResourceForQueryTemplate'))
    return
  }
  catalogDrawerVisible.value = false
  await loadSampleQuery({ locator })
}

const toggleQueryAiPanel = async () => {
  if (queryCopilotBusy.value) return
  queryAiOpen.value = !queryAiOpen.value
  if (queryAiOpen.value) {
    await nextTick()
    queryAiInputRef.value?.focus()
  }
}

const collectSelectedQueryResources = () => {
  const locator = catalogSelection.value?.identity?.locator || targetLocator.value || ''
  if (!locator || !selectedEngineId.value) return []
  let parsed
  try {
    parsed = parseLocator(locator)
  } catch {
    return []
  }
  if (!isQueryInputResource(parsed)) return []
  const name = catalogSelection.value?.display?.label || parsed.path?.at(-1) || locator
  return [resourceFact({
    role: name,
    name,
    engine_id: parsed.engineId,
    locator
  })]
}

const resolveMQLQueryResources = async selectedLocator => {
  const references = mqlCollectionReferences(queryContent.value)
  if (!selectedLocator || references.length === 0) return []
  let selected
  try {
    selected = parseLocator(selectedLocator)
  } catch {
    return []
  }
  if (String(selectedTarget.value?.engine?.engine_type || '').toLowerCase() !== 'mongodb') return []

  if (selected.itemId && references.length === 1 && selected.path?.at(-1) === references[0]) {
    return collectSelectedQueryResources()
  }

  let databaseLocator = selected.type === 'database' ? selectedLocator : ''
  if (!databaseLocator) {
    const ancestors = await getResourceTreeAncestors('/api/v1/meta', selected.engineId, selectedLocator)
    databaseLocator = (ancestors?.ancestors || []).find(node => node?.type === 'database')?.locator || ''
  }
  if (!databaseLocator) return []

  const databaseNode = await getResourceTreeNode('/api/v1/meta', selected.engineId, databaseLocator)
  const collections = []
  for (const child of databaseNode?.children || []) {
    try {
      const parsed = parseLocator(child?.locator || '')
      if (parsed.itemId && child?.type === 'collection') {
        collections.push({ name: parsed.path?.at(-1), child })
      }
    } catch {
      // 非规范子节点不能成为查询资源。
    }
  }
  const resolution = matchMQLCollectionReferences(queryContent.value, collections)
  if (resolution.missing.length > 0) return []
  return resolution.matches.map(({ name, child }) => {
    const parsed = parseLocator(child.locator)
    return resourceFact({
      role: name,
      name,
      engine_id: parsed.engineId,
      locator: child.locator
    })
  })
}

const isSelectedMongoDBDatabase = selectedLocator => {
  if (String(selectedTarget.value?.engine?.engine_type || '').toLowerCase() !== 'mongodb') return false
  try {
    return parseLocator(selectedLocator).type === 'database'
  } catch {
    return false
  }
}

const generateQueryWithCopilot = async () => {
  if (queryCopilotBusy.value) return
  if (!queryAiPrompt.value.trim()) {
    ElMessage.warning(t('develop.query.describeQuery'))
    return
  }
  if (!selectedTarget.value || !selectedEngineId.value) {
    ElMessage.warning(t('develop.query.selectDataSourceFirst'))
    return
  }
  queryAcceptedClarificationAnswers.value = {}
  const selectedLocator = catalogSelection.value?.identity?.locator || targetLocator.value || ''
  let resources = collectSelectedQueryResources()
  if (
    currentQueryLanguage.value === 'mql'
    && selectedLocator
    && queryContent.value.trim()
  ) {
    resources = await resolveMQLQueryResources(selectedLocator)
  }
  if (selectedLocator && resources.length === 0) {
    if (
      currentQueryLanguage.value === 'mql'
      && !queryContent.value.trim()
      && isSelectedMongoDBDatabase(selectedLocator)
    ) {
      await submitQueryGeneration([], { resourceScopeLocator: selectedLocator })
      return
    }
    ElMessage.warning(t('develop.query.selectQueryResourceOrDeclareCollection'))
    return
  }
  await submitQueryGeneration(resources)
}

const submitQueryGeneration = async (
  resources,
  { resourceScopeLocator = '', clarificationAnswers = {} } = {}
) => {
  generatingQuery.value = true
  try {
    const confirmedAnswers = {
      ...queryAcceptedClarificationAnswers.value,
      ...clarificationAnswers
    }
    const result = await generateQueryFromNL({
      query: queryAiPrompt.value.trim(),
      engine_id: selectedEngineId.value,
      query_language: currentQueryLanguage.value,
      resources,
      resource_scope_locator: resourceScopeLocator || undefined,
      current_query: queryContent.value.trim() || undefined,
      clarification_answers: confirmedAnswers
    })
    const resolved = resolveQueryGenerationResult(result)
    if (resolved.clarifications.length) {
      queryClarifications.value = resolved.clarifications
      queryClarificationResources.value = resolved.resources
      queryAcceptedClarificationAnswers.value = confirmedAnswers
      queryClarificationAnswers.value = Object.fromEntries(resolved.clarifications.map(clarification => [
        clarification.key,
        confirmedAnswers[clarification.key]
          ?? (clarification.control === 'multiple_choice' ? [] : '')
      ]))
      selectedQueryResourceCandidatesByRole.value = defaultResourceCandidatesByRole(
        resolved.clarifications.flatMap(clarification => clarification.resourceCandidates)
      )
      queryClarificationVisible.value = true
      return false
    }
    const generatedLanguage = resolved.queryLanguage || currentQueryLanguage.value
    const generatedQuery = formatGeneratedQueryForEditor(resolved.query, generatedLanguage)
    if (queryContent.value.trim() && queryContent.value.trim() !== generatedQuery.trim()) {
      try {
        await ElMessageBox.confirm(
          t('develop.query.replaceGeneratedQueryConfirm'),
          t('develop.query.replaceGeneratedQueryTitle'),
          {
            confirmButtonText: t('develop.query.replace'),
            cancelButtonText: t('develop.query.cancel'),
            type: 'warning',
            customClass: 'addp-message-box'
          }
        )
      } catch {
        return false
      }
    }
    queryContent.value = generatedQuery
    currentQueryLanguage.value = generatedLanguage
    const generatedAt = Date.now()
    queryParameters.value = resolved.queryParameters.map((parameter, index) => (
      queryParameterEditorItem(parameter, `generated-${generatedAt}-${index}`)
    ))
    executionParameterOverrides.value = {}
    clearResult()
    queryAiOpen.value = false
    queryClarificationVisible.value = false
    queryClarifications.value = []
    queryClarificationAnswers.value = {}
    queryAcceptedClarificationAnswers.value = {}
    queryClarificationResources.value = []
    selectedQueryResourceCandidatesByRole.value = {}
    announcement.value = t('develop.query.queryGenerated')
    if (resolved.warnings.length) ElMessage.warning(resolved.warnings.join('；'))
    else ElMessage.success(t('develop.query.queryGenerated'))
    return true
  } catch (error) {
    const detail = error.response?.data?.detail || error.response?.data?.error || error.message
    ElMessage.error(detail || t('develop.query.queryGenerationFailed'))
    return false
  } finally {
    generatingQuery.value = false
  }
}

const confirmQueryClarifications = async () => {
  if (!queryClarificationComplete.value) return
  const selectedResources = confirmedResources(
    queryResourceCandidates.value,
    selectedQueryResourceCandidatesByRole.value
  )
  const resources = selectedResources.length
    ? selectedResources
    : queryClarificationResources.value
  const clarificationAnswers = Object.fromEntries(
    Object.entries(queryClarificationAnswers.value).filter(([, value]) => (
      Array.isArray(value) ? value.length > 0 : String(value || '').trim().length > 0
    ))
  )
  queryClarificationVisible.value = false
  await submitQueryGeneration(resources, { clarificationAnswers })
}

const queryResourceCandidateName = candidate => (
  candidate.name || candidate.full_name || t('develop.query.unnamedResource')
)

const queryResourceCandidateType = candidate => {
  const type = String(candidate.asset_type || candidate.data_type || '').trim().toLowerCase()
  const typeKey = {
    collection: 'mongodbCollection',
    table: 'table',
    view: 'view',
    graph: 'graph',
    document: 'document',
    raster: 'raster',
    vector: 'vector'
  }[type] || 'generic'
  return t(`develop.query.resourceTypes.${typeKey}`)
}

const queryResourceCandidateDatabase = candidate => {
  const ancestors = Array.isArray(candidate.ancestors) ? candidate.ancestors : []
  const database = [...ancestors].reverse().find(item => item?.type === 'database' && item.label)
  return database ? t('develop.query.resourceDatabase', { name: database.label }) : ''
}

const queryResourceCandidateFacts = candidate => [
  queryResourceCandidateDatabase(candidate),
  t('develop.query.resourceType', { type: queryResourceCandidateType(candidate) }),
  candidate.geometry_column
    ? t('develop.query.geometryColumnValue', { name: candidate.geometry_column })
    : '',
  candidate.crs ? t('develop.query.coordinateSystemValue', { name: candidate.crs }) : ''
].filter(Boolean).join(' · ')

const handleSaveTask = async (taskData) => {
  if (!relationTaskValid.value) {
    ElMessage.warning(relationTaskError.value)
    return
  }
  if (!hasValidQueryParameters()) {
    parameterDrawerVisible.value = true
    ElMessage.warning(t('develop.query.queryParametersInvalid'))
    return
  }
  try {
    const task = await saveQueryTask({
      ...taskData,
      query_type: currentQueryLanguage.value,
      target_locator: hasRelationInputs.value ? '' : (catalogSelection.value?.identity?.locator || targetLocator.value || ''),
      query_parameters: queryParameters.value.map(queryParameterPayload),
      relation_inputs: normalizeRelationInputs(relationInputs.value)
    })
    currentTaskId.value = task.id
    currentTaskName.value = task.name
    currentTask.value = task
    markSaved()
    await navigateDevelopTaskEditor(router, 'query', task.id, { history: 'replace' })
    ElMessage.success(t('develop.query.saveTaskSuccess'))
    showSaveDialog.value = false
    if (saveForEngineSwitch.value && pendingQueryTarget.value) {
      const targetValue = pendingQueryTarget.value
      saveForEngineSwitch.value = false
      await applyQueryTargetSwitch(targetValue, { saved: true })
    }
  } catch (error) {
    saveForEngineSwitch.value = false
    pendingQueryTarget.value = ''
    ElMessage.error(t('develop.query.saveTaskFailed') + (error.response?.data?.error || error.message))
  }
}

const handleSaveDialogVisibility = (visible) => {
  showSaveDialog.value = visible
  if (!visible && saveForEngineSwitch.value) {
    saveForEngineSwitch.value = false
    pendingQueryTarget.value = ''
  }
}

const handlePersistQueryTask = async () => {
  if (!relationTaskValid.value) {
    ElMessage.warning(relationTaskError.value)
    return
  }
  if (!hasValidQueryParameters()) {
    parameterDrawerVisible.value = true
    ElMessage.warning(t('develop.query.queryParametersInvalid'))
    return
  }
  if (!currentTaskId.value) {
    showSaveDialog.value = true
    return
  }
  await persistCurrentQueryTask()
}

const persistCurrentQueryTask = async () => {
  if (!currentTaskId.value) return false
  if (!relationTaskValid.value) {
    ElMessage.warning(relationTaskError.value)
    return false
  }
  if (!hasValidQueryParameters()) {
    parameterDrawerVisible.value = true
    ElMessage.warning(t('develop.query.queryParametersInvalid'))
    return false
  }
  try {
    const task = currentTask.value || {}
    const updated = await updateQueryTask(currentTaskId.value, {
      name: task.name || currentTaskName.value,
      display_name: task.display_name || currentTaskName.value,
      engine_id: selectedEngineId.value,
      query: queryContent.value,
      query_type: currentQueryLanguage.value,
      target_locator: hasRelationInputs.value ? '' : (catalogSelection.value?.identity?.locator || targetLocator.value || ''),
      query_parameters: queryParameters.value.map(queryParameterPayload),
      relation_inputs: normalizeRelationInputs(relationInputs.value),
      description: task.description,
      tags: task.tags || [],
      timeout: task.timeout
    })
    currentTask.value = updated
    currentTaskName.value = updated.name
    markSaved()
    ElMessage.success(t('develop.query.updateTaskSuccess'))
    return true
  } catch (error) {
    ElMessage.error(t('develop.query.updateTaskFailed') + (error.response?.data?.error || error.message))
    return false
  }
}

const loadTask = async (taskId) => {
  const task = await getDevTask(taskId)
  currentTaskId.value = task.id
  currentTaskName.value = task.name
  currentTask.value = task
  queryContent.value = task.content?.query || ''
  queryParameters.value = (Array.isArray(task.content?.query_parameters) ? task.content.query_parameters : []).map((parameter, index) => (
    queryParameterEditorItem(parameter, `saved-${index}-${parameter.name}`)
  ))
  relationInputs.value = normalizeRelationInputs(task.content?.relation_inputs)
  executionParameterOverrides.value = {}
  currentQueryLanguage.value = String(task.content?.query_type || '').toLowerCase()
  const engineID = task.execution_config?.engine_id
  selectedQueryTarget.value = engineID ? `engine:${engineID}` : ''
  targetLocator.value = task.content?.target_locator || ''
  initialCatalogLocator.value = targetLocator.value
  catalogSelection.value = null
  fieldRequestSequence += 1
  fieldSourcesRequestSequence += 1
  fieldCompletions.value = []
  fieldSourceContexts.value = []
  clearResult()
  if (!queryContent.value) ElMessage.warning(t('develop.query.taskNoSql'))
}

const resetQueryEditorForCreate = async () => {
  currentTaskId.value = null
  currentTaskName.value = ''
  currentTask.value = null
  queryContent.value = ''
  queryParameters.value = []
  relationInputs.value = []
  executionParameterOverrides.value = {}
  targetLocator.value = ''
  initialCatalogLocator.value = ''
  catalogSelection.value = null
  fieldRequestSequence += 1
  fieldSourcesRequestSequence += 1
  fieldCompletions.value = []
  fieldSourceContexts.value = []
  clearResult()
  queryClarificationVisible.value = false
  queryClarifications.value = []
  queryClarificationAnswers.value = {}
  queryAcceptedClarificationAnswers.value = {}
  queryClarificationResources.value = []
  selectedQueryResourceCandidatesByRole.value = {}
  if (!selectedTarget.value) {
    selectedQueryTarget.value = queryTargets.value.find(target => target.available)?.value || ''
  }
  currentQueryLanguage.value = selectedCapability.value.defaultLanguage
}

async function applyQueryTaskRoute() {
  if (applyingQueryTaskRoute) return
  applyingQueryTaskRoute = true
  try {
    const taskId = developTaskIDFromRoute(route)
    const canonicalLocation = buildDevelopTaskEditorLocation('query', taskId)
    if (route.fullPath !== router.resolve(canonicalLocation).fullPath) {
      await navigateDevelopTaskEditor(router, 'query', taskId, { history: 'replace' })
    }
    if (taskId) await loadTask(taskId)
    else await resetQueryEditorForCreate()
    markSaved()
  } catch (error) {
    ElMessage.error(t('develop.query.loadTaskFailed') + error.message)
  } finally {
    applyingQueryTaskRoute = false
  }
}

const confirmUnsavedRouteChange = async () => {
  if (bypassUnsavedRouteConfirm.value) return true
  if (!isDirty.value) return true
  try {
    await ElMessageBox.confirm(
      t('develop.query.unsavedConfirm'),
      t('develop.query.unsavedTitle'),
      {
        confirmButtonText: t('develop.query.leave'),
        cancelButtonText: t('develop.query.cancel'),
        type: 'warning',
        customClass: 'addp-message-box'
      }
    )
    return true
  } catch {
    return false
  }
}

const handleBeforeUnload = (event) => {
  if (!isDirty.value) return
  event.preventDefault()
  event.returnValue = ''
}

onBeforeRouteLeave(confirmUnsavedRouteChange)
onBeforeRouteUpdate((to, from) => {
  if (String(to.query.id || '') === String(from.query.id || '')) return true
  return confirmUnsavedRouteChange()
})

onMounted(async () => {
  mediaQuery = window.matchMedia('(max-width: 820px)')
  compactMediaListener = event => { isCompact.value = event.matches }
  isCompact.value = mediaQuery.matches
  mediaQuery.addEventListener('change', compactMediaListener)
  window.addEventListener('beforeunload', handleBeforeUnload)
  await loadEngines()
  await applyQueryTaskRoute()
  queryTaskRouteReady.value = true
  markSaved()
})

watch(() => route.fullPath, () => {
  if (queryTaskRouteReady.value) applyQueryTaskRoute()
})

watch([queryContent, currentQueryLanguage, selectedEngineId, targetLocator], () => {
  queryAnalysis.value = null
})

watch(currentQueryLanguage, () => {
  fieldCompletions.value = fieldCompletions.value.map(item => ({
    ...item,
    insertText: fieldInsertionText(item.label)
  }))
})

watch([selectedEngineId, sqlSourceSignature, targetLocator], () => {
  if (fieldSourcesDebounce) window.clearTimeout(fieldSourcesDebounce)
  fieldSourcesDebounce = window.setTimeout(() => {
    loadSQLFieldSources()
  }, 250)
})

watch([selectedEngineId, federatedQuery], () => {
  refreshCatalogEngines()
})

onBeforeUnmount(() => {
  executionRequestSequence += 1
  sampleRequests.invalidate()
  catalogEngineRequestSequence += 1
  fieldSourcesRequestSequence += 1
  if (fieldSourcesDebounce) window.clearTimeout(fieldSourcesDebounce)
  window.removeEventListener('beforeunload', handleBeforeUnload)
  if (mediaQuery && compactMediaListener) {
    mediaQuery.removeEventListener('change', compactMediaListener)
  }
})
</script>

<style scoped>
.query-workbench {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--addp-bg-primary);
}

.workbench-toolbar {
  min-height: 58px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 14px;
  border-bottom: 1px solid var(--addp-border-color);
  background: var(--addp-bg-primary);
}

.toolbar-primary,
.toolbar-actions,
.result-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.toolbar-primary h2 {
  max-width: 240px;
  margin: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--addp-text-primary);
  font-size: 17px;
  font-weight: 600;
}

.engine-select {
  width: 270px;
}

.engine-type {
  float: right;
  margin-left: 20px;
  color: var(--addp-text-tertiary);
  font-size: 12px;
}

.workbench-body {
  flex: 1;
  min-height: 0;
  display: flex;
  overflow: hidden;
}

.catalog-panel {
  min-width: 240px;
  max-width: 480px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--addp-bg-secondary);
}

.catalog-heading,
.panel-heading {
  height: 40px;
  flex: 0 0 40px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 0 12px;
  border-bottom: 1px solid var(--addp-border-color);
  color: var(--addp-text-primary);
  font-size: 13px;
  font-weight: 600;
}

.panel-heading > span:first-child {
  display: flex;
  align-items: center;
  gap: 7px;
}

.editor-heading-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.catalog-tree {
  flex: 1;
  min-height: 0;
}

.catalog-tree :deep(.resource-tree-picker),
.catalog-tree :deep(.resource-tree) {
  height: 100%;
}

.catalog-tree :deep(.resource-tree) {
  border: 0;
  border-radius: 0;
}

.catalog-tree :deep(.el-card__header) {
  display: none;
}

.query-surface {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.parameter-panel-dock {
  flex: 0 0 360px;
  min-width: 320px;
  max-width: 440px;
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
  border-left: 1px solid var(--addp-border-color);
  background: var(--addp-bg-primary);
}

.parameter-panel-heading {
  height: 40px;
  flex: 0 0 40px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 12px;
  border-bottom: 1px solid var(--addp-border-color);
  color: var(--addp-text-primary);
  font-size: 13px;
  font-weight: 600;
}

.parameter-panel-heading > span {
  display: flex;
  align-items: center;
  gap: 7px;
}

.editor-panel,
.result-panel {
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.editor-panel {
  flex: 0 0 auto;
  max-height: calc(100% - 220px);
}

.result-panel {
  flex: 1;
}

.graph-result-view {
  width: 100%;
  height: 100%;
}

.editor-content,
.result-content {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.editor-content {
  display: flex;
  flex-direction: column;
}

.editor-content :deep(.monaco-editor-container) {
  flex: 1;
  min-height: 0;
}

.relation-input-config {
  flex: 0 0 auto;
  display: grid;
  gap: 8px;
  margin: 8px 12px 0;
}

.relation-input-heading {
  display: flex;
  align-items: baseline;
  gap: 10px;
  font-weight: 600;
}

.relation-input-hint {
  color: var(--addp-text-secondary);
  font-size: 12px;
  font-weight: 400;
}

.dirty-indicator {
  color: var(--el-color-warning);
  font-weight: 500;
}

.query-diagnostic-alert {
  flex: 0 0 auto;
  margin: 8px 12px 0;
}

.query-diagnostic-list {
  margin: 0;
  padding-left: 18px;
}

.query-diagnostic-list li {
  display: flex;
  align-items: center;
  gap: 8px;
}

.resize-handle {
  flex: 0 0 auto;
  background: var(--addp-border-color);
  transition: background-color 0.15s ease;
}

.resize-handle:hover,
.resize-handle:focus-visible {
  background: var(--el-color-primary);
  outline: none;
}

.resize-handle.vertical {
  width: 5px;
  cursor: col-resize;
}

.resize-handle.horizontal {
  height: 5px;
  cursor: row-resize;
}

.drawer-catalog-actions {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 8px;
}

.query-resource-context,
.query-ai-context,
.query-resource-candidate-heading {
  display: flex;
  align-items: center;
  gap: 8px;
}

.query-resource-context {
  margin-bottom: 12px;
}

.query-resource-candidate-list {
  display: grid;
  gap: 14px;
  max-height: min(52vh, 480px);
  overflow: auto;
}

.query-resource-candidate-group {
  padding: 10px 12px;
  border: 1px solid var(--addp-border-color);
  border-radius: 6px;
  background: var(--addp-bg-secondary);
}

.query-resource-candidate-group h3 {
  margin: 0 0 8px;
  color: var(--addp-text-primary);
  font-size: 13px;
}

.query-resource-candidate-options {
  display: grid;
  gap: 8px;
}

.query-resource-candidate {
  width: 100%;
  min-height: 58px;
  margin: 0;
  align-items: flex-start;
}

.query-resource-candidate :deep(.el-radio__label) {
  min-width: 0;
  flex: 1;
  white-space: normal;
}

.query-resource-candidate-content {
  display: grid;
  gap: 3px;
  min-width: 0;
}

.query-resource-candidate-heading {
  justify-content: space-between;
  min-width: 0;
  color: var(--addp-text-primary);
  font-weight: 600;
}

.query-resource-candidate-facts,
.query-resource-candidate-reason {
  color: var(--addp-text-secondary);
  font-size: 12px;
  overflow-wrap: anywhere;
}

.query-ai-fab-wrapper {
  position: fixed;
  right: 22px;
  bottom: 32px;
  z-index: 1000;
  display: flex;
  align-items: flex-end;
  gap: 10px;
}

.query-ai-fab {
  width: 42px;
  height: 42px;
  flex-shrink: 0;
  box-shadow: var(--addp-shadow-hover);
}

.query-ai-panel {
  width: min(360px, calc(100vw - 92px));
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  background: var(--addp-bg-primary);
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  box-shadow: var(--addp-shadow-card);
}

.query-ai-panel-header,
.query-ai-panel-footer,
.query-ai-selected-resource {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.query-ai-panel-header {
  color: var(--addp-text-primary);
  font-size: 13px;
  font-weight: 600;
}

.query-ai-selected-resource {
  justify-content: flex-start;
  min-width: 0;
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.query-ai-selected-resource strong {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--addp-text-primary);
}

.query-ai-panel-footer {
  justify-content: flex-end;
}

.query-ai-input :deep(.el-textarea__inner) {
  resize: none;
}

.query-ai-slide-enter-active,
.query-ai-slide-leave-active {
  transition: opacity 0.18s ease, transform 0.18s ease;
}

.query-ai-slide-enter-from,
.query-ai-slide-leave-to {
  opacity: 0;
  transform: translateX(12px);
}

.parameter-toolbar {
  display: flex;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 8px;
  padding: 12px;
  margin-bottom: 12px;
}

.parameter-sync-alert {
  margin: 12px 12px 0;
}

.parameter-list {
  display: grid;
  gap: 12px;
  min-height: 0;
  overflow: auto;
  padding: 0 12px 16px;
}

.parameter-item {
  padding: 12px;
  border: 1px solid var(--addp-border-color);
  border-radius: 6px;
  background: var(--addp-bg-secondary);
}

.parameter-item-heading,
.parameter-item-actions {
  display: flex;
  align-items: center;
}

.parameter-item-heading {
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.parameter-item-heading strong {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.parameter-item-actions {
  flex: 0 0 auto;
  gap: 6px;
}

.parameter-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 0 12px;
}

.parameter-description {
  grid-column: 1 / -1;
}

.parameter-grid :deep(.el-select),
.parameter-grid :deep(.el-input-number) {
  width: 100%;
}

@media (max-width: 1120px) {
  .workbench-toolbar {
    align-items: flex-start;
    flex-wrap: wrap;
  }

  .toolbar-actions {
    margin-left: auto;
  }
}

@media (max-width: 820px) {
  .workbench-toolbar {
    padding: 8px;
  }

  .toolbar-primary,
  .toolbar-actions {
    width: 100%;
  }

  .toolbar-primary h2 {
    display: none;
  }

  .engine-select {
    flex: 1;
    width: auto;
  }

  .toolbar-actions {
    overflow-x: auto;
    padding-bottom: 2px;
  }

  .editor-panel {
    max-height: min(52vh, calc(100% - 180px));
  }

  .parameter-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .parameter-description {
    grid-column: auto;
  }

  .parameter-panel-dock {
    display: none;
  }

  .query-ai-fab-wrapper {
    right: 12px;
    bottom: 18px;
  }

  .query-ai-panel {
    width: min(320px, calc(100vw - 76px));
  }
}
</style>
