<template>
  <div class="service-catalog">
    <div class="header">
      <h2>服务目录</h2>
      <div class="header-actions">
        <el-button type="success" @click="$router.push('/published-services')">服务发布管理</el-button>
        <el-button type="primary" @click="$router.push('/services')">服务注册管理</el-button>
      </div>
    </div>

    <el-tabs v-model="activeTab" v-loading="loading">
      <el-tab-pane label="全部服务" name="all">
        <div class="service-grid">
          <ServiceCard
            v-for="service in allServices"
            :key="getServiceKey(service)"
            :service="service"
            :source="service._source"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && allServices.length === 0" description="暂无服务" />
      </el-tab-pane>

      <el-tab-pane label="WMS 服务" name="wms">
        <div class="service-grid">
          <ServiceCard
            v-for="service in getServicesByType('wms')"
            :key="getServiceKey(service)"
            :service="service"
            :source="service._source"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && getServicesByType('wms').length === 0" description="暂无 WMS 服务" />
      </el-tab-pane>

      <el-tab-pane label="WFS 服务" name="wfs">
        <div class="service-grid">
          <ServiceCard
            v-for="service in getServicesByType('wfs')"
            :key="getServiceKey(service)"
            :service="service"
            :source="service._source"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && getServicesByType('wfs').length === 0" description="暂无 WFS 服务" />
      </el-tab-pane>

      <el-tab-pane label="WMTS 服务" name="wmts">
        <div class="service-grid">
          <ServiceCard
            v-for="service in getServicesByType('wmts')"
            :key="getServiceKey(service)"
            :source="service._source"
            :service="service"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && getServicesByType('wmts').length === 0" description="暂无 WMTS 服务" />
      </el-tab-pane>

      <el-tab-pane label="OGC API" name="ogc_api">
        <div class="service-grid">
          <ServiceCard
            v-for="service in getServicesByType('ogc_api')"
            :key="getServiceKey(service)"
            :service="service"
            :source="service._source"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && getServicesByType('ogc_api').length === 0" description="暂无 OGC API 服务" />
      </el-tab-pane>

      <el-tab-pane label="Data API" name="data_api">
        <div class="service-grid">
          <ServiceCard
            v-for="service in getServicesByType('data_api')"
            :key="getServiceKey(service)"
            :service="service"
            :source="service._source"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && getServicesByType('data_api').length === 0" description="暂无 Data API 服务" />
      </el-tab-pane>

      <el-tab-pane label="REST API" name="rest">
        <div class="service-grid">
          <ServiceCard
            v-for="service in getServicesByType('rest')"
            :key="getServiceKey(service)"
            :service="service"
            :source="service._source"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && getServicesByType('rest').length === 0" description="暂无 REST API 服务" />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import serviceAPI from '../api/service'
import publishedServiceAPI from '../api/publishedService'
import ServiceCard from '../components/ServiceCard.vue'

const router = useRouter()
const loading = ref(false)
const activeTab = ref('all')
const externalServices = ref([])
const internalServices = ref([])
const dataServices = ref([])

// 合并所有服务
const allServices = computed(() => {
  const external = externalServices.value.map(s => ({ ...s, _source: 'external' }))
  const internal = internalServices.value.map(s => ({ ...s, _source: 'internal' }))
  const data = dataServices.value.map(s => ({ ...s, _source: 'data' }))
  return [...external, ...internal, ...data]
})

// 根据服务类型获取服务（需要处理内部服务的多协议情况）
const getServicesByType = (type) => {
  return allServices.value.filter(s => {
    if (s._source === 'data') {
      // 数据服务：只匹配 data_api
      return type === 'data_api'
    } else if (s._source === 'external') {
      // 服务注册：直接匹配 service_type
      return s.service_type === type
    } else {
      // 空间服务：检查对应的 enabled_xxx 标志
      if (type === 'wms') return s.enabled_wms
      if (type === 'wfs') return s.enabled_wfs
      if (type === 'wmts') return s.enabled_wmts
      if (type === 'ogc_api') return s.enabled_ogc_api
      return false
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

    // 并行加载三类服务
    const [externalData, internalData, dataData] = await Promise.all([
      loadExternalServices(),
      loadInternalServices(),
      loadDataServices()
    ])

    externalServices.value = externalData
    internalServices.value = internalData
    dataServices.value = dataData
  } catch (error) {
    ElMessage.error('加载服务目录失败: ' + (error.response?.data?.message || error.message))
  } finally {
    loading.value = false
  }
}

// 加载外部服务
const loadExternalServices = async () => {
  try {
    const catalog = await serviceAPI.getCatalog()

    // 后端返回的是按类型分组的 map: { "wms": [...], "wfs": [...] }
    if (catalog && typeof catalog === 'object') {
      const allServicesList = []
      Object.values(catalog).forEach(serviceList => {
        if (Array.isArray(serviceList)) {
          allServicesList.push(...serviceList)
        }
      })
      return allServicesList
    }
    return []
  } catch (error) {
    console.error('加载外部服务失败:', error)
    return []
  }
}

// 加载内部服务
const loadInternalServices = async () => {
  try {
    const response = await publishedServiceAPI.listServices({ page: 1, limit: 1000 })
    return response.data || []
  } catch (error) {
    console.error('加载发布服务失败:', error)
    return []
  }
}

// 加载数据服务
const loadDataServices = async () => {
  try {
    // TODO: 当前数据服务没有列表API，暂时返回空数组
    // 未来可以调用 dataAPI.list() 获取数据服务列表
    return []
  } catch (error) {
    console.error('加载数据服务失败:', error)
    return []
  }
}

// 处理服务点击
const handleServiceClick = (service) => {
  if (service._source === 'external') {
    router.push(`/services/${service.id}`)
  } else if (service._source === 'internal') {
    router.push(`/published-services/${service.id}`)
  } else if (service._source === 'data') {
    // 数据服务：已删除，不再跳转
    ElMessage.warning('该功能已移至Develop模块的查询工作台')
  }
}

onMounted(() => {
  loadCatalog()
})
</script>

<style scoped>
.service-catalog {
  padding: 24px;
  height: 100%;
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
