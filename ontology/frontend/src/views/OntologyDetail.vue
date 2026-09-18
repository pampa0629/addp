<template>
  <main class="page">
    <header class="toolbar">
      <el-button @click="go('/ontologies')">
        {{ t('ontology.backList') }}
      </el-button>
      <h2>{{ route.params.ontology_id }}</h2>
      <el-button :loading="loading" :disabled="busy" @click="load">
        {{ t('ontology.refresh') }}
      </el-button>
    </header>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-alert
      v-if="uncertain"
      :title="t('ontology.reconcile')"
      type="warning"
      :closable="false"
    />
    <template v-if="head">
      <el-descriptions border :column="1">
        <el-descriptions-item :label="t('ontology.activeRevision')">
          {{ head.active_revision ?? t('ontology.inactive') }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('ontology.activeGeneration')">
          <span class="identifier">{{ head.active_generation || '—' }}</span>
        </el-descriptions-item>
      </el-descriptions>
      <div class="toolbar">
        <h3>{{ t('ontology.revisions') }}</h3>
        <el-button
          v-if="auth.hasPermission('ontology.revision.update')"
          :disabled="
            busy ||
            loading ||
            uncertain ||
            !latest ||
            ['draft', 'in_review'].includes(latest.status)
          "
          type="primary"
          @click="createRevision"
        >
          {{ t('ontology.nextRevision') }}
        </el-button>
      </div>
      <el-table :data="revisions" v-loading="loading">
        <el-table-column :label="t('ontology.revision')">
          <template #default="{ row }">
            <el-button
              type="primary"
              link
              :disabled="busy"
              @click="
                go(`/ontologies/${head.ontology_id}/revisions/${row.revision}`)
              "
            >
              {{ row.revision }}
            </el-button>
          </template>
        </el-table-column>
        <el-table-column :label="t('ontology.status')">
          <template #default="{ row }">
            {{ t(`ontology.statuses.${row.status}`) }}
          </template>
        </el-table-column>
        <el-table-column prop="version" :label="t('ontology.version')" />
        <el-table-column :label="t('ontology.updatedAt')" min-width="200">
          <template #default="{ row }">
            {{ formatDateTime(row.updated_at) }}
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        :current-page="page"
        :page-size="20"
        :total="total"
        layout="total, prev, pager, next"
        @current-change="changePage"
      />
    </template>
  </main>
</template>
<script setup>
import { computed, ref, watch, onBeforeUnmount } from 'vue'
import {
  useRoute,
  useRouter,
  onBeforeRouteLeave,
  onBeforeRouteUpdate
} from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import {
  navigateConsoleModuleRoute,
  useConsolePageDescriptor,
  useUnsavedChangesGuard,
  formatDateTime
} from '@common-ui'
import { useAuthStore } from '../store/auth'
import client from '../api/client'
import { createOntologyAPI } from '../api/ontology.mjs'
import {
  definitionInput,
  validID,
  positiveNumber,
  requiresReconciliation
} from '../utils/definition.mjs'
const { t } = useI18n(),
  route = useRoute(),
  router = useRouter(),
  auth = useAuthStore(),
  api = createOntologyAPI(client)
const head = ref(null),
  latest = ref(null),
  revisions = ref([]),
  total = ref(0),
  error = ref(''),
  loading = ref(false),
  busy = ref(false),
  uncertain = ref(false)
const page = computed(() =>
  positiveNumber(route.query.page) ? Number(route.query.page) : 1
)
let request = 0
const go = (path, query = {}, history = 'push') =>
  navigateConsoleModuleRoute(
    router,
    'ontology',
    { path, query },
    { history }
  ).catch((e) => {
    error.value = e.message
  })
const changePage = (n) => {
  if (!busy.value) return go(route.path, n === 1 ? {} : { page: n })
}
useConsolePageDescriptor(router, 'ontology', {
  title: computed(() => t('ontology.title')),
  subject: computed(() => head.value?.ontology_id || ''),
  ready: computed(() => Boolean(head.value))
})
onBeforeRouteLeave(() => !busy.value)
onBeforeRouteUpdate(() => !busy.value)
useUnsavedChangesGuard({ router, isDirty: () => busy.value })
async function load() {
  if (busy.value) return
  const current = ++request,
    id = route.params.ontology_id
  loading.value = true
  error.value = ''
  head.value = null
  latest.value = null
  revisions.value = []
  try {
    if (!validID(id)) {
      error.value = t('ontology.invalidRoute')
      return
    }
    if (
      auth.contextType !== 'tenant' ||
      !auth.hasPermission('ontology.revision.read')
    ) {
      error.value = t('ontology.noAccess')
      return
    }
    const canonical = page.value === 1 ? {} : { page: String(page.value) }
    if (JSON.stringify(route.query) !== JSON.stringify(canonical)) {
      await go(route.path, canonical, 'replace')
      return
    }
    const [h, list] = await Promise.all([
      api.head(id),
      api.revisions(id, page.value)
    ])
    const last = await api.get(id, h.last_revision)
    if (current === request) {
      head.value = h
      revisions.value = list.data
      total.value = list.total
      latest.value = last
      uncertain.value = false
    }
  } catch (e) {
    if (current === request)
      error.value = e.response?.data?.error || t('ontology.failed')
  } finally {
    if (current === request) loading.value = false
  }
}
async function createRevision() {
  if (busy.value || uncertain.value || !latest.value) return
  busy.value = true
  try {
    const confirmed = await ElMessageBox.confirm(
      t('ontology.confirmNext'),
      t('ontology.nextRevision'),
      {
        customClass: 'addp-message-box',
        confirmButtonText: t('ontology.confirm'),
        cancelButtonText: t('ontology.cancel')
      }
    ).then(
      () => true,
      () => false
    )
    if (!confirmed) return
    const result = await api.create(
      head.value.ontology_id,
      head.value.last_revision + 1,
      definitionInput(latest.value.snapshot.definition)
    )
    busy.value = false
    await go(`/ontologies/${result.ontology_id}/revisions/${result.revision}`)
  } catch (e) {
    error.value = e.response?.data?.error || t('ontology.failed')
    uncertain.value = requiresReconciliation(e)
  } finally {
    busy.value = false
  }
}
watch(() => route.fullPath, load, { immediate: true })
onBeforeUnmount(() => {
  request++
})
</script>
<style scoped>
.page {
  padding: 24px;
  display: grid;
  gap: 16px;
}
.toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 16px;
}
h2,
h3 {
  margin: 0;
  color: var(--addp-text-primary);
  overflow-wrap: anywhere;
}
.identifier {
  overflow-wrap: anywhere;
}
</style>
