<template>
  <el-card class="access-review-card" shadow="never">
    <template #header>
      <div class="access-review-card__header">
        <div>
          <strong>{{ t('security.accessRequest.reviewTitle') }}</strong>
          <p>{{ t('security.accessRequest.reviewDescription') }}</p>
        </div>
        <div class="access-review-card__scope">
          <el-radio-group v-model="scopeModel" size="small" @change="emit('scopeChange')">
            <el-radio-button value="pending">{{ t('security.accessRequest.scopes.pending') }}</el-radio-button>
            <el-radio-button value="history">{{ t('security.accessRequest.scopes.history') }}</el-radio-button>
          </el-radio-group>
          <el-tag v-if="total > 0" :type="scope === 'pending' ? 'warning' : 'info'" effect="plain">{{ total }}</el-tag>
        </div>
      </div>
    </template>

    <div class="access-review-filters">
      <el-input
        v-model.trim="resourceSearch"
        clearable
        :placeholder="t('security.accessRequest.filters.resourcePlaceholder')"
        @keyup.enter="emit('applyFilters')"
      />
      <el-input
        v-model.trim="requesterSearch"
        clearable
        :placeholder="t('security.accessRequest.filters.requesterPlaceholder')"
        @keyup.enter="emit('applyFilters')"
      />
      <el-select
        v-if="scope === 'history'"
        v-model="state"
        clearable
        :placeholder="t('security.accessRequest.filters.allStates')"
      >
        <el-option v-for="value in ['approved', 'rejected', 'expired']" :key="value" :label="t(`security.accessRequest.states.${value}`)" :value="value" />
      </el-select>
      <el-select
        v-if="scope === 'history'"
        v-model="authorizationState"
        clearable
        :placeholder="t('security.accessRequest.filters.allAuthorizationStates')"
      >
        <el-option v-for="value in ['active', 'expired', 'revoked', 'superseded']" :key="value" :label="t(`security.accessRequest.authorizationStates.${value}`)" :value="value" />
      </el-select>
      <el-date-picker
        v-model="createdRangeModel"
        type="datetimerange"
        unlink-panels
        :range-separator="t('security.accessRequest.filters.to')"
        :start-placeholder="t('security.accessRequest.filters.createdFrom')"
        :end-placeholder="t('security.accessRequest.filters.createdTo')"
      />
      <el-button type="primary" @click="emit('applyFilters')">{{ t('security.accessRequest.filters.search') }}</el-button>
      <el-button v-if="hasFilters" @click="emit('resetFilters')">{{ t('security.accessRequest.filters.reset') }}</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" size="small">
      <el-table-column :label="t('security.accessRequest.resourceField')" min-width="260">
        <template #default="{ row }"><strong>{{ row.target_full_name }}</strong><br><span>{{ row.component?.key }}</span></template>
      </el-table-column>
      <el-table-column :label="t('security.accessRequest.requester')" min-width="150">
        <template #default="{ row }">
          <div class="access-actor">
            <strong>{{ row.requester?.display_name }}</strong>
            <span>{{ releaseActorLabel(row.requester?.id) }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column :label="t('security.accessRequest.requestedUntil')" width="190">
        <template #default="{ row }">{{ formatDateTime(row.requested_expires_at) }}</template>
      </el-table-column>
      <el-table-column :label="t('security.accessRequest.createdAt')" width="190">
        <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column prop="rationale" :label="t('security.accessRequest.rationale')" min-width="240" show-overflow-tooltip />
      <el-table-column v-if="scope === 'history'" :label="t('security.accessRequest.state')" width="110">
        <template #default="{ row }"><el-tag size="small" :type="requestStateType(row.state)">{{ t(`security.accessRequest.states.${row.state}`) }}</el-tag></template>
      </el-table-column>
      <el-table-column v-if="scope === 'history'" :label="t('security.accessRequest.authorizationState')" min-width="170">
        <template #default="{ row }">
          <div v-if="row.authorization_state" class="access-authorization-state">
            <el-tag size="small" :type="authorizationStateType(row.authorization_state)">
              {{ t(`security.accessRequest.authorizationStates.${row.authorization_state}`) }}
            </el-tag>
            <span v-if="row.authorized_until">
              {{ t('security.accessRequest.authorizedUntil', { time: formatDateTime(row.authorized_until) }) }}
            </span>
            <el-button
              v-if="canReadExemptions && row.enrollment_id && row.exemption_id"
              class="access-authorization-link"
              link
              type="primary"
              size="small"
              @click="emit('openAuthorization', row)"
            >
              {{ t('security.accessRequest.viewAuthorization') }}
            </el-button>
          </div>
          <span v-else>{{ t('security.accessRequest.authorizationStates.not_granted') }}</span>
        </template>
      </el-table-column>
      <el-table-column v-if="scope === 'history'" :label="t('security.accessRequest.reviewer')" width="130">
        <template #default="{ row }">
          <div v-if="row.reviewer" class="access-actor">
            <strong>{{ row.reviewer.display_name }}</strong>
            <span>{{ releaseActorLabel(row.reviewer.id) }}</span>
          </div>
          <span v-else>{{ t('security.common.notAvailable') }}</span>
        </template>
      </el-table-column>
      <el-table-column v-if="scope === 'history'" :label="t('security.accessRequest.processedAt')" width="190">
        <template #default="{ row }">{{ formatDateTime(row.decided_at || row.requested_expires_at) }}</template>
      </el-table-column>
      <el-table-column v-if="scope === 'history'" prop="decision_rationale" :label="t('security.accessRequest.decisionRationaleLabel')" min-width="220" show-overflow-tooltip>
        <template #default="{ row }">{{ row.decision_rationale || t('security.common.notAvailable') }}</template>
      </el-table-column>
      <el-table-column v-if="scope === 'pending'" :label="t('security.common.actions')" width="190" fixed="right">
        <template #default="{ row }">
          <div v-if="row.can_decide" class="access-decision-actions">
            <el-button link type="primary" @click="emit('decide', row, 'approve')">{{ t('security.accessRequest.approve') }}</el-button>
            <el-button link type="danger" @click="emit('decide', row, 'reject')">{{ t('security.accessRequest.reject') }}</el-button>
          </div>
          <div v-else-if="row.decision_unavailable_reason" class="access-decision-unavailable">
            <el-tag size="small" type="info">{{ t(`security.accessRequest.unavailableLabels.${row.decision_unavailable_reason}`) }}</el-tag>
            <span>{{ t(`security.accessRequest.unavailableReasons.${row.decision_unavailable_reason}`) }}</span>
          </div>
        </template>
      </el-table-column>
    </el-table>

    <el-empty v-if="!loading && rows.length === 0" :description="t(`security.accessRequest.emptyStates.${scope}`)" :image-size="48" />
    <div v-if="total > pageSize" class="pagination">
      <el-pagination
        v-model:current-page="pageModel"
        v-model:page-size="pageSizeModel"
        background
        layout="total, sizes, prev, pager, next"
        :page-sizes="[10, 20, 50, 100]"
        :total="total"
        @change="emit('pageChange')"
      />
    </div>
  </el-card>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  rows: { type: Array, required: true },
  total: { type: Number, required: true },
  page: { type: Number, required: true },
  pageSize: { type: Number, required: true },
  loading: { type: Boolean, default: false },
  scope: { type: String, required: true },
  filters: { type: Object, required: true },
  createdRange: { type: Array, required: true },
  canReadExemptions: { type: Boolean, default: false },
  releaseActorLabel: { type: Function, required: true },
  formatDateTime: { type: Function, required: true }
})

const emit = defineEmits([
  'update:scope',
  'update:filters',
  'update:createdRange',
  'update:page',
  'update:pageSize',
  'scopeChange',
  'applyFilters',
  'resetFilters',
  'pageChange',
  'openAuthorization',
  'decide'
])
const { t } = useI18n()

const scopeModel = computed({
  get: () => props.scope,
  set: value => emit('update:scope', value)
})
const createdRangeModel = computed({
  get: () => props.createdRange,
  set: value => emit('update:createdRange', value)
})
const pageModel = computed({
  get: () => props.page,
  set: value => emit('update:page', value)
})
const pageSizeModel = computed({
  get: () => props.pageSize,
  set: value => emit('update:pageSize', value)
})

function filterModel(key) {
  return computed({
    get: () => props.filters[key],
    set: value => emit('update:filters', { ...props.filters, [key]: value })
  })
}

const resourceSearch = filterModel('resourceSearch')
const requesterSearch = filterModel('requesterSearch')
const state = filterModel('state')
const authorizationState = filterModel('authorizationState')
const hasFilters = computed(() => Boolean(
  resourceSearch.value ||
  requesterSearch.value ||
  state.value ||
  authorizationState.value ||
  props.createdRange.length
))

function requestStateType(value) {
  if (value === 'approved') return 'success'
  if (value === 'rejected') return 'danger'
  return 'info'
}

function authorizationStateType(value) {
  if (value === 'active') return 'success'
  if (value === 'revoked') return 'danger'
  return 'info'
}
</script>

<style scoped>
.access-review-card { margin-bottom: 12px; border-color: var(--addp-border-color); background: var(--addp-bg-primary); }
.access-review-card__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.access-review-card__header p { margin: 5px 0 0; color: var(--addp-text-secondary); font-size: 13px; }
.access-review-card__scope { display: flex; align-items: center; gap: 10px; }
.access-review-filters { display: flex; flex-wrap: wrap; gap: 10px; padding: 12px 14px; border-bottom: 1px solid var(--addp-border-color); }
.access-review-filters > .el-input { width: min(240px, 100%); }
.access-review-filters > .el-select { width: min(180px, 100%); }
.access-review-filters > :deep(.el-date-editor) { width: min(360px, 100%); }
.access-actor { display: flex; flex-direction: column; gap: 2px; }
.access-actor span { color: var(--addp-text-secondary); font-size: 12px; }
.access-authorization-state { display: flex; flex-direction: column; align-items: flex-start; gap: 4px; }
.access-authorization-state span { color: var(--addp-text-secondary); font-size: 12px; }
.access-authorization-link { height: auto; padding: 0; }
.access-decision-actions { display: flex; align-items: center; gap: 4px; }
.access-decision-unavailable { display: flex; flex-direction: column; align-items: flex-start; gap: 4px; color: var(--addp-text-secondary); font-size: 12px; }
.pagination { display: flex; justify-content: flex-end; padding-top: 16px; }
:deep(.el-card__body) { padding: 0; }
:deep(.el-table) { background: var(--addp-bg-primary); }
@media (max-width: 720px) {
  .access-review-card__header { flex-direction: column; }
  .access-review-filters > .el-input, .access-review-filters > .el-select, .access-review-filters > :deep(.el-date-editor) { width: 100%; }
}
</style>
