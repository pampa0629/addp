<template>
  <div class="metric-workspace" v-loading="loading">
    <div class="workspace-header">
      <h2>{{ item?.name || t('model.metric_workspace.title') }}</h2>
      <el-button v-if="route.params.id" @click="go()">{{
        t('model.metric_workspace.back')
      }}</el-button
      ><el-button v-else-if="can('create')" type="primary" @click="go('new')">{{
        t('model.metric_workspace.create')
      }}</el-button>
    </div>
    <el-alert
      v-if="error"
      :title="error"
      type="error"
      show-icon
      :closable="false"
    />
    <el-alert v-if="!error && domainLoadError" :title="t('model.common.reference_data_unavailable')" type="warning" show-icon :closable="false" />
    <el-card v-if="!route.params.id && !error" shadow="never">
      <div class="metric-domain-filter">
        <span id="metric-source-domain-label">{{ t('model.metric_workspace.source_domain') }}</span>
        <BusinessDomainSelect
          :model-value="sourceDomainId || ''"
          :options="domains"
          aria-labelledby="metric-source-domain-label"
          @update:model-value="changeSourceDomain"
        >
          <el-option :label="t('model.metric_workspace.all_source_domains')" value="" />
          <el-option v-if="domainLoadError && sourceDomainId" :label="t('model.metric_workspace.domain_unavailable')" :value="sourceDomainId" />
        </BusinessDomainSelect>
      </div>
      <el-table :data="filteredItems" stripe
        ><el-table-column prop="name" :label="t('model.metric_workspace.name')"
          ><template #default="{ row }"
            ><el-button link type="primary" @click="go(row.id)">{{
              row.name
            }}</el-button></template
          ></el-table-column
        ><el-table-column :label="t('model.metric_workspace.source')"
          ><template #default="{ row }">{{
            tables.find((table) => table.id === row.fact_table_id)?.name || '—'
          }}</template></el-table-column
        ><el-table-column :label="t('model.metric_workspace.source_domain')" min-width="150"
          ><template #default="{ row }">{{ sourceDomainLabel(row) }}</template></el-table-column
        ><el-table-column :label="t('model.metric_workspace.definition_ownership')" min-width="150"
          ><template #default="{ row }">{{ definitionOwnershipLabel(row) }}</template></el-table-column
        ></el-table
      >
    </el-card>
    <el-card v-else-if="isNew && !error" shadow="never">
      <el-form label-position="top" :disabled="!can('create') || busy">
        <el-form-item :label="t('model.metric_workspace.definition')" required
          ><el-select
            v-model="identity.metric_definition_id"
            filterable
            @change="selectDefinition"
            ><el-option
              v-for="definition in definitions"
              :key="definition.id"
              :value="definition.id"
              :label="
                definition.current_revision?.name || definition.code
              " /></el-select
        ></el-form-item>
        <el-form-item :label="t('model.metric_workspace.source')" required
          ><el-select v-model="identity.fact_table_id" filterable
            ><el-option
              v-for="table in tables.filter(
                (v) => v.table_type === 'fact' && v.status === 'approved',
              )"
              :key="table.id"
              :value="table.id"
              :label="table.name" /></el-select
        ></el-form-item>
        <el-form-item :label="t('model.metric_workspace.name')" required
          ><el-input v-model="identity.name" maxlength="200"
        /></el-form-item>
        <el-button type="primary" :loading="busy" @click="create">{{
          t('model.metric_workspace.create')
        }}</el-button>
      </el-form>
    </el-card>
    <template v-else-if="item && !error">
      <el-card shadow="never">
        <div class="workspace-header">
          <el-button link type="primary" @click="openDefinition">{{
            t('model.metric_workspace.definition_detail')
          }}</el-button
          ><el-button
            link
            type="primary"
            @click="
              navigateModelRoute(router, {
                path: `/logical-tables/${item.fact_table_id}`,
              })
            "
            >{{
              tables.find((v) => v.id === item.fact_table_id)?.name
            }}</el-button
          >
        </div>
        <el-select
          :model-value="selectedRevision"
          :placeholder="t('model.metric_workspace.revision')"
          @change="selectRevision"
          ><el-option
            v-for="revision in item.revisions"
            :key="revision.id"
            :value="revision.id"
            :label="`R${revision.revision_no} · ${t(`model.metric_workspace.${revision.status}`)}`"
        /></el-select>
        <el-tag
          v-if="revision"
          :type="revision.status === 'published' ? 'success' : 'info'"
          >{{ t(`model.metric_workspace.${revision.status}`) }}</el-tag
        >
        <MetricRevisionServices
          v-if="revision && !editingNew"
          :implementation-id="item.id" :revision="revision"
          :can-read="auth.hasPermission('service.definition.read')"
        />
        <el-button v-if="!editable && can('update')" @click="startDraft">{{
          t('model.metric_workspace.new_draft')
        }}</el-button>
        <el-popconfirm
          v-if="
            can('delete') && item.revisions.every((v) => v.status === 'draft')
          "
          :title="t('model.metric_workspace.confirm_delete')"
          @confirm="remove"
          ><template #reference
            ><el-button link type="danger">{{
              t('model.metric_workspace.delete')
            }}</el-button></template
          ></el-popconfirm
        >
      </el-card>
      <MetricSourceSummary
        :implementation="item" :revision="revision" :tables="tables"
        :engines="sourceEngines" :engine-error="sourceEngineError"
        :pending="editingNew || isDirty"
        :can-navigate="auth.hasPermission('model.logical_model.read')"
        @open-table="id => navigateModelRoute(router, { path: `/logical-tables/${id}`, query: { tab: 'physical-target' } })"
      />
      <el-card shadow="never">
        <el-alert
          v-if="!editable"
          :title="t('model.metric_workspace.immutable')"
          type="info"
          :closable="false"
        />
        <el-form
          label-position="top"
          :disabled="!editable || !can('update') || busy"
        >
          <el-form-item :label="t('model.metric_workspace.operation')" required>
            <el-select v-model="form.operation" @change="operationChanged">
              <el-option
                value="count_distinct"
                :label="t('model.metric_workspace.count_distinct')"
              />
              <el-option
                value="directional_overlap"
                :label="t('model.metric_workspace.directional_overlap')"
              />
              <el-option
                value="sum_decimal_by_group"
                :label="t('model.metric_workspace.sum_decimal_by_group')"
              />
            </el-select>
          </el-form-item>
          <el-alert
            v-if="form.operation === 'directional_overlap'"
            :title="t('model.metric_workspace.overlap_contract')"
            type="info"
            :closable="false"
          />
          <el-alert
            v-if="form.operation === 'sum_decimal_by_group'"
            :title="t('model.metric_workspace.grouped_sum_contract')"
            type="info"
            :closable="false"
          />
          <el-form-item
            :label="t('model.metric_workspace.choose_revision')"
            required
            ><el-select v-model="form.metric_definition_revision_id"
              ><el-option
                v-for="definition in definitionRevisions"
                :key="definition.id"
                :value="definition.id"
                :label="`${definition.name} · R${definition.revision_no}`" /></el-select
          ></el-form-item>
          <div v-if="form.operation !== 'sum_decimal_by_group'" class="field-grid">
            <el-form-item :label="t('model.metric_workspace.subject')" required
              ><el-select v-model="form.subject_field_id"
                ><el-option
                  v-for="field in choices.filter(
                    (v) => v.relation_id === 0 && v.data_type === 'string',
                  )"
                  :key="field.key"
                  :value="field.field_id"
                  :label="field.label" /></el-select
            ></el-form-item>
            <el-form-item
              :label="t('model.metric_workspace.subject_relation')"
              required
              ><el-select v-model="form.subject_relation_id" @change="form.subject_label = ''"
                ><el-option
                  v-for="relation in relations.filter(
                    (v) => v.source_field === form.subject_field_id,
                  )"
                  :key="relation.id"
                  :value="relation.id"
                  :label="
                    relation.target_table_name ||
                    tables.find((v) => v.id === relation.target_table)?.name
                  " /></el-select
            ></el-form-item>
            <el-form-item v-if="form.operation === 'count_distinct'">
              <el-checkbox v-model="form.include_details">{{ t('model.metric_workspace.include_details') }}</el-checkbox>
            </el-form-item>
            <el-form-item :label="t('model.metric_workspace.subject_label')">
              <el-select v-model="form.subject_label" clearable>
                <el-option
                  v-for="field in choices.filter(v => v.relation_id === form.subject_relation_id && v.data_type === 'string')"
                  :key="field.key" :value="field.key" :label="field.label"
                />
              </el-select>
            </el-form-item>
            <el-form-item :label="t('model.metric_workspace.distinct')" required
              ><el-select v-model="form.distinct"
                ><el-option
                  v-for="field in choices"
                  :key="field.key"
                  :value="field.key"
                  :label="field.label" /></el-select
            ></el-form-item>
            <el-form-item :label="t('model.metric_workspace.time')" required
              ><el-select v-model="form.time"
                ><el-option
                  v-for="field in choices.filter((v) => v.data_type === 'date')"
                  :key="field.key"
                  :value="field.key"
                  :label="field.label" /></el-select
            ></el-form-item>
          </div>
          <div v-else class="field-grid">
            <el-form-item :label="t('model.metric_workspace.group_field')" required>
              <el-select v-model="form.group">
                <el-option
                  v-for="field in choices.filter(v => v.relation_id === 0 && v.data_type === 'string')"
                  :key="field.key" :value="field.key" :label="field.label"
                />
              </el-select>
            </el-form-item>
            <el-form-item :label="t('model.metric_workspace.measure_field')" required>
              <el-select v-model="form.measure">
                <el-option
                  v-for="field in choices.filter(v => v.relation_id === 0 && v.data_type === 'decimal')"
                  :key="field.key" :value="field.key" :label="field.label"
                />
              </el-select>
            </el-form-item>
          </div>
          <el-form-item
            v-if="form.operation === 'count_distinct'"
            :label="t('model.metric_workspace.filters')"
            ><div class="filters">
              <div
                v-for="(filter, index) in form.filters"
                :key="index"
                class="filter-row"
              >
                <el-select v-model="filter.field"
                  ><el-option
                    v-for="field in choices.filter(
                      (v) => v.data_type === 'bool',
                    )"
                    :key="field.key"
                    :value="field.key"
                    :label="field.label" /></el-select
                ><el-select v-model="filter.value"
                  ><el-option
                    :value="true"
                    :label="t('model.metric_workspace.true')" /><el-option
                    :value="false"
                    :label="t('model.metric_workspace.false')" /></el-select
                ><el-button @click="form.filters.splice(index, 1)">{{
                  t('model.common.delete')
                }}</el-button>
              </div>
              <el-button
                @click="form.filters.push({ field: '', value: true })"
                >{{ t('model.metric_workspace.add_filter') }}</el-button
              >
            </div></el-form-item
          >
          <el-button
            v-if="editable"
            type="primary"
            :loading="busy"
            @click="save"
            >{{ t('model.metric_workspace.save') }}</el-button
          >
        </el-form>
      </el-card>
      <el-card v-if="revision" shadow="never">
        <p>{{ t('model.metric_workspace.contract_valid') }}</p>
        <el-button
          v-if="revision.status === 'draft' && can('publish')"
          type="primary"
          :disabled="isDirty"
          :loading="busy"
          @click="publishRevision"
          >{{ t('model.metric_workspace.publish') }}</el-button
        >
        <el-button
          v-if="revision.status === 'published' && !editingNew && can('offline')"
          :loading="busy"
          @click="withdrawDialog = true"
          >{{ t('model.metric_workspace.withdraw') }}</el-button
        >
        <el-button
          v-if="
            revision.status === 'published' &&
            !editingNew &&
            (auth.hasPermission('service.definition.create') ||
              auth.hasPermission('service.definition.update'))
          "
          type="primary"
          @click="openServiceDialog"
          >{{ t('model.metric_workspace.service') }}</el-button
        >
      </el-card>
    </template>
    <el-dialog
      v-model="withdrawDialog"
      class="addp-dialog"
      :title="t('model.metric_workspace.withdraw_title', { revision: revision?.revision_no })"
      width="min(560px, calc(100vw - 32px))"
      :close-on-click-modal="!busy"
      :close-on-press-escape="!busy"
      :show-close="!busy"
    >
      <p aria-live="polite">
        {{ withdrawLoading ? t('model.metric_workspace.withdraw_loading') :
          withdrawCount !== null ? t('model.metric_workspace.withdraw_count', { count: withdrawCount }) :
          t(`model.metric_workspace.${withdrawError}`) }}
      </p>
      <el-alert :title="t('model.metric_workspace.withdraw_effect')" type="warning" :closable="false" />
      <template #footer>
        <el-button v-if="auth.hasPermission('service.definition.read')" :loading="withdrawLoading" :disabled="busy" @click="withdrawRefresh++">{{ t('model.metric_workspace.refresh_services') }}</el-button>
        <el-button :disabled="busy" @click="withdrawDialog = false">{{ t('common.cancel') }}</el-button>
        <el-button type="danger" :loading="busy" :disabled="withdrawLoading || !can('offline')" @click="withdrawRevision">{{ t('model.metric_workspace.confirm_withdraw') }}</el-button>
      </template>
    </el-dialog>
    <el-dialog
      class="addp-dialog"
      v-model="serviceDialog"
      :title="t('model.metric_workspace.service')"
      width="min(760px, calc(100vw - 32px))"
      >
      <MetricSourceSummary
        v-if="serviceDialog && item && revision"
        class="publication-source"
        :implementation="item" :revision="revision" :tables="tables"
        :engines="sourceEngines" :engine-error="sourceEngineError"
        show-implementation
      />
      <el-form label-position="top">
        <el-form-item :label="t('model.metric_workspace.result_kind')">
          <el-select v-model="serviceResultKind">
            <el-option value="" :label="t('model.metric_workspace.summary_result')" />
            <el-option v-if="revision?.contract.include_details" value="details" :label="t('model.metric_workspace.detail_result')" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('model.metric_workspace.service_target')">
          <el-radio-group v-model="serviceMode">
            <el-radio
              value="create"
              :disabled="!auth.hasPermission('service.definition.create')"
              >{{ t('model.metric_workspace.create_service') }}</el-radio
            >
            <el-radio
              value="rebind"
              :disabled="
                !auth.hasPermission('service.definition.update') ||
                !auth.hasPermission('service.definition.read')
              "
              >{{ t('model.metric_workspace.rebind_service') }}</el-radio
            >
          </el-radio-group>
        </el-form-item>
        <el-form-item
          v-if="serviceMode === 'rebind'"
          :label="t('model.metric_workspace.existing_service')"
        >
          <el-select
            v-model="existingServiceID"
            filterable
            remote
            :remote-method="searchServices"
            :loading="serviceSearchLoading"
          >
            <el-option
              v-for="service in existingServices"
              :key="service.id"
              :value="service.id"
              :label="`${service.title} (${service.service_name})`"
            />
          </el-select>
        </el-form-item>
        <el-alert v-if="serviceVersionConflict" type="warning" :closable="false">
          {{ t('model.metric_workspace.rebind_conflict') }}
          <el-button link type="primary" :loading="busy" @click="reloadServiceTarget">{{ t('model.metric_workspace.reload_service') }}</el-button>
        </el-alert>
        <el-alert
          v-if="serviceMode === 'rebind'"
          :title="t('model.metric_workspace.rebind_warning')"
          type="warning"
          :closable="false" />
        <el-form-item v-else :label="t('model.metric_workspace.service_name')"
          ><el-input v-model="serviceName" /></el-form-item></el-form
      ><template #footer
        ><el-button type="primary" :loading="busy" :disabled="serviceVersionConflict" @click="publishService">{{
          t('model.metric_workspace.service')
        }}</el-button></template
      ></el-dialog
    >
  </div>
</template>
<script setup>
import { computed, reactive, ref, watch } from 'vue';
import { useRouter, useRoute, onBeforeRouteUpdate } from 'vue-router';
import { useI18n } from 'vue-i18n';
import { ElMessage } from 'element-plus';
import { BusinessDomainSelect, buildBusinessDomainOptions, openConsoleRoute, listResourceTreeEngines } from '@common-ui';
import {
  domainAPI,
  logicalTableAPI,
  standardMetricAPI,
  metricImplementationAPI as api,
  metricServiceAPI,
} from '../api/model';
import { useAuthStore } from '../store/auth';
import { navigateModelRoute } from '../utils/moduleNavigation';
import { useUnsavedChanges } from '../composables/useUnsavedChanges';
import { getModelErrorMessage } from '../utils/apiError';
import { buildMetricImplementationListRouteQuery, resolveMetricImplementationListRouteState } from '../utils/routeState';
import MetricSourceSummary from '../components/MetricSourceSummary.vue';
import MetricRevisionServices from '../components/MetricRevisionServices.vue';
const router = useRouter(),
  route = useRoute(),
  auth = useAuthStore(),
  { t } = useI18n();
const items = ref([]),
  item = ref(null),
  tables = ref([]),
  domains = ref([]),
  definitions = ref([]),
  definitionRevisions = ref([]),
  relations = ref([]),
  choices = ref([]);
const sourceEngines = ref([]), sourceEngineError = ref(false);
const loading = ref(false),
  busy = ref(false),
  error = ref(''),
  domainLoadError = ref(false),
  selectedRevision = ref(null),
  editingNew = ref(false),
  serviceDialog = ref(false),
  serviceName = ref('');
const serviceResultKind = ref('');
const identity = reactive({
  fact_table_id: null,
  metric_definition_id: null,
  name: '',
  note: '',
});
const blank = () => ({
  operation: 'count_distinct',
  metric_definition_revision_id: null,
  subject_field_id: null,
  subject_relation_id: null,
  subject_label: '',
  include_details: false,
  distinct: '',
  time: '',
  group: '',
  measure: '',
  filters: [],
});
const form = reactive(blank());
const isNew = computed(() => route.params.id === 'new');
const revision = computed(() =>
  item.value?.revisions.find((v) => v.id === selectedRevision.value),
);
const editable = computed(
  () =>
    editingNew.value || !revision.value || revision.value.status === 'draft',
);
const can = (action) =>
  auth.hasPermission(`model.metric_implementation.${action}`);
const sourceDomainId = computed(() => resolveMetricImplementationListRouteState(route.query).sourceDomainId);
const filteredItems = computed(() => sourceDomainId.value
  ? items.value.filter(row => Number(tables.value.find(table => table.id === row.fact_table_id)?.domain_id) === sourceDomainId.value)
  : items.value);
const domainLabel = id => domains.value.find(domain => domain.id === Number(id))?.path.join(' / ')
  || t('model.metric_workspace.domain_unavailable');
const sourceDomainLabel = row => {
  const table = tables.value.find(value => value.id === row.fact_table_id);
  if (!table) return t('model.metric_workspace.domain_unavailable');
  return table.domain_id ? domainLabel(table.domain_id) : t('model.metric_workspace.unassigned_source');
};
const definitionOwnershipLabel = row => {
  const definition = definitions.value.find(value => value.id === row.metric_definition_id);
  if (!definition) return t('model.metric_workspace.domain_unavailable');
  if (definition.scope_type === 'platform') return t('model.metric_workspace.platform_scope');
  if (definition.scope_type === 'tenant_common') return t('model.metric_workspace.tenant_common_scope');
  return definition.scope_type === 'domain' && definition.owner_domain_id
    ? domainLabel(definition.owner_domain_id)
    : t('model.metric_workspace.domain_unavailable');
};
const { isDirty, markSaved, confirmDiscardChanges } = useUnsavedChanges({
  state: computed(() => (isNew.value ? identity : form)),
  t,
});
const go = (id) =>
  navigateModelRoute(router, {
    path: `/metric-implementations${id ? `/${id}` : ''}`,
    query: buildMetricImplementationListRouteQuery({
      sourceDomainId: sourceDomainId.value,
    }),
  });
const changeSourceDomain = value => navigateModelRoute(router, {
  path: '/metric-implementations',
  query: buildMetricImplementationListRouteQuery({ sourceDomainId: Number(value) || null }),
});
const refKey = (v) => `${v.relation_id}:${v.field_id}`;
const fieldRef = (key) => {
  const [relation_id, field_id] = key.split(':').map(Number);
  if (
    !Number.isInteger(relation_id) ||
    relation_id < 0 ||
    !Number.isInteger(field_id) ||
    field_id <= 0
  )
    throw Error(t('model.metric_workspace.required'));
  return { relation_id, field_id };
};
function setRevision(id) {
  selectedRevision.value = id;
  editingNew.value = false;
  Object.assign(form, blank());
  const r = revision.value;
  if (r) {
    const c = r.contract;
    Object.assign(form, {
      metric_definition_revision_id: r.metric_definition_revision_id,
      operation: c.operation,
      include_details: Boolean(c.include_details),
      subject_field_id: c.subject?.field_id || null,
      subject_relation_id: c.subject_relation_id || null,
      subject_label: c.subject_label ? refKey(c.subject_label) : '',
      distinct: c.distinct ? refKey(c.distinct) : '',
      time: c.time ? refKey(c.time) : '',
      group: c.group ? refKey(c.group) : '',
      measure: c.measure ? refKey(c.measure) : '',
      filters: (c.filters || []).map((v) => ({
        field: refKey(v.field),
        value: v.value,
      })),
    });
  }
  serviceName.value = item.value
    ? `metric_${item.value.id}_r${r?.revision_no || 1}`
    : '';
  markSaved();
}
const selectRevision = async (id) => {
  if (await confirmDiscardChanges()) setRevision(id);
};
const startDraft = async () => {
  if (await confirmDiscardChanges()) {
    const draft = item.value.revisions.find((v) => v.status === 'draft');
    if (draft) setRevision(draft.id);
    else {
      editingNew.value = true;
      markSaved();
    }
  }
};
const selectDefinition = (id) => {
  identity.name =
    definitions.value.find((v) => v.id === id)?.current_revision?.name || '';
};
const openDefinition = () =>
  openConsoleRoute(`/standard/metrics/${item.value.metric_definition_id}`);
let generation = 0;
async function load() {
  const request = ++generation;
  loading.value = true;
  error.value = '';
  item.value = null;
  sourceEngines.value = [];
  sourceEngineError.value = false;
  if (!can('read')) {
    error.value = t('model.metric_workspace.unavailable');
    loading.value = false;
    return;
  }
  try {
    if (!route.params.id) {
      const state = resolveMetricImplementationListRouteState(route.query);
      if (state.changed) {
        await navigateModelRoute(router, { path: '/metric-implementations', query: state.query }, { history: 'replace' });
        return;
      }
    }
    const [allTables, defs, domainResult] = await Promise.all([
      logicalTableAPI.listAll(),
      standardMetricAPI.listAll(),
      domainAPI.list().then(value => ({ value }), () => ({ error: true })),
    ]);
    if (request !== generation) return;
    tables.value = allTables;
    definitions.value = defs;
    domainLoadError.value = Boolean(domainResult.error);
    domains.value = domainResult.error ? [] : buildBusinessDomainOptions(domainResult.value || []);
    if (!route.params.id) {
      const { factTableId } = resolveMetricImplementationListRouteState(route.query);
      items.value = await api.list(factTableId ? { fact_table_id: factTableId } : {});
      markSaved();
      return;
    }
    if (isNew.value) {
      Object.assign(identity, {
        fact_table_id: Number(route.query.fact_table_id) || null,
        metric_definition_id: null,
        name: '',
        note: '',
      });
      markSaved();
      return;
    }
    const current = await api.get(Number(route.params.id));
    if (request !== generation) return;
    const [factFields, links, revisions] = await Promise.all([
      logicalTableAPI.getFields(current.fact_table_id),
      logicalTableAPI.listDimensionRelations(current.fact_table_id),
      standardMetricAPI.revisions(current.metric_definition_id),
    ]);
    const related = await Promise.all(
      links.map(async (link) => ({
        link,
        fields: await logicalTableAPI.getFields(link.target_table),
      })),
    );
    if (request !== generation) return;
    item.value = current;
    void loadSourceEngines(request);
    relations.value = links;
    definitionRevisions.value = revisions.filter(
      (v) => v.status === 'published',
    );
    const convert = (fields, rid, label) =>
      fields.map((field) => ({
        field_id: field.id,
        relation_id: rid,
        data_type: field.data_type,
        key: `${rid}:${field.id}`,
        label: `${label} · ${field.name} (${field.column_name})`,
      }));
    choices.value = [
      ...convert(
        factFields,
        0,
        tables.value.find((v) => v.id === current.fact_table_id)?.name || '',
      ),
      ...related.flatMap(({ link, fields }) =>
        convert(
          fields,
          link.id,
          tables.value.find((v) => v.id === link.target_table)?.name || '',
        ),
      ),
    ];
    const requested = Number(route.query.revision_id);
    if (requested && !current.revisions.some((v) => v.id === requested))
      throw Error(t('model.metric_workspace.unavailable'));
    setRevision(requested || current.revisions[0]?.id || null);
  } catch (err) {
    if (request === generation)
      error.value = getModelErrorMessage(
        err,
        t,
        'model.metric_workspace.load_failed',
      );
  } finally {
    if (request === generation) loading.value = false;
  }
}
async function loadSourceEngines(request) {
  if (!auth.hasPermission('meta.catalog.read')) return;
  try {
    const engines = await listResourceTreeEngines('/api/v1/meta');
    if (request === generation) sourceEngines.value = engines;
  } catch {
    if (request === generation) sourceEngineError.value = true;
  }
}
async function action(fn) {
  busy.value = true;
  try {
    await fn();
  } catch (err) {
    ElMessage.error(
      getModelErrorMessage(err, t, 'model.metric_workspace.action_failed'),
    );
  } finally {
    busy.value = false;
  }
}
const create = () =>
  action(async () => {
    if (
      !identity.fact_table_id ||
      !identity.metric_definition_id ||
      !identity.name.trim()
    )
      throw Error(t('model.metric_workspace.required'));
    const created = await api.create({ ...identity });
    markSaved();
    await go(created.id);
  });
const save = () =>
  action(async () => {
    if (!form.metric_definition_revision_id || (form.operation === 'sum_decimal_by_group'
      ? (!form.group || !form.measure)
      : (!form.subject_field_id || !form.subject_relation_id || !form.distinct || !form.time)))
      throw Error(t('model.metric_workspace.required'));
    const contract = form.operation === 'sum_decimal_by_group' ? {
      operation: form.operation,
      group: fieldRef(form.group),
      measure: fieldRef(form.measure),
    } : {
      ...(form.include_details ? { include_details: true } : {}),
      operation: form.operation,
      subject: { relation_id: 0, field_id: form.subject_field_id },
      subject_relation_id: form.subject_relation_id,
      ...(form.subject_label ? { subject_label: fieldRef(form.subject_label) } : {}),
      distinct: fieldRef(form.distinct),
      time: fieldRef(form.time),
      filters: form.filters.map((v) => ({
        field: fieldRef(v.field),
        value: v.value,
      })),
    };
    item.value = await api.saveDraft(item.value.id, {
      version: item.value.version,
      metric_definition_revision_id: form.metric_definition_revision_id,
      contract,
    });
    setRevision(item.value.revisions.find((v) => v.status === 'draft').id);
    ElMessage.success(t('model.metric_workspace.saved'));
  });
const publishRevision = () =>
  action(async () => {
    const id = revision.value.id;
    item.value = await api.publish(
      item.value.id,
      id,
      item.value.version,
    );
    setRevision(id);
  });
const withdrawDialog = ref(false), withdrawLoading = ref(false),
  withdrawCount = ref(null), withdrawError = ref('withdraw_unavailable'), withdrawRefresh = ref(0);
watch(() => [item.value?.id, item.value?.version, revision.value?.id, editingNew.value], () => {
  withdrawDialog.value = false;
}, { flush: 'sync' });
watch([withdrawDialog, withdrawRefresh, () => auth.hasPermission('service.definition.read')], async (_values, _previous, onCleanup) => {
  let cancelled = false;
  onCleanup(() => { cancelled = true; });
  withdrawCount.value = null;
  withdrawLoading.value = false;
  withdrawError.value = 'withdraw_unavailable';
  if (!withdrawDialog.value) return;
  if (!auth.hasPermission('service.definition.read')) {
    withdrawError.value = 'withdraw_forbidden';
    return;
  }
  withdrawLoading.value = true;
  try {
    const result = await metricServiceAPI.references(item.value.id, revision.value.id, 1, 1);
    if (!cancelled) withdrawCount.value = result.total;
  } catch (err) {
    if (!cancelled) withdrawError.value = err.response?.status === 403 ? 'withdraw_forbidden' : 'withdraw_unavailable';
  } finally {
    if (!cancelled) withdrawLoading.value = false;
  }
});
const withdrawRevision = () => {
  if (!withdrawDialog.value || withdrawLoading.value || busy.value || !can('offline') || revision.value?.status !== 'published' || editingNew.value) return;
  const id = revision.value.id, implementationID = item.value.id, version = item.value.version, request = generation;
  return action(async () => {
    const updated = await api.withdraw(implementationID, id, version);
    // A navigation during the request must not replace another implementation's state.
    if (request !== generation || item.value?.id !== implementationID || revision.value?.id !== id) return;
    withdrawDialog.value = false;
    item.value = updated;
    setRevision(id);
  });
};
const remove = () =>
  action(async () => {
    await api.delete(item.value.id, item.value.version);
    markSaved();
    await go();
  });
const serviceVersionConflict = ref(false);
const serviceMode = ref('create'),
  existingServiceID = ref(null),
  existingServices = ref([]),
  serviceSearchLoading = ref(false);
let serviceSearchGeneration = 0;
async function searchServices(search = '') {
  const request = ++serviceSearchGeneration;
  serviceSearchLoading.value = true;
  try {
    const response = await metricServiceAPI.search(search);
    if (request === serviceSearchGeneration)
      existingServices.value = response.data.filter(
        (v) => ['sql', 'table', 'analytical'].includes(v.config_type),
      );
  } catch (err) {
    if (request === serviceSearchGeneration) {
      existingServices.value = [];
      ElMessage.error(
        getModelErrorMessage(err, t, 'model.metric_workspace.load_failed'),
      );
    }
  } finally {
    if (request === serviceSearchGeneration) serviceSearchLoading.value = false;
  }
}
const openServiceDialog = () =>
  action(async () => {
    serviceMode.value = auth.hasPermission('service.definition.create')
      ? 'create'
      : 'rebind';
    existingServiceID.value = null;
    serviceVersionConflict.value = false;
    serviceResultKind.value = '';
    serviceDialog.value = true;
    if (
      auth.hasPermission('service.definition.update') &&
      auth.hasPermission('service.definition.read')
    )
      await searchServices();
  });
const operationChanged = () => {
  if (form.operation === 'directional_overlap') { form.filters = []; form.include_details = false; }
  if (form.operation === 'sum_decimal_by_group') { form.filters = []; form.include_details = false; }
};
const reloadServiceTarget = () => action(async () => {
  const target = await metricServiceAPI.get(existingServiceID.value);
  existingServices.value = [target, ...existingServices.value.filter(v => v.id !== target.id)];
  serviceVersionConflict.value = false;
});
const publishService = () =>
  action(async () => {
    if (serviceVersionConflict.value) return;
    const metric_source = {
      implementation_id: item.value.id,
      revision_id: revision.value.id,
      ...(serviceResultKind.value ? { result_kind: serviceResultKind.value } : {}),
    };
    let service;
    if (serviceMode.value === 'rebind') {
      const target = existingServices.value.find(
        (v) => v.id === existingServiceID.value,
      );
      if (!target) {
        ElMessage.error(t('model.metric_workspace.required'));
        return;
      }
      if (!Number.isSafeInteger(target.version) || target.version <= 0) {
        ElMessage.error(t('model.metric_workspace.rebind_version_unavailable'));
        return;
      }
      try {
        service = await metricServiceAPI.rebind(target.id, {
          metric_source,
          version: target.version,
        });
      } catch (err) {
        if (err.response?.data?.error_code !== 'resource_version_conflict') throw err;
        serviceVersionConflict.value = true;
        return;
      }
    } else {
      if (!serviceName.value.trim())
        throw Error(t('model.metric_workspace.required'));
      service = await metricServiceAPI.create({
        service_name: serviceName.value.trim(),
        title: serviceResultKind.value ? `${item.value.name} · ${t('model.metric_workspace.detail_result')}` : item.value.name,
        config_type: 'analytical',
        metric_source,
        public_access: false,
        max_features:
          revision.value.contract.operation === 'directional_overlap'
            ? 240
            : 120,
      });
    }
    serviceDialog.value = false;
    await openConsoleRoute(`/service/query-services/${service.id}`);
  });
onBeforeRouteUpdate((to, from) =>
  String(to.params.id) === String(from.params.id) &&
  to.fullPath !== from.fullPath
    ? confirmDiscardChanges()
    : true,
);
watch(
  () => [route.params.id, route.query.fact_table_id, route.query.source_domain_id, route.query.revision_id],
  load,
  { immediate: true },
);
</script>
<style scoped>
.metric-workspace {
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  color: var(--addp-text-primary);
}
.workspace-header {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  justify-content: space-between;
}
.workspace-header h2 {
  margin: 0;
}
.metric-domain-filter {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}
.metric-domain-filter .el-select {
  width: min(320px, 100%);
}
.publication-source {
  margin-bottom: 20px;
}
.field-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.el-select {
  width: 100%;
}
.el-tag {
  margin: 12px 8px 12px 0;
}
.filters {
  width: 100%;
  display: grid;
  gap: 8px;
}
.filter-row {
  display: grid;
  grid-template-columns: 2fr 1fr auto;
  gap: 8px;
}
@media (max-width: 720px) {
  .metric-workspace {
    padding: 12px;
  }
  .field-grid {
    grid-template-columns: 1fr;
  }
  .filter-row {
    grid-template-columns: 1fr;
  }
}
</style>
