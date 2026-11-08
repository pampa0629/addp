<template>
  <section data-testid="engine-instance-overview" :aria-busy="loading" v-loading="loading">
    <div class="overview-toolbar">
      <span v-if="snapshot">{{ t('system.engine.instanceOverview.queriedAt') }}: {{ formatDateTime(snapshot.queried_at) }}</span>
      <el-button :disabled="loading || !allowed" @click="load">{{ t('system.engine.actions.refresh') }}</el-button>
    </div>
    <el-alert v-if="error" :title="error" type="error" :closable="false" data-testid="overview-error" />
    <template v-else-if="snapshot">
      <el-table :data="snapshot.metrics" border data-testid="overview-metrics">
        <el-table-column :label="t('system.engine.instanceOverview.metric')" min-width="170">
          <template #default="{ row }">{{ metricLabel(row.key) }}</template>
        </el-table-column>
        <el-table-column :label="t('system.engine.instanceOverview.value')" min-width="160">
          <template #default="{ row }">
            <span v-if="row.status === 'available'">{{ number(row.value) }} {{ t(`system.engine.instanceOverview.units.${row.unit}`) }}</span>
            <span v-else>{{ t(`system.engine.instanceOverview.states.${row.status}`) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('system.engine.instanceOverview.scope')" min-width="180">
          <template #default="{ row }">{{ t(`system.engine.instanceOverview.scopes.${row.scope}`) }}<template v-if="row.scope_name">: {{ row.scope_name }}</template></template>
        </el-table-column>
        <el-table-column :label="t('system.engine.instanceOverview.semantics')" min-width="100">
          <template #default="{ row }">{{ t(`system.engine.instanceOverview.semanticsValues.${row.semantics}`) }}</template>
        </el-table-column>
      </el-table>
      <p class="overview-note">{{ t('system.engine.instanceOverview.notice') }}</p>
    </template>
    <p v-else-if="!loading" class="overview-note">{{ t('system.engine.instanceOverview.refreshRequired') }}</p>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatDateTime } from '../../../../../common-frontend/basic/src/utils/formatters'
import { createLatestRequestScope } from '../../../../../common-frontend/basic/src/utils/latestRequestScope.mjs'
import { enginesAPI } from '../../api/engines'
import { useAuthStore } from '../../store/auth'
import { useEngineAuthorizationSession } from '../../utils/engineAuthorizationSession'
import { supportsEngineInstanceOverview, instanceOverviewSnapshot } from '../../utils/engineInstanceOverview'

const props = defineProps({ engine: { type: Object, required: true } })
const { t, te, locale } = useI18n(), auth = useAuthStore()
const { authorizationKey, authorizationReady } = useEngineAuthorizationSession(auth, computed(() => props.engine))
const allowed = computed(() => authorizationReady.value && auth.hasPermission('system.engine.read') && supportsEngineInstanceOverview(props.engine))
const requests = createLatestRequestScope(), snapshot = ref(null), error = ref(''), loading = ref(false)
const metricLabel = key => te(`system.engine.instanceOverview.metrics.${key}`) ? t(`system.engine.instanceOverview.metrics.${key}`) : key
const number = value => new Intl.NumberFormat(locale.value).format(BigInt(value))
async function load() {
  const ticket = requests.invalidate()
  snapshot.value = null; error.value = ''; loading.value = false
  if (!allowed.value) return
  loading.value = true
  const engine = props.engine
  try {
    const result = await requests.request(options => enginesAPI.getInstanceOverview(engine.id, options))
    if (!requests.current(ticket)) return
    if (result?.version !== engine.version) throw new Error('instance_version_changed')
    snapshot.value = instanceOverviewSnapshot(result, engine)
  } catch (failure) {
    if (requests.current(ticket)) error.value = t(failure.message === 'instance_version_changed' ? 'system.engine.instanceOverview.versionChanged' : 'system.engine.instanceOverview.failed')
  } finally {
    if (requests.current(ticket)) loading.value = false
  }
}
let initialReadStarted = false
watch([authorizationKey, allowed, () => props.engine.version], () => {
  if (!initialReadStarted && allowed.value) {
    initialReadStarted = true
    load()
    return
  }
  // A changed identity or background health observation invalidates the display;
  // it must not turn health polling into automatic source-statistics retries.
  requests.invalidate(); snapshot.value = null; error.value = ''; loading.value = false
}, { immediate: true })
onBeforeUnmount(() => requests.dispose())
</script>

<style scoped>
.overview-toolbar { display: flex; justify-content: space-between; align-items: center; gap: 12px; flex-wrap: wrap; margin-bottom: 12px; }
.overview-note { color: var(--addp-text-secondary); margin: 12px 0 0; }
</style>
