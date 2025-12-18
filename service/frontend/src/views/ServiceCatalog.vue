<template>
  <div class="service-catalog">
    <div class="header">
      <h2>服务目录</h2>
      <el-button type="primary" @click="$router.push('/services')">服务管理</el-button>
    </div>

    <el-tabs v-model="activeTab" v-loading="loading">
      <el-tab-pane label="全部服务" name="all">
        <div class="service-grid">
          <ServiceCard
            v-for="service in allServices"
            :key="service.id"
            :service="service"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && allServices.length === 0" description="暂无服务" />
      </el-tab-pane>

      <el-tab-pane label="WMS 服务" name="wms">
        <div class="service-grid">
          <ServiceCard
            v-for="service in getServicesByType('wms')"
            :key="service.id"
            :service="service"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && getServicesByType('wms').length === 0" description="暂无 WMS 服务" />
      </el-tab-pane>

      <el-tab-pane label="WFS 服务" name="wfs">
        <div class="service-grid">
          <ServiceCard
            v-for="service in getServicesByType('wfs')"
            :key="service.id"
            :service="service"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && getServicesByType('wfs').length === 0" description="暂无 WFS 服务" />
      </el-tab-pane>

      <el-tab-pane label="WMTS 服务" name="wmts">
        <div class="service-grid">
          <ServiceCard
            v-for="service in getServicesByType('wmts')"
            :key="service.id"
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
            :key="service.id"
            :service="service"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && getServicesByType('ogc_api').length === 0" description="暂无 OGC API 服务" />
      </el-tab-pane>

      <el-tab-pane label="Data API" name="data_api">
        <div class="service-grid">
          <ServiceCard
            v-for="service in getServicesByType('data_api')"
            :key="service.id"
            :service="service"
            @click="handleServiceClick(service)"
          />
        </div>
        <el-empty v-if="!loading && getServicesByType('data_api').length === 0" description="暂无 Data API 服务" />
      </el-tab-pane>

      <el-tab-pane label="REST API" name="rest">
        <div class="service-grid">
          <ServiceCard
            v-for="service in getServicesByType('rest')"
            :key="service.id"
            :service="service"
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
import ServiceCard from '../components/ServiceCard.vue'

const router = useRouter()
const loading = ref(false)
const activeTab = ref('all')
const services = ref([])

const allServices = computed(() => services.value)

const getServicesByType = (type) => {
  return services.value.filter(s => s.service_type === type)
}

const loadCatalog = async () => {
  try {
    loading.value = true
    const { data } = await serviceAPI.getCatalog()
    services.value = data.services || []
  } catch (error) {
    ElMessage.error('加载服务目录失败: ' + (error.response?.data?.message || error.message))
  } finally {
    loading.value = false
  }
}

const handleServiceClick = (service) => {
  router.push(`/services/${service.id}`)
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

.service-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 20px;
  padding: 20px 0;
}
</style>
