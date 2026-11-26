<template>
  <div class="service-catalog">
    <div class="header">
      <h2>{{ t('service.catalog.title') }}</h2>
      <div class="header-actions">
        <el-button v-if="canReadDefinitions" type="success" @click="openQueryServices">{{ t('service.catalog.manageQuery') }}</el-button>
        <el-button v-if="canReadRegistrations" type="primary" @click="openRegisteredServices">{{ t('service.catalog.manageRegistered') }}</el-button>
      </div>
    </div>

    <el-alert v-if="failedSources.length" type="error" :closable="false"
      :title="t('service.catalog.sourcesFailed', { sources: failedSources.map(source => t('service.serviceCard.sourceType.' + source)).join(', ') })">
      <el-button link @click="loadCatalog">{{ t('service.common.refresh') }}</el-button>
    </el-alert>
    <el-tabs v-model="activeTab" v-loading="loading" @tab-change="handleTabChange">
      <el-tab-pane v-for="tab in tabs" :key="tab.name" :label="t('service.catalog.' + tab.label)" :name="tab.name">
        <template v-if="activeTab === tab.name">
          <div class="service-grid">
            <ServiceCard v-for="service in visibleServices" :key="service._source + '-' + service.id"
              :source="service._source" :service="service" @click="handleServiceClick(service)" />
          </div>
          <el-empty v-if="!loading && !failedSources.length && !visibleServices.length" :description="t('service.catalog.' + tab.empty)" />
        </template>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { resolveCanonicalTabRouteState } from '@common-ui'
import queryServiceAPI from '../api/queryService'
import registeredServiceAPI from '../api/registeredService'
import tileServiceAPI from '../api/tileService'
import graphQueryServiceAPI from '../api/graphQueryService'
import ServiceCard from '../components/ServiceCard.vue'
import { getEnabledProtocols } from '../utils/serviceHelper'
import { navigateServiceRoute } from '@/utils/moduleNavigation'
import { useAuthStore } from '../store/auth'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const canReadDefinitions = computed(() => authStore.hasPermission('service.definition.read'))
const canReadRegistrations = computed(() => authStore.hasPermission('service.external_registration.read'))
const loading = ref(false)
const tabs = [
  { name: 'all', label: 'tabAll', empty: 'emptyAll' },
  { name: 'wms', label: 'tabWms', empty: 'emptyWms' },
  { name: 'wfs', label: 'tabWfs', empty: 'emptyWfs' },
  { name: 'wmts', label: 'tabWmts', empty: 'emptyWmts' },
  { name: 'ogc_api', label: 'tabOgcApi', empty: 'emptyOgcApi' },
  { name: 'xyz', label: 'tabXyz', empty: 'emptyXyz' },
  { name: 'rest', label: 'tabRest', empty: 'emptyRest' },
  { name: 'graph', label: 'tabGraph', empty: 'emptyGraph' }
]
const resolveRouteState = routeQuery => resolveCanonicalTabRouteState({
  allowedTabs: tabs.map(tab => tab.name),
  defaultTab: 'all',
  routeQuery
})
const activeTab = ref(resolveRouteState(route.query).tab)
const allServices = ref([])
const failedSources = ref([])
const visibleServices = computed(() => {
  const type = activeTab.value
  if (type === 'all') return allServices.value
  return allServices.value.filter(s => {
    if (s._source === 'query') {
      // 查询服务：检查协议配置
      const protocols = getEnabledProtocols(s)
      if (type === 'rest') {
        return protocols.some(p => p.key === 'rest_api')
      }
      if (type === 'ogc_api') {
        return protocols.some(p => p.key === 'ogc_features')
      }
      return false
    } else if (s._source === 'tile') {
      // 瓦片服务：检查协议配置
      if (type === 'xyz' && s.protocols?.xyz?.enabled) {
        return true
      }
      if (type === 'wmts' && s.protocols?.wmts?.enabled) {
        return true
      }
      if (type === 'ogc_api' && s.protocols?.ogc_tiles?.enabled) {
        return true
      }
      return false
    } else if (s._source === 'registered') {
      // 注册服务：直接匹配 service_type
      return s.service_type === type
    } else if (s._source === 'graph') {
      // 图查询服务：仅在图查询标签页显示
      return type === 'graph'
    }
    return false
  })
})

const pageSize = 100
async function loadPages(source, fetchPage) {
  const services = []
  for (let page = 1; ; page++) {
    const { data, total } = await fetchPage(page)
    services.push(...data.map(service => ({ ...service, _source: source })))
    if (!data.length || services.length >= total) return services
  }
}

async function loadCatalog() {
  if (loading.value) return
  loading.value = true
  failedSources.value = []
  const sources = []
  if (canReadDefinitions.value) {
    sources.push(
      ['query', page => queryServiceAPI.listServices({ page, limit: pageSize })],
      ['tile', page => tileServiceAPI.listServices({ offset: (page - 1) * pageSize, limit: pageSize })],
      ['graph', page => graphQueryServiceAPI.listServices({ page, page_size: pageSize })]
    )
  }
  if (canReadRegistrations.value) {
    sources.push(['registered', page => registeredServiceAPI.listServices({ page, limit: pageSize })])
  }
  const results = await Promise.allSettled(sources.map(([source, fetchPage]) => loadPages(source, fetchPage)))
  allServices.value = results.flatMap(result => result.status === 'fulfilled' ? result.value : [])
  failedSources.value = sources.filter((_, index) => results[index].status === 'rejected').map(([source]) => source)
  loading.value = false
}

// 处理服务点击
const handleServiceClick = (service) => {
  if (service._source === 'registered' && !canReadRegistrations.value) return
  if (service._source !== 'registered' && !canReadDefinitions.value) return
  if (service._source === 'query') {
    navigateServiceRoute(router, `/query-services/${service.id}`)
  } else if (service._source === 'registered') {
    navigateServiceRoute(router, `/services/${service.id}`)
  } else if (service._source === 'tile') {
    navigateServiceRoute(router, `/tile/${service.id}`)
  } else if (service._source === 'graph') {
    navigateServiceRoute(router, `/graph-services/${service.id}`)
  }
}

const openQueryServices = () => navigateServiceRoute(router, '/query-services')
const openRegisteredServices = () => navigateServiceRoute(router, '/services')

async function handleTabChange(tab) {
  const routeState = resolveRouteState({ tab })
  const location = { path: route.path, query: routeState.query }
  if (router.resolve(location).fullPath !== route.fullPath) {
    await navigateServiceRoute(router, location, { history: 'replace' })
  }
}

async function restoreTabFromRoute() {
  const routeState = resolveRouteState(route.query)
  activeTab.value = routeState.tab
  if (routeState.changed) {
    await navigateServiceRoute(router, {
      path: route.path,
      query: routeState.query
    }, { history: 'replace' })
  }
}

watch(() => route.query, restoreTabFromRoute)
onMounted(async () => {
  await restoreTabFromRoute()
  await loadCatalog()
})
</script>

<style scoped>
.service-catalog {
  padding: 24px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.header h2 {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
}

.header-actions {
  display: flex;
  gap: 10px;
}

.service-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 20px;
  padding: 20px 0;
}
</style>
