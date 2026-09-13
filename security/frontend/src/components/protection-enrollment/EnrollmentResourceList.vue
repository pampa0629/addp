<template>
  <section>
    <div class="list-scope-bar">
      <el-radio-group v-model="scopeModel" size="small" @change="emit('scopeChange')">
        <el-radio-button value="current">{{ t('security.enrollment.listScopes.current') }}</el-radio-button>
        <el-radio-button value="released">{{ t('security.enrollment.listScopes.released') }}</el-radio-button>
        <el-radio-button value="all">{{ t('security.enrollment.listScopes.all') }}</el-radio-button>
      </el-radio-group>
    </div>

    <el-card class="enrollment-card" shadow="never">
      <el-table v-loading="loading" :data="presentedRows" row-key="id">
        <el-table-column :label="t('security.enrollment.resource')" min-width="320">
          <template #default="{ row }">
            <EnrollmentResourceIdentity
              :identity="row.identity"
              @open="emit('open', $event)"
            />
          </template>
        </el-table-column>

        <el-table-column :label="t('security.enrollment.state')" width="190">
          <template #default="{ row }">
            <div class="state-cell">
              <el-tag :type="row.state.type">{{ row.state.label }}</el-tag>
              <span>{{ row.state.description }}</span>
            </div>
          </template>
        </el-table-column>

        <el-table-column :label="t('security.enrollment.progress')" min-width="430">
          <template #default="{ row }">
            <div class="owner-grid">
              <div v-for="owner in row.owners" :key="owner.key" class="owner-item">
                <span class="owner-name">{{ owner.label }}</span>
                <el-tag size="small" :type="owner.state.type">
                  {{ owner.state.label }}
                </el-tag>
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column :label="scope === 'released' ? t('security.enrollment.releaseCompletedAt') : t('security.enrollment.discovery')" width="210">
          <template #default="{ row }">
            <span v-if="scope === 'released'" class="release-time">{{ row.releasedAt }}</span>
            <div v-else class="discovery-cell">
              <el-tag size="small" :type="row.discovery.type">{{ row.discovery.label }}</el-tag>
              <span>{{ row.discovery.observedAt }}</span>
            </div>
          </template>
        </el-table-column>

        <el-table-column :label="t('security.common.actions')" width="210" fixed="right">
          <template #default="{ row }">
            <div class="row-actions">
              <el-button
                v-if="canCreate && row.canReEnroll"
                link
                type="primary"
                @click="emit('reEnroll', row.enrollment)"
              >
                {{ t('security.enrollment.reEnroll') }}
              </el-button>
              <el-button link type="primary" @click="emit('open', row.enrollment)">{{ t('security.enrollment.viewDetails') }}</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && rows.length === 0" :description="emptyDescription">
        <el-button v-if="canCreate && scope === 'current'" type="primary" @click="emit('create')">{{ t('security.enrollment.create') }}</el-button>
      </el-empty>

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
  scope: { type: String, required: true },
  canCreate: { type: Boolean, default: false },
  presentation: { type: Object, required: true }
})

const emit = defineEmits([
  'update:scope',
  'update:page',
  'update:pageSize',
  'scopeChange',
  'pageChange',
  'open',
  'reEnroll',
  'create'
])
const { t } = useI18n()

const scopeModel = computed({
  get: () => props.scope,
  set: value => emit('update:scope', value)
})
const pageModel = computed({
  get: () => props.page,
  set: value => emit('update:page', value)
})
const pageSizeModel = computed({
  get: () => props.pageSize,
  set: value => emit('update:pageSize', value)
})
const presentedRows = computed(() => props.rows.map(row => props.presentation.row(row)))
const emptyDescription = computed(() => t(`security.enrollment.emptyStates.${props.scope}`))
</script>

<style scoped>
.list-scope-bar { display: flex; align-items: center; margin-bottom: 12px; }
.enrollment-card { border-color: var(--addp-border-color); background: var(--addp-bg-primary); }
.state-cell { display: flex; flex-direction: column; align-items: flex-start; gap: 7px; }
.state-cell span { color: var(--addp-text-secondary); font-size: 12px; line-height: 1.45; }
.discovery-cell { display: flex; flex-direction: column; align-items: flex-start; gap: 6px; }
.discovery-cell span { color: var(--addp-text-tertiary); font-size: 12px; }
.owner-grid { display: grid; grid-template-columns: repeat(2, minmax(170px, 1fr)); gap: 8px 16px; }
.owner-item { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.owner-name { color: var(--addp-text-secondary); font-size: 13px; }
.pagination { display: flex; justify-content: flex-end; padding-top: 16px; }
:deep(.el-card__body) { padding: 0; }
:deep(.el-table) { background: var(--addp-bg-primary); }
@media (max-width: 1280px) {
  .owner-grid { grid-template-columns: 1fr; }
}
</style>
