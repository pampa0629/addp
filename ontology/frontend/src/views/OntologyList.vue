<template>
  <main class="page">
    <header class="toolbar">
      <h2>{{ t('ontology.list') }}</h2>
      <el-button v-if="canUpdate" type="primary" @click="go('/ontologies/new')">
        {{ t('ontology.create') }}
      </el-button>
    </header>
    <el-alert
      v-if="error"
      :title="error"
      type="error"
      :closable="false"
      show-icon
    />
    <template v-if="canRead">
      <el-button :loading="loading" @click="load">
        {{ t('ontology.refresh') }}
      </el-button>
      <el-table :data="items" v-loading="loading" stripe>
        <el-table-column
          prop="ontology_id"
          :label="t('ontology.id')"
          min-width="200"
        >
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              @click="go(`/ontologies/${row.ontology_id}`)"
            >
              {{ row.ontology_id }}
            </el-button>
          </template>
        </el-table-column>
        <el-table-column
          prop="last_revision"
          :label="t('ontology.lastRevision')"
        />
        <el-table-column :label="t('ontology.activeRevision')">
          <template #default="{ row }">
            {{ row.active_revision ?? t('ontology.inactive') }}
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
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { navigateConsoleModuleRoute } from '@common-ui'
import { useAuthStore } from '../store/auth'
import client from '../api/client'
import { createOntologyAPI } from '../api/ontology.mjs'
import { positiveNumber } from '../utils/definition.mjs'
const { t } = useI18n(),
  route = useRoute(),
  router = useRouter(),
  auth = useAuthStore(),
  api = createOntologyAPI(client)
const canRead = computed(
  () =>
    auth.contextType === 'tenant' &&
    auth.hasPermission('ontology.revision.read')
)
const canUpdate = computed(
  () => canRead.value && auth.hasPermission('ontology.revision.update')
)
const page = computed(() =>
  positiveNumber(route.query.page) ? Number(route.query.page) : 1
)
const items = ref([]),
  total = ref(0),
  loading = ref(false),
  error = ref('')
let request = 0
const go = (path, query = {}) =>
  navigateConsoleModuleRoute(router, 'ontology', { path, query }).catch((e) => {
    error.value = e.message
  })
const changePage = (number) =>
  go('/ontologies', number === 1 ? {} : { page: number })
async function load() {
  const current = ++request
  items.value = []
  total.value = 0
  error.value = ''
  loading.value = true
  try {
    if (!canRead.value) {
      error.value = t('ontology.noAccess')
      return
    }
    const canonical = page.value === 1 ? {} : { page: String(page.value) }
    if (JSON.stringify(route.query) !== JSON.stringify(canonical)) {
      await navigateConsoleModuleRoute(
        router,
        'ontology',
        { path: '/ontologies', query: canonical },
        { history: 'replace' }
      )
      return
    }
    const result = await api.list(page.value)
    if (request === current) {
      items.value = result.data
      total.value = result.total
    }
  } catch (e) {
    if (request === current)
      error.value = e.response?.data?.error || t('ontology.failed')
  } finally {
    if (request === current) loading.value = false
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
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
h2 {
  margin: 0;
  color: var(--addp-text-primary);
}
.page > .el-button {
  justify-self: start;
}
</style>
