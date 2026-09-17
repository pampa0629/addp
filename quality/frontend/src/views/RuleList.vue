<template>
  <div class="rule-list">
    <div class="page-header">
      <h2>{{ t("quality.rule.title") }}</h2>
      <el-button
        v-if="can('quality.rule.create')"
        type="primary"
        @click="navigate('create')"
        >{{ t("quality.rule.create") }}</el-button
      >
    </div>
    <el-form inline>
      <el-form-item :label="t('quality.domain.owner')">
        <DomainOwnershipSelect v-model="ownerDomainID" filter :can-read="can('standard.domain.read')"
          style="width: 280px" @loaded="domains = $event" @update:model-value="changeDomain" />
      </el-form-item>
    </el-form>
    <el-alert
      v-if="loadError"
      :title="loadError"
      type="error"
      :closable="false"
    />
    <el-table v-loading="loading" :data="rows" border>
      <el-table-column :label="t('quality.domain.owner')" min-width="180">
        <template #default="{ row }">{{ domainOwnershipLabel(row.owner_domain_id, t, domains) }}</template>
      </el-table-column>
      <el-table-column
        prop="name"
        :label="t('quality.plan.name')"
        min-width="220"
      />
      <el-table-column
        prop="code"
        :label="t('quality.rule.code')"
        min-width="220"
      />
      <el-table-column :label="t('quality.rule.type')" min-width="130"
        ><template #default="{ row }">{{
          t(`quality.plan.type_${row.type}`)
        }}</template></el-table-column
      >
      <el-table-column :label="t('quality.rule.revision')" width="90"
        ><template #default="{ row }"
          >R{{ row.revision_no }}</template
        ></el-table-column
      >
      <el-table-column :label="t('quality.rule.source')" min-width="120">
        <template #default="{ row }">{{ t(row.source ? 'quality.rule.standardSource' : 'quality.rule.manualSource') }}</template>
      </el-table-column>
      <el-table-column :label="t('quality.rule.plans')" width="130"
        ><template #default="{ row }"
          ><el-button
            v-if="can('quality.plan.read')"
            link
            @click="showPlans(row)"
            >{{ row.plan_count }}</el-button
          ><span v-else>{{ row.plan_count }}</span></template
        ></el-table-column
      >
      <el-table-column :label="t('quality.plan.actions')" width="180"
        ><template #default="{ row }"
          ><el-button
            v-if="can('quality.rule.update')"
            link
            @click="navigate('edit', row.id)"
            >{{ t("quality.plan.edit") }}</el-button
          ><el-button
            v-if="can('quality.rule.delete')"
            link
            type="danger"
            :disabled="row.plan_count > 0 || deletingID === row.id"
            @click="remove(row)"
            >{{ t("quality.plan.delete") }}</el-button
          ></template
        ></el-table-column
      >
    </el-table>
    <el-pagination
      :current-page="page"
      :page-size="pageSize"
      :total="total"
      :page-sizes="[20, 50, 100]"
      layout="total, sizes, prev, pager, next"
      @current-change="changePage"
      @size-change="changeSize"
    />

    <el-dialog
      v-model="visible"
      class="addp-dialog"
      :title="editingID ? t('quality.rule.edit') : t('quality.rule.create')"
      width="min(820px, calc(100vw - 24px))"
      :close-on-click-modal="false"
      :close-on-press-escape="!saving"
      :show-close="!saving"
      @closed="closeRoute"
    >
      <el-form
        ref="formRef"
        :model="form"
        :rules="validation"
        label-position="top"
      >
        <div class="form-grid">
          <el-form-item :label="t('quality.rule.code')" prop="code"
            ><el-input
              v-model="form.code"
              :disabled="!!editingID"
              maxlength="100"
          /></el-form-item>
          <el-form-item :label="t('quality.plan.name')" prop="name"
            ><el-input v-model="form.name" maxlength="200"
          /></el-form-item>
        </div>
        <el-form-item :label="t('quality.plan.description')"
          ><el-input v-model="form.description" type="textarea"
        /></el-form-item>
        <el-form-item :label="t('quality.domain.owner')">
          <DomainOwnershipSelect v-if="visible" v-model="form.owner_domain_id" :can-read="can('standard.domain.read')" />
        </el-form-item>
        <div class="source-bar">
          <el-button @click="openSource">{{
            t("quality.plan.importStandard")
          }}</el-button>
          <el-button v-if="form.source" link @click="form.source = null">{{
            t("quality.plan.detachSource")
          }}</el-button>
        </div>
        <RuleSourceDetails v-if="visible" :source="form.source" :can-read="can('standard.element.read')" @loaded="sourceDetail = $event" />
        <el-form-item :label="t('quality.rule.type')"
          ><el-select
            v-model="form.type"
            :disabled="!!form.source"
            @change="form.params = defaultConstraint(form.type)"
            ><el-option
              v-for="type in PLAN_TYPES"
              :key="type"
              :value="type"
              :label="t(`quality.plan.type_${type}`)" /></el-select
        ></el-form-item>
        <RuleConstraintFields
          :type="form.type"
          :params="form.params"
          :disabled="!!form.source"
          :value-labels="sourceDetail?.code_set_revision?.items || []"
        />
        <el-alert
          v-if="conflict"
          :title="t('quality.rule.conflict')"
          type="warning"
          :closable="false"
        />
      </el-form>
      <template #footer
        ><el-button v-if="conflict" @click="reloadEditor">{{
          t("quality.rule.reload")
        }}</el-button
        ><el-button :disabled="saving" @click="visible = false">{{
          t("quality.plan.cancel")
        }}</el-button
        ><el-button type="primary" :loading="saving" @click="save">{{
          t("quality.plan.save")
        }}</el-button></template
      >
    </el-dialog>

    <el-dialog
      v-model="sourceVisible"
      class="addp-dialog"
      :title="t('quality.plan.importStandard')"
      width="min(640px, calc(100vw - 24px))"
    >
      <el-form label-position="top">
        <el-form-item :label="t('quality.plan.standardElement')"
          ><el-select
            v-model="sourceID"
            filterable
            remote
            remote-show-suffix
            clearable
            :placeholder="t('quality.rule.browseElements')"
            :no-data-text="sourceError ? t('quality.rule.candidatesFailed') : t(sourceKeyword ? 'quality.rule.noMatches' : 'quality.rule.noCandidates')"
            :remote-method="searchElements"
            :loading="sourceLoading"
            @change="selectSource"
            ><el-option
              v-for="e in candidateOptions"
              :key="e.id"
              :value="e.id"
              :label="e.name + ' · ' + e.code + ' · R' + e.revision_no" /></el-select
        ></el-form-item>
        <el-alert v-if="sourceError" :title="t('quality.rule.candidatesFailed')" type="error" :closable="false" />
        <el-button v-if="sourceError" link @click="loadElements(sourcePage)">{{ t('quality.rule.retry') }}</el-button>
        <p v-if="!sourceLoading && !sourceError && !candidates.length" role="status">{{ t(sourceKeyword ? 'quality.rule.noMatches' : 'quality.rule.noCandidates') }}</p>
        <el-pagination :current-page="sourcePage" :page-size="30" :total="sourceTotal" layout="total, prev, pager, next" :disabled="sourceLoading" @current-change="loadElements" />
        <p>{{ t('quality.rule.importableHint') }}</p>
        <RuleSourceDetails v-if="sourceVisible && candidateSource" :source="candidateSource" :can-read="can('standard.element.read')" preview @loaded="candidateDetail = $event" />
        <el-alert v-if="selectedSource && !sourceRules.length" :title="t(emptySourceReason)" type="warning" :closable="false" />
        <el-form-item :label="t('quality.rule.type')"
          ><el-select v-model="sourceKey" :disabled="!sourceRules.length"
            ><el-option
              v-for="r in sourceRules"
              :key="r.rule_key"
              :value="r.rule_key"
              :label="t(`quality.plan.type_${r.type}`)" /></el-select
        ></el-form-item>
      </el-form>
      <template #footer
        ><el-button @click="sourceVisible = false">{{
          t("quality.plan.cancel")
        }}</el-button
        ><el-button
          type="primary"
          :disabled="!sourceKey"
          @click="importSource"
          >{{ t("quality.plan.importStandard") }}</el-button
        ></template
      >
    </el-dialog>

    <el-dialog
      v-model="plansVisible"
      class="addp-dialog"
      :title="t('quality.rule.plans')"
      width="min(760px, calc(100vw - 24px))"
    >
      <el-table v-loading="plansLoading" :data="referencingPlans"
        ><el-table-column
          prop="name"
          :label="t('quality.plan.name')"
        /><el-table-column
          prop="code"
          :label="t('quality.plan.code')"
        /><el-table-column
          v-if="can('quality.plan.update') && can('quality.rule.read')"
          :label="t('quality.plan.actions')"
          ><template #default="{ row }"
            ><el-button link @click="openPlan(row)">{{
              t("quality.plan.edit")
            }}</el-button></template
          ></el-table-column
        ></el-table
      >
      <el-pagination
        :current-page="plansPage"
        :page-size="20"
        :total="plansTotal"
        layout="total, prev, pager, next"
        @current-change="loadPlans"
      />
      <template #footer
        ><el-button @click="plansVisible = false">{{
          t("quality.plan.cancel")
        }}</el-button></template
      >
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { ElMessage, ElMessageBox } from "element-plus";
import { ruleAPI } from "../api/quality";
import { useAuthStore } from "../store/auth";
import { navigateQualityRoute } from "../utils/moduleNavigation";
import {
  PLAN_TYPES,
  defaultConstraint,
  editableConstraint,
  serializeConstraint,
} from "../utils/planContract";
import {
  buildRuleRouteQuery,
  resolveRuleRouteState,
} from "../utils/ruleRouteState";
import RuleConstraintFields from "../components/RuleConstraintFields.vue";
import RuleSourceDetails from "../components/RuleSourceDetails.vue";
import DomainOwnershipSelect from "../components/DomainOwnershipSelect.vue";
import { domainOwnershipLabel } from '../utils/domainOwnership';
const ownerDomainID = ref(null), domains = ref([]);
const changeDomain = () => { page.value = 1; return navigate(); };
const { t } = useI18n(),
  route = useRoute(),
  router = useRouter(),
  auth = useAuthStore();
const can = (permission) => auth.hasPermission(permission);
const rows = ref([]),
  loading = ref(false),
  loadError = ref(""),
  page = ref(1),
  pageSize = ref(20),
  total = ref(0);
const visible = ref(false),
  editingID = ref(null),
  saving = ref(false),
  deletingID = ref(null),
  conflict = ref(false),
  formRef = ref(null);
const blank = () => ({
  code: "",
  owner_domain_id: null,
  name: "",
  description: "",
  type: "not_null",
  params: {},
  source: null,
  version: 0,
});
const form = reactive(blank());
const validation = computed(() => ({
  code: [
    { required: true, message: t("quality.rule.codeRequired") },
    { pattern: /^[a-z][a-z0-9_]*$/, message: t("quality.plan.codeFormat") },
  ],
  name: [{ required: true, message: t("quality.rule.nameRequired") }],
}));
let listSeq = 0,
  editorSeq = 0,
  sourceSeq = 0,
  plansSeq = 0,
  routeSeq = 0,
  disposed = false;
const message = (error) =>
  error.response?.data?.error || t("quality.plan.loadFailed");
const navigate = (mode = "list", ruleID = null, history = "push") =>
  navigateQualityRoute(
    router,
    {
      path: "/rules",
      query: buildRuleRouteQuery({
        mode,
        ruleID,
        page: page.value,
        pageSize: pageSize.value,
        ownerDomainID: ownerDomainID.value,
      }),
    },
    { history },
  );
const load = async () => {
  const seq = ++listSeq;
  loading.value = true;
  loadError.value = "";
  try {
    const result = await ruleAPI.list({
      page: page.value,
      page_size: pageSize.value,
      ...(ownerDomainID.value != null ? { owner_domain_id: ownerDomainID.value } : {}),
    });
    if (seq !== listSeq || disposed) return;
    rows.value = result.data;
    total.value = result.total;
  } catch (error) {
    if (seq === listSeq) loadError.value = message(error);
  } finally {
    if (seq === listSeq) loading.value = false;
  }
};
const restore = async (state) => {
  const seq = ++editorSeq;
  sourceSeq++;
  sourceLoading.value = false;
  candidates.value = [];
  selectedSource.value = null;
  sourceDetail.value = null;
  sourceID.value = null;
  sourceKey.value = null;
  if (state.mode === "list") {
    visible.value = false;
    sourceVisible.value = false;
    return;
  }
  if (
    !can(state.mode === "edit" ? "quality.rule.update" : "quality.rule.create")
  ) {
    await navigate("list", null, "replace");
    return;
  }
  conflict.value = false;
  sourceVisible.value = false;
  if (state.mode === "create") {
    editingID.value = null;
    Object.assign(form, blank());
    visible.value = true;
    return;
  }
  try {
    const rule = await ruleAPI.get(state.ruleID);
    if (seq !== editorSeq || disposed) return;
    editingID.value = rule.id;
    Object.assign(form, rule, {
      owner_domain_id: rule.owner_domain_id ?? null,
      params: editableConstraint(rule.type, rule.params),
      source: rule.source || null,
    });
    visible.value = true;
  } catch (error) {
    ElMessage.error(message(error));
  }
};
const closeRoute = () => {
  if (resolveRuleRouteState(route.query).mode !== "list")
    navigate("list", null, "replace");
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
    await restore(resolveRuleRouteState(route.query));
  } catch {}
};
const save = async () => {
  if (saving.value) return;
  try {
    await formRef.value.validate();
  } catch {
    return;
  }
  if (saving.value) return;
  saving.value = true;
  try {
    const payload = {
      code: form.code.trim(),
      name: form.name.trim(),
      description: form.description.trim(),
      owner_domain_id: form.owner_domain_id ?? null,
      type: form.type,
      params: serializeConstraint(form.type, form.params),
      ...(form.source ? { source: form.source } : {}),
      ...(editingID.value ? { version: form.version } : {}),
    };
    if (editingID.value) await ruleAPI.update(editingID.value, payload);
    else await ruleAPI.create(payload);
    visible.value = false;
    await load();
    ElMessage.success(t("quality.rule.saveSuccess"));
  } catch (error) {
    conflict.value =
      error.response?.data?.error_code === "resource_version_conflict";
    ElMessage.error(message(error));
  } finally {
    saving.value = false;
  }
};
const remove = async (row) => {
  if (deletingID.value) return;
  try {
    await ElMessageBox.confirm(
      t("quality.rule.confirmDelete", { name: row.name }),
      t("quality.plan.delete"),
      {
        customClass: "addp-message-box",
        confirmButtonText: t("quality.plan.delete"),
        cancelButtonText: t("quality.plan.cancel"),
        confirmButtonClass: "el-button--danger",
      },
    );
    deletingID.value = row.id;
    await ruleAPI.delete(row.id, row.version);
    await load();
  } catch (error) {
    if (error !== "cancel" && error !== "close")
      ElMessage.error(message(error));
  } finally {
    deletingID.value = null;
  }
};
const changePage = async (value) => {
  page.value = value;
  await navigate("list", null, "replace");
};
const changeSize = async (value) => {
  pageSize.value = value;
  page.value = 1;
  await navigate("list", null, "replace");
};
const sourceVisible = ref(false),
  sourceID = ref(null),
  sourceKey = ref(null),
  candidates = ref([]),
  sourceLoading = ref(false);
const sourceDetail = ref(null), selectedSource = ref(null), sourcePage = ref(1), sourceTotal = ref(0), sourceKeyword = ref(''), sourceError = ref(false);
const candidateDetail = ref(null);
const candidateSource = computed(() => selectedSource.value ? { element_id: selectedSource.value.id, element_revision_id: selectedSource.value.revision_id } : null);
const emptySourceReason = computed(() => {
  const detail = candidateDetail.value;
  if (!detail) return 'quality.rule.noImportableRules';
  const hasConstraints = detail.nullable === false || detail.length != null || Boolean(detail.format?.trim()) || ['range', 'enumeration'].includes(detail.value_domain_kind);
  return hasConstraints ? 'quality.rule.missingCompiledRules' : 'quality.rule.noValueConstraints';
});
const candidateOptions = computed(() => selectedSource.value && !candidates.value.some(e => e.id === selectedSource.value.id) ? [selectedSource.value, ...candidates.value] : candidates.value);
const sourceRules = computed(
  () =>
    selectedSource.value?.quality_rules.rules.filter((r) => r.enabled) || [],
);
const selectSource = (id) => {
  candidateDetail.value = null;
  selectedSource.value = candidateOptions.value.find(e => e.id === id) || null;
  sourceKey.value = null;
};
const openSource = () => {
  candidateDetail.value = null;
  sourceVisible.value = true;
  sourceID.value = null;
  selectedSource.value = null;
  sourceKey.value = null;
  sourceKeyword.value = '';
  loadElements(1);
};
const searchElements = (keyword) => {
  const normalized = keyword.trim();
  // Element Plus calls the remote method again when reopening the dropdown.
  // An unchanged query must not reset the page selected by the user.
  if (normalized === sourceKeyword.value) return;
  sourceKeyword.value = normalized;
  return loadElements(1);
};
const loadElements = async (requestedPage) => {
  const seq = ++sourceSeq;
  candidates.value = [];
  sourcePage.value = requestedPage;
  sourceError.value = false;
  sourceLoading.value = true;
  try {
    const result = await ruleAPI.listElementCandidates({
      keyword: sourceKeyword.value,
      page: requestedPage,
      page_size: 30,
    });
    if (seq === sourceSeq && !disposed) {
      candidates.value = result.data;
      sourceTotal.value = result.total;
    }
  } catch (error) {
    if (seq === sourceSeq && !disposed) sourceError.value = true;
  } finally {
    if (seq === sourceSeq) sourceLoading.value = false;
  }
};
const importSource = () => {
  const e = selectedSource.value,
    r = sourceRules.value.find((r) => r.rule_key === sourceKey.value);
  if (!e || !r) return;
  form.name ||= e.name;
  form.type = r.type;
  form.params =
    r.type === "not_null"
      ? {}
      : r.type === "allowed_values"
        ? { values: [...r.params.values] }
        : { constraint: JSON.parse(JSON.stringify(r.params)) };
  form.source = {
    element_id: e.id,
    element_revision_id: e.revision_id,
    rule_key: r.rule_key,
  };
  sourceVisible.value = false;
};
const plansVisible = ref(false),
  plansLoading = ref(false),
  referencingPlans = ref([]),
  plansTotal = ref(0),
  plansPage = ref(1),
  referenceID = ref(null);
const loadPlans = async (value = 1) => {
  const seq = ++plansSeq;
  plansPage.value = value;
  plansLoading.value = true;
  try {
    const result = await ruleAPI.plans(referenceID.value, {
      page: value,
      page_size: 20,
    });
    if (seq !== plansSeq || disposed) return;
    referencingPlans.value = result.data;
    plansTotal.value = result.total;
  } catch (error) {
    ElMessage.error(message(error));
  } finally {
    if (seq === plansSeq) plansLoading.value = false;
  }
};
const showPlans = async (row) => {
  referenceID.value = row.id;
  referencingPlans.value = [];
  plansVisible.value = true;
  await loadPlans();
};
const openPlan = (row) =>
  navigateQualityRoute(router, {
    path: "/plans",
    query: { task_id: String(row.id) },
  });
watch(
  () => route.query,
  async (query) => {
    const seq = ++routeSeq;
    const state = resolveRuleRouteState(query);
    if (state.changed) {
      await navigateQualityRoute(
        router,
        { path: "/rules", query: state.query },
        { history: "replace" },
      );
      return;
    }
    page.value = state.page;
    pageSize.value = state.pageSize;
    ownerDomainID.value = state.ownerDomainID;
    await load();
    if (seq !== routeSeq || disposed) return;
    await restore(state);
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  disposed = true;
  routeSeq++;
  listSeq++;
  editorSeq++;
  sourceSeq++;
  plansSeq++;
});
</script>
<style scoped>
.rule-list {
  padding: 20px;
}
.page-header,
.source-bar {
  display: flex;
  align-items: center;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 16px;
}
.page-header h2 {
  margin: 0;
}
.form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 260px), 1fr));
  gap: 16px;
}
.el-pagination {
  margin-top: 16px;
  justify-content: flex-end;
}
.rule-list :deep(.el-select) {
  width: 100%;
}
</style>
