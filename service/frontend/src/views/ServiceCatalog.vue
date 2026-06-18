<template>
  <div class="service-catalog">
    <div class="header">
      <h2>{{ t('service.catalog.title') }}</h2>
      <div class="header-actions">
        <el-button type="success" @click="$router.push('/query-services')">{{ t('service.catalog.manageQuery') }}</el-button>
        <el-button type="primary" @click="$router.push('/registered-services')">{{ t('service.catalog.manageRegistered') }}</el-button>
      </div>
    </div>

    <el-tabs v-model="activeTab" v-loading="loading">
      <el-tab-pane :label="t('service.catalog.tabAll')" name="all">
        <div class="service-grid">
          <ServiceCard
            v-for="service in allServices"
            :key="getServiceKey(service)"
            :service="service"
            :source="service._source"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && allServices.length === 0" :description="t('service.catalog.emptyAll')" />
      </el-tab-pane>

      <el-tab-pane :label="t('service.catalog.tabWms')" name="wms">
        <div class="service-grid">
          <ServiceCard
            v-for="service in getServicesByType('wms')"
            :key="getServiceKey(service)"
            :service="service"
            :source="service._source"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && getServicesByType('wms').length === 0" :description="t('service.catalog.emptyWms')" />
      </el-tab-pane>

      <el-tab-pane :label="t('service.catalog.tabWfs')" name="wfs">
        <div class="service-grid">
          <ServiceCard
            v-for="service in getServicesByType('wfs')"
            :key="getServiceKey(service)"
            :service="service"
            :source="service._source"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && getServicesByType('wfs').length === 0" :description="t('service.catalog.emptyWfs')" />
      </el-tab-pane>

      <el-tab-pane :label="t('service.catalog.tabWmts')" name="wmts">
        <div class="service-grid">
          <ServiceCard
            v-for="service in getServicesByType('wmts')"
            :key="getServiceKey(service)"
            :source="service._source"
            :service="service"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && getServicesByType('wmts').length === 0" :description="t('service.catalog.emptyWmts')" />
      </el-tab-pane>

      <el-tab-pane :label="t('service.catalog.tabOgcApi')" name="ogc_api">
        <div class="service-grid">
          <ServiceCard
            v-for="service in getServicesByType('ogc_api')"
            :key="getServiceKey(service)"
            :service="service"
            :source="service._source"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && getServicesByType('ogc_api').length === 0" :description="t('service.catalog.emptyOgcApi')" />
      </el-tab-pane>

      <el-tab-pane :label="t('service.catalog.tabXyz')" name="xyz">
        <div class="service-grid">
          <ServiceCard
            v-for="service in getServicesByType('xyz')"
            :key="getServiceKey(service)"
            :service="service"
            :source="service._source"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && getServicesByType('xyz').length === 0" :description="t('service.catalog.emptyXyz')" />
      </el-tab-pane>

      <el-tab-pane :label="t('service.catalog.tabRest')" name="rest">
        <div class="service-grid">
          <ServiceCard
            v-for="service in getServicesByType('rest')"
            :key="getServiceKey(service)"
            :service="service"
            :source="service._source"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && getServicesByType('rest').length === 0" :description="t('service.catalog.emptyRest')" />
      </el-tab-pane>

      <el-tab-pane :label="t('service.catalog.tabGraph')" name="graph">
        <div class="service-grid">
          <ServiceCard
            v-for="service in getServicesByType('graph')"
            :key="getServiceKey(service)"
            :service="service"
            :source="service._source"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && getServicesByType('graph').length === 0" :description="t('service.catalog.emptyGraph')" />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import serviceAPI from '../api/service'
import queryServiceAPI from '../api/queryService'
import registeredServiceAPI from '../api/registeredService'
import tileServiceAPI from '../api/tileService'
import graphQueryServiceAPI from '../api/graphQueryService'
import ServiceCard from '../components/ServiceCard.vue'
import { getEnabledProtocols } from '../utils/serviceHelper'

const { t } = useI18n()
const router = useRouter()
const loading = ref(false)
const activeTab = ref('all')
const externalServices = ref([])
const queryServices = ref([])  // 查询服务（原内部服务）
const registeredServices = ref([])  // 注册服务
const tileServices = ref([])  // 瓦片服务
const graphQueryServices = ref([])  // 图查询服务

// 合并所有服务
const allServices = computed(() => {
  const external = externalServices.value.map(s => ({ ...s, _source: 'external' }))
  const query = queryServices.value.map(s => ({ ...s, _source: 'query' }))
  const registered = registeredServices.value.map(s => ({ ...s, _source: 'registered' }))
  const tile = tileServices.value.map(s => ({ ...s, _source: 'tile' }))
  const graph = graphQueryServices.value.map(s => ({ ...s, _source: 'graph' }))
  return [...external, ...query, ...registered, ...tile, ...graph]
})

// 根据服务类型获取服务（需要处理查询服务的多协议情况）
const getServicesByType = (type) => {
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
    } else {
      // 外部服务（旧的 service 注册）：直接匹配 service_type
      return s.service_type === type
    }
  })
}

// 生成唯一的服务key
const getServiceKey = (service) => {
  return `${service._source}-${service.id}`
}

// 加载服务目录
const loadCatalog = async () => {
  try {
    loading.value = true

    // 并行加载查询服务、注册服务和瓦片服务
    const [queryData, registeredData, tileData, graphData] = await Promise.all([
      loadQueryServices(),
      loadRegisteredServices(),
      loadTileServices(),
      loadGraphQueryServices()
    ])

    externalServices.value = []  // 旧架构的外部服务已废弃
    queryServices.value = queryData
    registeredServices.value = registeredData
    tileServices.value = tileData
    graphQueryServices.value = graphData
  } catch (error) {
    ElMessage.error(t('service.catalog.loadFailed') + ': ' + (error.response?.data?.message || error.message))
  } finally {
    loading.value = false
  }
}

// 加载外部服务 (已废弃,保留空实现以兼容)
const loadExternalServices = async () => {
  // 旧架构的外部服务已废弃,现在统一使用注册服务
  return []
}

// 加载图查询服务
const loadGraphQueryServices = async () => {
  try {
    const res = await graphQueryServiceAPI.listServices({ page: 1, page_size: 1000 })
    return res.data || []
  } catch (error) {
    console.error('加载图查询服务失败:', error)
    return []
  }
}

// 加载查询服务
const loadQueryServices = async () => {
  try {
    const response = await queryServiceAPI.listServices({ page: 1, limit: 1000 })
    return response.data || []
  } catch (error) {
    console.error('加载查询服务失败:', error)
    return []
  }
}

// 加载注册服务
const loadRegisteredServices = async () => {
  try {
    const response = await registeredServiceAPI.listServices({ page: 1, limit: 1000 })
    return response.data || []
  } catch (error) {
    console.error('加载注册服务失败:', error)
    return []
  }
}

// 加载瓦片服务
const loadTileServices = async () => {
  try {
    const response = await tileServiceAPI.listServices({ offset: 0, limit: 1000 })
    return response.data || []
  } catch (error) {
    console.error('加载瓦片服务失败:', error)
    return []
  }
}

// 处理服务点击
const handleServiceClick = (service) => {
  if (service._source === 'external') {
    router.push(`/services/${service.id}`)
  } else if (service._source === 'query') {
    router.push(`/query-services/${service.id}`)
  } else if (service._source === 'registered') {
    router.push(`/registered-services/${service.id}`)
  } else if (service._source === 'tile') {
    router.push(`/tile/${service.id}`)
  } else if (service._source === 'graph') {
    router.push(`/graph-services/${service.id}`)
  }
}

onMounted(() => {
  loadCatalog()
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
