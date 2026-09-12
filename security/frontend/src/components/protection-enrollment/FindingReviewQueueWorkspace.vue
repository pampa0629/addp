<template>
  <section>
    <div class="review-queue-intro">
      <div>
        <strong>{{ t('security.reviewQueue.title') }}</strong>
        <p>{{ t('security.reviewQueue.description') }}</p>
      </div>
      <el-tag type="warning" effect="plain">{{ t('security.reviewQueue.pendingTotal', { count: total }) }}</el-tag>
    </div>

    <div class="review-queue-filters">
      <el-select
        v-model="typeIdModel"
        clearable
        :placeholder="t('security.reviewQueue.allSensitiveTypes')"
        @change="emit('filterChange')"
      >
        <el-option v-for="item in sensitiveTypes" :key="item.id" :label="item.name" :value="String(item.id)" />
      </el-select>
      <el-select
        v-model="detectorVersionModel"
        clearable
        filterable
        :placeholder="t('security.reviewQueue.allRecognitionMethods')"
        @change="emit('filterChange')"
      >
        <el-option v-for="item in capabilities" :key="item.key" :label="capabilityOptionLabel(item)" :value="item.key" />
      </el-select>
      <el-button v-if="typeId || detectorVersion" @click="emit('resetFilters')">
        {{ t('security.reviewQueue.clearFilters') }}
      </el-button>
    </div>

    <el-card class="enrollment-card review-queue-card" shadow="never">
      <el-table v-loading="loading" :data="rows" row-key="id">
        <el-table-column :label="t('security.reviewQueue.resource')" min-width="260">
          <template #default="{ row }">
            <EnrollmentResourceIdentity
              :row="row"
              :resource-name="resourceName"
              :resource-path="resourcePath"
              :item-type-label="itemTypeLabel"
              :engine-label="engineLabel"
              @open="emit('openResource', row)"
            />
          </template>
        </el-table-column>
        <el-table-column :label="t('security.reviewQueue.candidate')" min-width="250">
          <template #default="{ row }">
            <div class="queue-candidate">
              <strong>{{ row.component_key }}</strong>
              <span>{{ typeName(row.sensitive_data_type_id) }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('security.reviewQueue.recognition')" min-width="270">
          <template #default="{ row }">
            <div class="queue-recognition">
              <span>{{ capabilityName(row) }}</span>
              <code>{{ row.detector_version }}</code>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('security.finding.evidence')" min-width="250">
          <template #default="{ row }">
            <div class="queue-evidence">
              <span>{{ evidenceDescription(row) }}</span>
              <small>{{ t('security.finding.confidenceValue', { value: confidenceLabel(row.confidence) }) }} · {{ formatDateTime(row.observed_at) }}</small>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('security.common.actions')" width="210" fixed="right">
          <template #default="{ row }">
            <div class="queue-actions">
              <el-button v-if="canReview" link type="primary" @click="emit('review', row, 'confirm')">{{ t('security.finding.review') }}</el-button>
              <el-button v-if="canReview" link type="danger" @click="emit('review', row, 'reject')">{{ t('security.finding.markFalsePositive') }}</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && rows.length === 0" :description="t('security.reviewQueue.empty')" />

      <div v-if="total > pageSize" class="pagination">
        <el-pagination
          v-model:current-page="pageModel"
          v-model:page-size="pageSizeModel"
          background
          layout="total, sizes, prev, pager, next"
          :page-sizes="[20, 50, 100]"
          :total="total"
          @change="emit('pageChange')"
        />
      </div>
    </el-card>
  </section>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import EnrollmentResourceIdentity from './EnrollmentResourceIdentity.vue'

const props = defineProps({
  rows: { type: Array, required: true },
  total: { type: Number, required: true },
  page: { type: Number, required: true },
  pageSize: { type: Number, required: true },
  loading: { type: Boolean, default: false },
  typeId: { type: [String, Number], default: '' },
  detectorVersion: { type: String, default: '' },
  sensitiveTypes: { type: Array, required: true },
  detectorCapabilities: { type: Array, required: true },
  canReview: { type: Boolean, default: false },
  resourceName: { type: Function, required: true },
  resourcePath: { type: Function, required: true },
  itemTypeLabel: { type: Function, required: true },
  engineLabel: { type: Function, required: true },
  typeName: { type: Function, required: true },
  capabilityName: { type: Function, required: true },
  evidenceDescription: { type: Function, required: true },
  confidenceLabel: { type: Function, required: true },
  formatDateTime: { type: Function, required: true }
})

const emit = defineEmits([
  'update:typeId',
  'update:detectorVersion',
  'update:page',
  'update:pageSize',
  'filterChange',
  'resetFilters',
  'pageChange',
  'openResource',
  'review'
])
const { t } = useI18n()

const typeIdModel = computed({
  get: () => props.typeId,
  set: value => emit('update:typeId', value)
})
const detectorVersionModel = computed({
  get: () => props.detectorVersion,
  set: value => emit('update:detectorVersion', value)
})
const pageModel = computed({
  get: () => props.page,
  set: value => emit('update:page', value)
})
const pageSizeModel = computed({
  get: () => props.pageSize,
  set: value => emit('update:pageSize', value)
})
const capabilities = computed(() => {
  const values = new Map(props.detectorCapabilities.map(item => [String(item.key || ''), item]))
  for (const finding of props.rows) {
    const capability = finding?.explanation?.capability
    const key = String(finding?.detector_version || '')
    if (key && !values.has(key)) values.set(key, capability?.key ? capability : { key })
  }
  return [...values.values()].filter(item => item.key).sort((left, right) => String(left.key).localeCompare(String(right.key)))
})

function capabilityOptionLabel(capability) {
  const key = String(capability?.key || '')
  const i18nKey = String(capability?.name_i18n_key || '')
  const translated = i18nKey ? t(i18nKey) : ''
  const name = translated && translated !== i18nKey ? translated : key
  return name === key ? key : `${name}（${key}）`
}
</script>

<style scoped>
.review-queue-intro { display: flex; align-items: center; justify-content: space-between; gap: 20px; margin-bottom: 12px; padding: 13px 15px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-primary); }
.review-queue-intro p { margin: 5px 0 0; color: var(--addp-text-secondary); font-size: 13px; }
.review-queue-filters { display: flex; flex-wrap: wrap; gap: 10px; margin-bottom: 12px; }
.review-queue-filters .el-select { width: min(340px, 100%); }
.enrollment-card { border-color: var(--addp-border-color); background: var(--addp-bg-primary); }
.review-queue-card :deep(.el-card__body) { padding-top: 8px; }
.queue-candidate, .queue-recognition, .queue-evidence { display: flex; min-width: 0; flex-direction: column; align-items: flex-start; gap: 5px; }
.queue-candidate span, .queue-evidence small { color: var(--addp-text-secondary); font-size: 12px; }
.queue-recognition code { max-width: 100%; overflow: hidden; color: var(--addp-text-tertiary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.queue-actions { display: flex; flex-wrap: wrap; gap: 2px; }
.pagination { display: flex; justify-content: flex-end; padding-top: 16px; }
:deep(.el-card__body) { padding: 0; }
:deep(.el-table) { background: var(--addp-bg-primary); }
@media (max-width: 720px) {
  .review-queue-intro { align-items: flex-start; flex-direction: column; }
  .review-queue-filters .el-select { width: 100%; }
}
</style>
