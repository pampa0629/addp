<template>
  <div class="quality-plan-list">
    <div class="page-header">
      <div>
        <h2>{{ t("quality.plan.title") }}</h2>
        <p>{{ t("quality.plan.subtitle") }}</p>
      </div>
      <div class="page-header-actions">
        <MonitorExecutionsButton
          v-if="can('monitor.execution.read')"
          module="quality"
          task-type="quality_plan"
        />
        <el-button
          v-if="can('quality.plan.create') && can('quality.rule.read')"
          type="primary"
          :icon="Plus"
          @click="openCreate"
        >
          {{ t("quality.plan.create") }}
        </el-button>
      </div>
    </div>

    <el-form inline>
      <el-form-item :label="t('quality.domain.owner')">
        <DomainOwnershipSelect :model-value="ownerDomainID" filter :can-read="can('standard.domain.read')"
          style="width: 280px" @loaded="domains = $event" @update:model-value="changeDomain" />
      </el-form-item>
    </el-form>
    <el-alert
      v-if="loadError"
      :title="loadError"
      type="error"
      show-icon
      :closable="false"
      class="load-error"
    >
      <el-button link type="danger" @click="loadTasks">{{
        t("quality.plan.retry")
      }}</el-button>
    </el-alert>

    <el-table
      v-else
      :data="tasks"
      v-loading="loading"
      border
      :empty-text="t('quality.plan.empty')"
    >
      <el-table-column :label="t('quality.domain.owner')" min-width="180">
        <template #default="{ row }">{{ domainOwnershipLabel(row.owner_domain_id, t, domains) }}</template>
      </el-table-column>
      <el-table-column
        prop="name"
        :label="t('quality.plan.name')"
        min-width="180"
      />
      <el-table-column
        prop="code"
        :label="t('quality.plan.code')"
        min-width="180"
      />
      <el-table-column :label="t('quality.plan.ruleCount')" width="110">
        <template #default="{ row }">{{ ruleCount(row) }}</template>
      </el-table-column>
      <el-table-column :label="t('quality.plan.lastExecution')" min-width="210">
        <template #default="{ row }">
          <template v-if="row.last_execution_id">
            <el-tag
              :type="statusType(row.last_execution_status)"
              size="small"
              >{{ statusLabel(row.last_execution_status) }}</el-tag
            >
            <el-button
              v-if="can('monitor.execution.read')"
              link
              type="primary"
              @click="openExecution(row.last_execution_id)"
              >{{ t("quality.plan.executionDetail") }}</el-button
            >
          </template>
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column
        :label="t('quality.plan.actions')"
        width="220"
        fixed="right"
      >
        <template #default="{ row }">
          <el-button
            v-if="can('quality.plan.execute')"
            :disabled="runningID !== null"
            link
            type="primary"
            @click="openRun(row)"
            >{{ t("quality.plan.run") }}</el-button
          >
          <el-button
            v-if="can('quality.plan.update') && can('quality.rule.read')"
            :disabled="isActive(row)"
            link
            type="primary"
            @click="openEdit(row)"
            >{{ t("quality.plan.edit") }}</el-button
          >
          <el-popconfirm
            v-if="can('quality.plan.delete')"
            :title="t('quality.plan.deleteConfirm')"
            @confirm="deleteTask(row)"
          >
            <template #reference
              ><el-button :disabled="isActive(row)" link type="danger">{{
                t("quality.plan.delete")
              }}</el-button></template
            >
          </el-popconfirm>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination
      v-model:current-page="pagination.page"
      v-model:page-size="pagination.pageSize"
      :page-sizes="[20, 50, 100]"
      layout="total, sizes, prev, pager, next"
      :total="pagination.total"
      class="pagination"
      @current-change="changePage"
      @size-change="changePageSize"
    />

    <el-dialog
      v-model="dialogVisible"
      class="addp-dialog gate-dialog"
      :title="
        editingID ? t('quality.plan.editTitle') : t('quality.plan.createTitle')
      "
      width="min(1040px, calc(100vw - 32px))"
      :close-on-click-modal="false"
      :close-on-press-escape="!submitting"
      :show-close="!submitting"
      @closed="clearDialogRoute"
    >
      <el-form
        ref="formRef"
        :model="form"
        :rules="validationRules"
        label-position="top"
      >
        <div class="basic-grid">
          <el-form-item :label="t('quality.plan.code')" prop="code">
            <el-input
              v-model="form.code"
              maxlength="100"
              :disabled="Boolean(editingID)"
            />
          </el-form-item>
          <el-form-item :label="t('quality.plan.name')" prop="name">
            <el-input v-model="form.name" maxlength="200" />
          </el-form-item>
        </div>
        <el-form-item :label="t('quality.plan.description')">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="t('quality.domain.owner')">
          <DomainOwnershipSelect v-model="form.owner_domain_id" :can-read="can('standard.domain.read')" />
        </el-form-item>
        <el-alert :title="t('quality.targets.definitionHelp')" type="info" :closable="false" />
        <div class="section-heading">
          <el-button @click="bindingToEdit = null; tablePickerVisible = !tablePickerVisible">{{ t('quality.plan.addTable') }}</el-button>
          <el-button @click="addDeferredTable">{{ t('quality.targets.addInput') }}</el-button>
        </div>
        <el-form-item v-if="tablePickerVisible" :label="t('quality.plan.addTable')">
          <ResourceTreePicker
            api-base-url="/api/v1/meta"
            mode="item"
            :key="bindingToEdit?.alias || 'new'"
            :initial-locator="bindingToEdit?.locator || ''"
            :engine-families="['tabular']"
            :engine-filter="(engine) => engine.engine_type === 'postgresql'"
            :selectable-filter="(node) => node?.type === 'table'"
            @select="addTable"
          />
        </el-form-item>

        <template v-if="bindings.length">
          <div class="section-heading">
            <div>
              <h3>{{ t("quality.plan.bindings") }}</h3>
              <p>{{ t("quality.plan.bindingsHelp") }}</p>
            </div>
          </div>
          <el-table :data="bindings" border size="small" class="binding-table">
            <el-table-column
              :label="t('quality.plan.logicalTable')"
              min-width="260"
            >
              <template #default="{ row }">
                <template v-if="row.locator">
                  <span>{{ t('quality.targets.engine', { id: parseLocator(row.locator).engineId }) }} · {{ formatLocatorDisplayPath(row.locator) }}</span>
                  <el-button link @click="row.locator = ''">{{ t('quality.targets.defer') }}</el-button>
                </template>
                <el-tag v-else type="info">{{ t('quality.targets.requiredAtRun') }}</el-tag>
                <el-button link @click="bindingToEdit = row; tablePickerVisible = true">{{ t('quality.targets.selectDefault') }}</el-button>
              </template>
            </el-table-column>
            <el-table-column :label="t('quality.plan.alias')" min-width="220">
              <template #default="{ row }"
                ><el-input v-model="row.alias"
              /></template>
            </el-table-column>
            <el-table-column :label="t('quality.plan.actions')" width="90"
              ><template #default="{ $index }"
                ><el-button
                  link
                  type="danger"
                  @click="bindings.splice($index, 1)"
                  >{{ t("quality.plan.delete") }}</el-button
                ></template
              ></el-table-column
            >
            <el-table-column :label="t('quality.plan.columns')" min-width="300">
              <template #default="{ row }">{{
                fieldsForAlias(row.alias)
                  .map((field) => field.column_name)
                  .join(", ") || "-"
              }}</template>
            </el-table-column>
          </el-table>

          <div class="section-heading rules-heading">
            <div>
              <h3>{{ t("quality.plan.rules") }}</h3>
              <p>{{ t("quality.plan.rulesHelp") }}</p>
            </div>
            <el-select
              v-model="selectedRule"
              filterable
              remote
              :remote-method="searchRules"
              :loading="candidateLoading"
              :placeholder="t('quality.rule.selectRule')"
              @visible-change="(visible) => visible && searchRules()"
            >
              <el-option
                v-for="rule in ruleCandidates"
                :key="rule.id"
                :value="rule.id"
                :label="rule.name + ' · R' + rule.revision_no"
              />
            </el-select>
            <el-button :disabled="!selectedRule" @click="addSelectedRule">{{
              t("quality.rule.addCheck")
            }}</el-button>
          </div>

          <el-empty
            v-if="!rules.length"
            :description="t('quality.plan.rulesEmpty')"
          />
          <el-card
            v-for="(item, index) in rules"
            :key="item.rule_key"
            shadow="never"
            class="rule-card"
          >
            <template #header>
              <div class="rule-header">
                <strong
                  >{{ index + 1 }}. {{ item.rule.name }} · R{{
                    item.revision_no
                  }}</strong
                >
                <div>
                  <el-select v-model="item.severity" class="severity-select">
                    <el-option
                      value="error"
                      :label="t('quality.plan.severityError')"
                    />
                    <el-option
                      value="warning"
                      :label="t('quality.plan.severityWarning')"
                    />
                    <el-option
                      value="info"
                      :label="t('quality.plan.severityInfo')"
                    />
                  </el-select>
                  <el-button
                    link
                    type="danger"
                    @click="rules.splice(index, 1)"
                    >{{ t("quality.plan.removeRule") }}</el-button
                  >
                </div>
              </div>
            </template>
            <div class="check-toolbar">
              <el-tag>{{ ruleTypeLabel(item.rule.type) }}</el-tag>
              <el-tag
                v-if="item.rule.latest_revision_no > item.revision_no"
                type="warning"
                >{{
                  t("quality.rule.newRevision", {
                    revision: item.rule.latest_revision_no,
                  })
                }}</el-tag
              >
              <el-button link @click="refreshRuleRevision(item)">{{
                t("quality.rule.checkRevision")
              }}</el-button>
              <el-button
                v-if="item.availableRevision"
                type="warning"
                plain
                @click="upgradeRule(item)"
                >{{
                  t("quality.rule.upgrade", {
                    revision: item.availableRevision.revision_no,
                  })
                }}</el-button
              >
              <el-checkbox v-model="item.disabled">{{
                t("quality.plan.disabled")
              }}</el-checkbox>
            </div>
            <RuleConstraintFields
              :type="item.rule.type"
              :params="item.rule.params"
              disabled
            />
            <div class="rule-grid">
              <el-form-item :label="t('quality.plan.tableAlias')">
                <el-select
                  v-model="item.bindings.table"
                  @change="resetRuleColumns(item)"
                >
                  <el-option
                    v-for="b in bindings"
                    :key="b.alias"
                    :value="b.alias"
                    :label="b.alias"
                  />
                </el-select>
              </el-form-item>
              <el-form-item
                v-if="
                  [
                    'not_null',
                    'allowed_values',
                    'format',
                    'length',
                    'value_range',
                  ].includes(item.rule.type)
                "
                :label="t('quality.plan.column')"
              >
                <el-select v-model="item.bindings.column" filterable :allow-create="!aliasBinding(item.bindings.table)?.locator" default-first-option>
                  <el-option
                    v-for="field in fieldsForAlias(item.bindings.table)"
                    :key="field.column_name"
                    :label="field.column_name"
                    :value="field.column_name"
                  />
                </el-select>
              </el-form-item>
              <el-form-item
                v-if="['unique_key', 'foreign_key'].includes(item.rule.type)"
                :label="t('quality.plan.columns')"
              >
                <el-select v-model="item.bindings.columns" multiple filterable :allow-create="!aliasBinding(item.bindings.table)?.locator" default-first-option>
                  <el-option
                    v-for="field in fieldsForAlias(item.bindings.table)"
                    :key="field.column_name"
                    :label="field.column_name"
                    :value="field.column_name"
                  />
                </el-select>
              </el-form-item>
              <template v-if="item.rule.type === 'foreign_key'">
                <el-form-item :label="t('quality.plan.referenceTable')">
                  <el-select
                    v-model="item.bindings.reference_table"
                    @change="item.bindings.reference_columns = []"
                    ><el-option
                      v-for="b in bindings"
                      :key="b.alias"
                      :value="b.alias"
                      :label="b.alias"
                  /></el-select>
                </el-form-item>
                <el-form-item :label="t('quality.plan.referenceColumns')">
                  <el-select
                    v-model="item.bindings.reference_columns"
                    :allow-create="!aliasBinding(item.bindings.reference_table)?.locator"
                    default-first-option
                    multiple
                    filterable
                    ><el-option
                      v-for="field in fieldsForAlias(
                        item.bindings.reference_table,
                      )"
                      :key="field.column_name"
                      :label="field.column_name"
                      :value="field.column_name"
                  /></el-select>
                </el-form-item>
              </template>
              <template v-if="item.rule.type === 'predicate_implication'">
                <el-form-item
                  v-for="key in ['when', 'then']"
                  :key="key"
                  :label="t(`quality.plan.${key}Condition`)"
                >
                  <el-select v-model="item.bindings[key + '_column']" filterable :allow-create="!aliasBinding(item.bindings.table)?.locator" default-first-option
                    ><el-option
                      v-for="field in fieldsForAlias(item.bindings.table)"
                      :key="field.column_name"
                      :label="field.column_name"
                      :value="field.column_name"
                  /></el-select>
                </el-form-item>
              </template>
            </div>
          </el-card>
        </template>
        <el-alert
          v-if="conflict"
          :title="t('quality.plan.versionConflict')"
          type="warning"
          :closable="false"
        />
      </el-form>
      <template #footer>
        <el-button v-if="conflict" @click="reloadEditor">{{
          t("quality.rule.reload")
        }}</el-button>
        <el-button :disabled="submitting" @click="dialogVisible = false">{{
          t("quality.plan.cancel")
        }}</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">{{
          t("quality.plan.save")
        }}</el-button>
      </template>
    </el-dialog>
    <el-dialog v-model="runVisible" class="addp-dialog" :title="t('quality.targets.runTitle')" width="min(800px, calc(100vw - 24px))" :close-on-click-modal="false" :show-close="!runningID" :close-on-press-escape="!runningID" @opened="runForm?.focus()">
      <template v-if="runTask">
        <p>{{ runTask.name }}</p>
        <el-alert :title="t('quality.targets.runHelp')" type="info" :closable="false" />
        <ExecutionParameterForm ref="runForm" v-model="runParameters" :contract="runContract" :disabled="Boolean(runningID)" />
      </template>
      <template #footer>
        <el-button :disabled="Boolean(runningID)" @click="runVisible = false">{{ t('quality.plan.cancel') }}</el-button>
        <el-button type="primary" :loading="Boolean(runningID)" :disabled="!runTask" @click="runPlan">{{ t('quality.plan.run') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import { Plus } from "@element-plus/icons-vue";
import { useI18n } from "vue-i18n";
import {
  MonitorExecutionsButton,
  ResourceTreePicker,
  ExecutionParameterForm,
  parseLocator,
  formatLocatorDisplayPath,
} from "@common-ui";
import { planAPI, ruleAPI, systemCatalogAPI } from "../api/quality";
import { useAuthStore } from "../store/auth";
import { navigateQualityRoute } from "../utils/moduleNavigation";
import { executionDetailRoute } from "../utils/executionNavigation";
import {
  bindingAlias,
  createCheckItem,
  serializeCheckItems,
} from "../utils/planContract";
import RuleConstraintFields from "../components/RuleConstraintFields.vue";
import {
  buildPlanRouteQuery,
  resolvePlanRouteState,
} from "../utils/planRouteState";
import DomainOwnershipSelect from "../components/DomainOwnershipSelect.vue";
import { domainOwnershipLabel } from '../utils/domainOwnership';
const ownerDomainID = ref(null), domains = ref([]);
const changeDomain = (value) => navigateQualityRoute(router, {
  path: route.path,
  query: buildPlanRouteQuery({ mode: 'list', page: 1, pageSize: pagination.pageSize, ownerDomainID: value }),
});

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const authStore = useAuthStore();
const can = (permission) => authStore.hasPermission(permission);
const tasks = ref([]);
const runningID = ref(null);
const isActive = (task) =>
  ["pending", "running"].includes(task.last_execution_status);
const ruleCandidates = ref([]);
const selectedRule = ref(null);
const candidateLoading = ref(false);
let candidateSequence = 0;
const searchRules = async (search = "") => {
  const seq = ++candidateSequence;
  candidateLoading.value = true;
  try {
    const result = await ruleAPI.list({ search, page: 1, page_size: 100 });
    if (seq === candidateSequence && !disposed)
      ruleCandidates.value = result.data;
  } catch (error) {
    if (seq === candidateSequence)
      ElMessage.error(
        error.response?.data?.error || t("quality.plan.loadFailed"),
      );
  } finally {
    if (seq === candidateSequence) candidateLoading.value = false;
  }
};
const addSelectedRule = () => {
  const rule = ruleCandidates.value.find((r) => r.id === selectedRule.value);
  if (!rule) return;
  rules.value.push(createCheckItem(rule, bindings.value[0]?.alias || ""));
  selectedRule.value = null;
};
const refreshRuleRevision = async (item) => {
  try {
    const latest = await ruleAPI.get(item.rule_id);
    if (disposed || !dialogVisible.value || !rules.value.includes(item)) return;
    item.rule.latest_revision_no = latest.revision_no;
    if (latest.revision_no <= item.revision_no) return;
    item.availableRevision = latest;
  } catch (error) {
    ElMessage.error(
      error.response?.data?.error || t("quality.plan.loadFailed"),
    );
  }
};
const upgradeRule = (item) => {
  const latest = item.availableRevision;
  if (!latest) return;
  if (item.rule.type !== latest.type)
    item.bindings = createCheckItem(latest, item.bindings.table).bindings;
  item.revision_no = latest.revision_no;
  item.rule = {
    ...latest,
    rule_id: latest.id,
    latest_revision_no: latest.revision_no,
  };
  delete item.availableRevision;
};
const runVisible = ref(false), runTask = ref(null), runParameters = ref({}), runForm = ref(null);
const openRun = async (task) => {
  try {
    const detail = await planAPI.get(task.id);
    if (disposed) return;
    runTask.value = detail;
    const required = Object.fromEntries(detail.table_bindings.filter(binding => !binding.locator).map(binding => [binding.alias, '']));
    runParameters.value = Object.keys(required).length ? { table_bindings: required } : {};
    runVisible.value = true;
  } catch (error) { ElMessage.error(error.response?.data?.error || t('quality.plan.loadFailed')); }
};
const runContract = computed(() => {
  const contract = runTask.value?.execution_contract;
  if (!contract) return {};
  return { ...contract, input_ui_schema: { ...contract.input_ui_schema, table_bindings: { ...contract.input_ui_schema.table_bindings, title: t('quality.targets.actual') } } };
});
const runPlan = async () => {
  const task = runTask.value;
  if (!task || runningID.value !== null) return;
  const overrides = runParameters.value.table_bindings || {};
  const missing = task.table_bindings.filter(binding => !(Object.hasOwn(overrides, binding.alias) ? overrides[binding.alias] : binding.locator));
  if (missing.length) return ElMessage.error(t('quality.targets.missing', { aliases: missing.map(binding => binding.alias).join(', ') }));
  runningID.value = task.id;
  try {
    const result = await planAPI.run(task.id, runParameters.value);
    runVisible.value = false;
    ElMessage.success(t("quality.plan.runSuccess"));
    await loadTasks();
    if (can("monitor.execution.read")) openExecution(result.execution_id);
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t("quality.plan.runFailed"));
  } finally {
    runningID.value = null;
  }
};

const fieldsByTableID = ref(new Map());
const bindings = ref([]);
const tablePickerVisible = ref(false);
const bindingToEdit = ref(null);
const addDeferredTable = () => {
  let i = bindings.value.length + 1;
  while (bindings.value.some(b => b.alias === `input_${i}`)) i++;
  bindings.value.push({ alias: `input_${i}`, locator: '' });
};
const rules = ref([]);
const loading = ref(false);
const loadError = ref("");
const dialogVisible = ref(false);
const submitting = ref(false);
const conflict = ref(false);
const editingID = ref(null);
const formRef = ref(null);
const pagination = reactive({ page: 1, pageSize: 20, total: 0 });
const form = reactive({ code: "", name: "", description: "", owner_domain_id: null, version: 0 });

let routeReady = false;
let listSequence = 0;
let pollTimer;
let disposed = false;
onBeforeUnmount(() => {
  disposed = true;
  listSequence++;
  candidateSequence++;
  clearTimeout(pollTimer);
});

const validationRules = {
  code: [
    {
      required: true,
      message: t("quality.plan.codeRequired"),
      trigger: "blur",
    },
    {
      pattern: /^[a-z][a-z0-9_]*$/,
      message: t("quality.plan.codeFormat"),
      trigger: "blur",
    },
  ],
  name: [
    {
      required: true,
      message: t("quality.plan.nameRequired"),
      trigger: "blur",
    },
  ],
};

const parseJSON = (value) =>
  typeof value === "string" ? JSON.parse(value) : value;
const ruleCount = (task) => task.check_items?.length || 0;
const statusType = (status) =>
  ({
    success: "success",
    failed: "danger",
    timeout: "danger",
    running: "warning",
    pending: "info",
  })[status] || "info";
const statusLabel = (status) =>
  ({
    pending: t("quality.execution.pending"),
    running: t("quality.execution.running"),
    success: t("quality.execution.success"),
    failed: t("quality.execution.failed"),
    timeout: t("quality.execution.timeout"),
    cancelled: t("quality.execution.cancelled"),
  })[status] ||
  status ||
  "-";
const ruleTypeLabel = (type) => t(`quality.plan.type_${type}`);

const aliasBinding = (alias) =>
  bindings.value.find((binding) => binding.alias === alias);
const fieldsForAlias = (alias) => {
  const binding = aliasBinding(alias);
  return binding ? fieldsByTableID.value.get(binding.locator) || [] : [];
};

const resetForm = () => {
  conflict.value = false;
  selectedRule.value = null;
  ruleCandidates.value = [];
  candidateSequence++;
  Object.assign(form, { code: "", name: "", description: "", owner_domain_id: null, version: 0 });
  bindings.value = [];
  rules.value = [];
  fieldsByTableID.value = new Map();
  tablePickerVisible.value = false;
  bindingToEdit.value = null;
};

const syncRoute = (mode = "list", taskID = null, history = "replace") =>
  navigateQualityRoute(
    router,
    {
      path: route.path,
      query: buildPlanRouteQuery({
        mode,
        taskID,
        page: pagination.page,
        pageSize: pagination.pageSize,
        ownerDomainID: ownerDomainID.value,
      }),
    },
    { history },
  );

const loadTasks = async () => {
  clearTimeout(pollTimer);
  const sequence = ++listSequence;
  loading.value = true;
  loadError.value = "";
  try {
    const response = await planAPI.list({
      page: pagination.page,
      page_size: pagination.pageSize,
      ...(ownerDomainID.value != null ? { owner_domain_id: ownerDomainID.value } : {}),
    });
    if (sequence !== listSequence) return;
    tasks.value = response?.data || [];
    pagination.total = response?.total || 0;
    const lastPage = Math.max(
      1,
      Math.ceil(pagination.total / pagination.pageSize),
    );
    if (pagination.page > lastPage) {
      pagination.page = lastPage;
      await syncRoute();
    }
  } catch (error) {
    if (sequence !== listSequence) return;
    tasks.value = [];
    pagination.total = 0;
    loadError.value =
      error.response?.data?.error || t("quality.plan.loadFailed");
  } finally {
    if (sequence === listSequence) {
      loading.value = false;
      if (!disposed && tasks.value.some(isActive))
        pollTimer = setTimeout(loadTasks, 2000);
    }
  }
};

const loadFields = async (locatorText) => {
  if (!locatorText) return;
  const locator = parseLocator(locatorText);
  const roots = await systemCatalogAPI.listChildren(locator.engineId);
  const root = roots.nodes.find(
    (node) => node.role === "branch" && node.path.segments.length === 1,
  );
  if (!root) throw new Error(t("quality.plan.referenceLoadFailed"));
  const schemas = await systemCatalogAPI.listChildren(
    locator.engineId,
    root.path,
    { limit: 1000 },
  );
  const schema = schemas.nodes.find((node) => node.name === locator.path[0]);
  if (!schema) throw new Error(t("quality.plan.referenceLoadFailed"));
  const children = await systemCatalogAPI.listChildren(
    locator.engineId,
    schema.path,
    { limit: 1000 },
  );
  const table = children.nodes.find((node) => node.name === locator.path[1]);
  if (!table) throw new Error(t("quality.plan.referenceLoadFailed"));
  const facts = await systemCatalogAPI.describeFacts(
    locator.engineId,
    table.path,
  );
  fieldsByTableID.value.set(
    locatorText,
    facts.table.fields.map((field) => ({ ...field, column_name: field.name })),
  );
};
const addTable = async (selection) => {
  const locator = selection?.identity?.locator;
  if (!locator || bindingToEdit.value?.locator === locator || bindings.value.some((binding) => binding !== bindingToEdit.value && binding.locator === locator))
    return;
  const parsed = parseLocator(locator);
  if (
    bindings.value.some(b => b !== bindingToEdit.value && b.locator && parseLocator(b.locator).engineId !== parsed.engineId)
  )
    return ElMessage.error(t("quality.plan.sameEngine"));
  try {
    await loadFields(locator);
    if (bindingToEdit.value) {
      bindingToEdit.value.locator = locator;
      bindingToEdit.value = null;
      tablePickerVisible.value = false;
      return;
    }
    bindings.value.push({
      locator,
      alias: bindingAlias(parsed.path.at(-1), bindings.value.length + 1),
    });
  } catch (error) {
    ElMessage.error(error.message || t("quality.plan.referenceLoadFailed"));
  }
};

const resetRuleColumns = (item) => {
  item.bindings = createCheckItem(
    { id: item.rule_id, revision_no: item.revision_no, ...item.rule },
    item.bindings.table,
  ).bindings;
};

const openCreate = () => syncRoute("create", null, "push");
const openEdit = (task) => syncRoute("edit", task.id, "push");
const openExecution = (executionID) => {
  const location = executionDetailRoute(executionID);
  if (location) navigateQualityRoute(router, location, { history: "push" });
};
const clearDialogRoute = () => {
  editingID.value = null;
  resetForm();
  if (routeReady && resolvePlanRouteState(route.query).mode !== "list")
    syncRoute();
};

let dialogSequence = 0;
const restoreDialog = async (state) => {
  const sequence = ++dialogSequence;
  conflict.value = false;
  if (state.mode === "list") {
    dialogVisible.value = false;
    editingID.value = null;
    resetForm();
    return;
  }
  const permission =
    state.mode === "edit" ? "quality.plan.update" : "quality.plan.create";
  if (!can(permission) || !can("quality.rule.read")) {
    ElMessage.error(t("quality.plan.permissionDenied"));
    await syncRoute();
    return;
  }
  if (state.mode === "create") {
    editingID.value = null;
    resetForm();
    dialogVisible.value = true;
    return;
  }
  try {
    const task = await planAPI.get(state.taskID);
    if (sequence !== dialogSequence || disposed) return;
    editingID.value = task.id;
    Object.assign(form, {
      code: task.code,
      name: task.name,
      description: task.description || "",
      owner_domain_id: task.owner_domain_id || null,
      version: task.version,
    });
    const existingBindings = parseJSON(task.table_bindings) || [];
    bindings.value = existingBindings;
    const fieldResults = await Promise.allSettled(
      existingBindings.map((binding) => loadFields(binding.locator)),
    );
    if (sequence !== dialogSequence || disposed) return;
    if (fieldResults.some((result) => result.status === "rejected"))
      ElMessage.warning(t("quality.plan.referenceLoadFailed"));
    rules.value = structuredClone(task.check_items);
    dialogVisible.value = true;
  } catch (error) {
    if (sequence !== dialogSequence || disposed) return;
    ElMessage.error(
      error.response?.data?.error || t("quality.plan.loadFailed"),
    );
    await syncRoute();
  }
};

const reloadEditor = async () => {
  try {
    await ElMessageBox.confirm(
      t("quality.rule.discardEdits"),
      t("quality.rule.reload"),
      {
        customClass: "addp-message-box",
        confirmButtonText: t("quality.rule.reload"),
        cancelButtonText: t("quality.plan.cancel"),
      },
    );
    await restoreDialog(resolvePlanRouteState(route.query));
  } catch {}
};
const assertValid = () => {
  if (!bindings.value.length || !rules.value.length)
    return t("quality.plan.ruleInvalid");
  const aliases = bindings.value.map((b) => b.alias);
  if (
    new Set(aliases).size !== aliases.length ||
    aliases.some((a) => !/^[a-z][a-z0-9_]*$/.test(a))
  )
    return t("quality.plan.ruleInvalid");
  for (const item of rules.value) {
    const b = item.bindings,
      type = item.rule.type;
    if (!aliases.includes(b.table)) return t("quality.plan.ruleInvalid");
    if (
      [
        "not_null",
        "allowed_values",
        "format",
        "length",
        "value_range",
      ].includes(type) &&
      !b.column
    )
      return t("quality.plan.ruleInvalid");
    if (["unique_key", "foreign_key"].includes(type) && !b.columns.length)
      return t("quality.plan.ruleInvalid");
    if (
      type === "foreign_key" &&
      (!aliases.includes(b.reference_table) ||
        b.columns.length !== b.reference_columns.length)
    )
      return t("quality.plan.ruleInvalid");
    if (type === "predicate_implication" && (!b.when_column || !b.then_column))
      return t("quality.plan.ruleInvalid");
  }
  return "";
};

const submit = async () => {
  if (submitting.value) return;
  const permission = editingID.value
    ? "quality.plan.update"
    : "quality.plan.create";
  if (!can(permission) || !can("quality.rule.read"))
    return ElMessage.error(t("quality.plan.permissionDenied"));
  try {
    await formRef.value.validate();
  } catch {
    return;
  }
  if (submitting.value) return;
  const validationError = assertValid();
  if (validationError) return ElMessage.error(validationError);
  submitting.value = true;
  try {
    const payload = {
      code: form.code.trim(),
      name: form.name.trim(),
      description: form.description.trim(),
      owner_domain_id: form.owner_domain_id ?? null,
      table_bindings: bindings.value.map((binding) => ({
        alias: binding.alias.trim(),
        locator: binding.locator,
      })),
      check_items: serializeCheckItems(rules.value),
      ...(editingID.value ? { version: form.version } : {}),
    };
    if (editingID.value) await planAPI.update(editingID.value, payload);
    else await planAPI.create(payload);
    ElMessage.success(
      editingID.value
        ? t("quality.plan.updateSuccess")
        : t("quality.plan.createSuccess"),
    );
    dialogVisible.value = false;
    await loadTasks();
  } catch (error) {
    conflict.value =
      error.response?.data?.error_code === "resource_version_conflict";
    ElMessage.error(
      error.response?.data?.error || t("quality.plan.saveFailed"),
    );
  } finally {
    submitting.value = false;
  }
};

const deleteTask = async (task) => {
  if (!can("quality.plan.delete"))
    return ElMessage.error(t("quality.plan.permissionDenied"));
  try {
    await planAPI.delete(task.id, task.version);
    ElMessage.success(t("quality.plan.deleteSuccess"));
    await loadTasks();
  } catch (error) {
    ElMessage.error(
      error.response?.data?.error || t("quality.plan.deleteFailed"),
    );
  }
};

const changePage = async (page) => {
  pagination.page = page;
  await syncRoute();
};
const changePageSize = async (size) => {
  pagination.pageSize = size;
  pagination.page = 1;
  await syncRoute();
};

watch(
  () => route.query,
  async (query) => {
    const state = resolvePlanRouteState(query);
    if (state.changed) {
      await navigateQualityRoute(
        router,
        { path: route.path, query: state.query },
        { history: "replace" },
      );
      return;
    }
    const listChanged =
      pagination.page !== state.page || pagination.pageSize !== state.pageSize || ownerDomainID.value !== state.ownerDomainID;
    pagination.page = state.page;
    pagination.pageSize = state.pageSize;
    ownerDomainID.value = state.ownerDomainID;
    if (listChanged || !routeReady) await loadTasks();
    await restoreDialog(state);
    routeReady = true;
  },
  { immediate: true },
);
</script>

<style scoped>
.quality-plan-list {
  padding: 20px;
}
.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
}
.page-header-actions {
  display: flex;
  gap: 8px;
}
.page-header h2 {
  margin: 0;
  font-size: 18px;
}
.page-header p,
.section-heading p {
  margin: 6px 0 0;
  color: var(--el-text-color-secondary);
}
.load-error {
  margin-bottom: 16px;
}
.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}
.basic-grid,
.rule-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 16px;
}
.section-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin: 18px 0 10px;
}
.section-heading h3 {
  margin: 0;
  font-size: 16px;
}
.binding-table {
  margin-bottom: 18px;
}
.rules-heading {
  align-items: center;
}
.check-toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  margin-bottom: 16px;
}
.rule-card {
  margin-bottom: 12px;
}
.rule-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}
.severity-select {
  width: 170px;
  margin-right: 10px;
}
.full-width {
  width: 100%;
}
.condition-editor {
  grid-column: 1 / -1;
  margin-bottom: 14px;
}
.condition-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
  margin-top: 8px;
}
@media (max-width: 760px) {
  .basic-grid,
  .rule-grid,
  .condition-grid {
    grid-template-columns: 1fr;
  }
}
</style>
