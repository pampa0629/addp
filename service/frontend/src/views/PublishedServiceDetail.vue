<template>
  <div class="published-service-detail" v-loading="loading">
    <!-- 顶部操作栏 -->
    <div class="page-header">
      <div class="header-left">
        <h2>{{ service?.title }}</h2>
        <div class="protocol-tags">
          <el-tag v-if="service?.enabled_wfs" size="small" type="success">WFS</el-tag>
          <el-tag v-if="service?.enabled_ogc_api" size="small" type="success">OGC API</el-tag>
          <el-tag v-if="service?.enabled_wmts" size="small" type="success">WMTS</el-tag>
          <el-tag v-if="service?.enabled_wms" size="small" type="info">WMS</el-tag>
        </div>
      </div>
      <div class="header-right">
        <el-button @click="goToEdit">编辑</el-button>
        <el-button @click="goToTest">测试服务</el-button>
        <el-button type="danger" @click="handleDelete">删除</el-button>
      </div>
    </div>

    <!-- 服务信息卡片 -->
    <el-card header="服务信息" style="margin-bottom: 20px;">
      <el-descriptions :column="2" border>
        <el-descriptions-item label="服务名称">{{ service?.service_name }}</el-descriptions-item>
        <el-descriptions-item label="标题">{{ service?.title }}</el-descriptions-item>
        <el-descriptions-item label="摘要" :span="2">
          {{ service?.abstract || '无' }}
        </el-descriptions-item>
        <el-descriptions-item label="关键词">
          <el-tag
            v-for="kw in service?.keywords"
            :key="kw"
            size="small"
            style="margin-right: 5px;"
          >
            {{ kw }}
          </el-tag>
          <span v-if="!service?.keywords || service.keywords.length === 0">无</span>
        </el-descriptions-item>
        <el-descriptions-item label="默认坐标系">EPSG:{{ service?.default_srid }}</el-descriptions-item>
        <el-descriptions-item label="最大要素数">{{ service?.max_features }}</el-descriptions-item>
        <el-descriptions-item label="存储引擎">
          Engine #{{ service?.engine_id }}
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- OGC 端点卡片 -->
    <el-card header="OGC 服务端点" style="margin-bottom: 20px;">
      <!-- WFS 端点 -->
      <div v-if="service?.enabled_wfs" class="endpoint-item">
        <div class="endpoint-title">
          <el-icon><Link /></el-icon>
          WFS GetCapabilities
        </div>
        <div class="endpoint-url">
          <el-input :value="wfsEndpoint" readonly />
          <el-button @click="copyEndpoint(wfsEndpoint)">复制</el-button>
          <el-button @click="testEndpoint(wfsEndpoint)">测试</el-button>
        </div>
      </div>

      <!-- OGC API Features 端点 -->
      <div v-if="service?.enabled_ogc_api" class="endpoint-item">
        <div class="endpoint-title">
          <el-icon><Link /></el-icon>
          OGC API Features (Landing Page)
        </div>
        <div class="endpoint-url">
          <el-input :value="ogcApiEndpoint" readonly />
          <el-button @click="copyEndpoint(ogcApiEndpoint)">复制</el-button>
          <el-button @click="testEndpoint(ogcApiEndpoint)">测试</el-button>
        </div>
      </div>

      <!-- WMTS 端点 -->
      <div v-if="service?.enabled_wmts" class="endpoint-item">
        <div class="endpoint-title">
          <el-icon><Link /></el-icon>
          WMTS GetCapabilities
        </div>
        <div class="endpoint-url">
          <el-input :value="wmtsEndpoint" readonly />
          <el-button @click="copyEndpoint(wmtsEndpoint)">复制</el-button>
          <el-button @click="testEndpoint(wmtsEndpoint)">测试</el-button>
        </div>
      </div>

      <div v-if="!service?.enabled_wfs && !service?.enabled_ogc_api && !service?.enabled_wmts" class="no-endpoints">
        <el-empty description="未启用任何 OGC 协议" />
      </div>
    </el-card>

    <!-- 图层列表 -->
    <el-card header="图层列表">
      <div style="margin-bottom: 16px;">
        <el-button type="primary" @click="showAddLayerDialog = true">添加图层</el-button>
      </div>

      <el-table :data="service?.layers || []" border stripe>
        <el-table-column prop="layer_name" label="图层名称" width="180" />
        <el-table-column prop="title" label="标题" width="180" />
        <el-table-column label="数据源" width="300">
          <template #default="{ row }">
            {{ row.schema_name }}.{{ row.table_name }}
          </template>
        </el-table-column>
        <el-table-column prop="geometry_column" label="几何列" width="120" />
        <el-table-column label="几何类型" width="150">
          <template #default="{ row }">
            {{ row.geometry_types?.join(', ') || '-' }}
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
              {{ row.enabled ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="150" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="editLayer(row)">编辑</el-button>
            <el-button size="small" type="danger" @click="deleteLayer(row.id)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-empty
        v-if="!service?.layers || service.layers.length === 0"
        description="暂无图层，请添加图层"
        style="margin-top: 20px;"
      />
    </el-card>

    <!-- 添加/编辑图层对话框 -->
    <el-dialog
      v-model="showLayerDialog"
      :title="editingLayer ? '编辑图层' : '添加图层'"
      width="600px"
    >
      <el-form :model="layerForm" label-width="120px">
        <el-form-item label="图层名称" required>
          <el-input v-model="layerForm.layer_name" placeholder="例如: poi_restaurants" />
          <div class="help-text">仅支持英文、数字和下划线</div>
        </el-form-item>

        <el-form-item label="标题" required>
          <el-input v-model="layerForm.title" placeholder="例如: 餐饮兴趣点" />
        </el-form-item>

        <el-form-item label="Schema" required>
          <el-input v-model="layerForm.schema_name" placeholder="例如: public" />
        </el-form-item>

        <el-form-item label="表名" required>
          <el-input v-model="layerForm.table_name" placeholder="例如: poi_restaurants" />
        </el-form-item>

        <el-form-item label="启用">
          <el-switch v-model="layerForm.enabled" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="showLayerDialog = false">取消</el-button>
        <el-button type="primary" @click="handleLayerSubmit" :loading="layerSubmitting">
          确定
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Link } from '@element-plus/icons-vue'
import publishedServiceAPI from "../api/publishedService'

const route = useRoute()
const router = useRouter()

const service = ref(null)
const loading = ref(true)
const showLayerDialog = ref(false)
const showAddLayerDialog = computed({
  get: () => showLayerDialog.value && !editingLayer.value,
  set: (val) => {
    if (val) {
      editingLayer.value = null
      resetLayerForm()
      showLayerDialog.value = true
    } else {
      showLayerDialog.value = false
    }
  }
})

const layerSubmitting = ref(false)
const editingLayer = ref(null)

const layerForm = ref({
  layer_name: '',
  title: '',
  schema_name: 'public',
  table_name: '',
  enabled: true
})

// 计算 OGC 端点 URL
const baseURL = computed(() => {
  // 根据环境判断使用哪个基础 URL
  const apiBase = import.meta.env.DEV ? 'http://localhost:8086' : window.location.origin
  return `${apiBase}/ogc`
})

const wfsEndpoint = computed(() =>
  `${baseURL.value}/wfs/${service.value?.service_name}?service=WFS&request=GetCapabilities&version=2.0.0`
)

const ogcApiEndpoint = computed(() =>
  `${baseURL.value}/api/${service.value?.service_name}/`
)

const wmtsEndpoint = computed(() =>
  `${baseURL.value}/wmts/${service.value?.service_name}?service=WMTS&request=GetCapabilities&version=1.0.0`
)

// 加载服务详情
const loadService = async () => {
  loading.value = true
  try {
    // createAPIClient 默认 extractData=true，已经提取了 response.data
    const service_data = await publishedServiceAPI.getService(route.params.id)
    service.value = service_data
  } catch (error) {
    ElMessage.error('加载失败: ' + (error.message || '未知错误'))
  } finally {
    loading.value = false
  }
}

// 复制端点 URL
const copyEndpoint = async (url) => {
  try {
    await navigator.clipboard.writeText(url)
    ElMessage.success('已复制到剪贴板')
  } catch (error) {
    // 降级方案
    const input = document.createElement('input')
    input.value = url
    document.body.appendChild(input)
    input.select()
    document.execCommand('copy')
    document.body.removeChild(input)
    ElMessage.success('已复制到剪贴板')
  }
}

// 测试端点
const testEndpoint = (url) => {
  window.open(url, '_blank')
}

// 跳转到编辑页
const goToEdit = () => {
  router.push(`/published-services/${route.params.id}/edit`)
}

// 跳转到测试页
const goToTest = () => {
  router.push(`/published-services/${route.params.id}/test`)
}

// 删除服务
const handleDelete = async () => {
  try {
    await ElMessageBox.confirm(
      '确定要删除该服务吗？此操作不可恢复。',
      '警告',
      {
        type: 'warning',
        confirmButtonText: '确定',
        cancelButtonText: '取消'
      }
    )

    await publishedServiceAPI.deleteService(route.params.id)
    ElMessage.success('删除成功')
    router.push('/published-services')
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('删除失败: ' + (error.message || '未知错误'))
    }
  }
}

// 重置图层表单
const resetLayerForm = () => {
  layerForm.value = {
    layer_name: '',
    title: '',
    schema_name: 'public',
    table_name: '',
    enabled: true
  }
}

// 编辑图层
const editLayer = (layer) => {
  editingLayer.value = layer
  layerForm.value = { ...layer }
  showLayerDialog.value = true
}

// 提交图层（添加或编辑）
const handleLayerSubmit = async () => {
  layerSubmitting.value = true
  try {
    if (editingLayer.value) {
      // 编辑模式
      await publishedServiceAPI.updateLayer(
        route.params.id,
        editingLayer.value.id,
        layerForm.value
      )
      ElMessage.success('更新成功')
    } else {
      // 添加模式
      await publishedServiceAPI.addLayer(route.params.id, layerForm.value)
      ElMessage.success('添加成功')
    }

    showLayerDialog.value = false
    editingLayer.value = null
    resetLayerForm()
    loadService() // 重新加载服务数据
  } catch (error) {
    ElMessage.error('操作失败: ' + (error.message || '未知错误'))
  } finally {
    layerSubmitting.value = false
  }
}

// 删除图层
const deleteLayer = async (layerId) => {
  try {
    await ElMessageBox.confirm('确定要删除该图层吗？', '警告', {
      type: 'warning',
      confirmButtonText: '确定',
      cancelButtonText: '取消'
    })

    await publishedServiceAPI.deleteLayer(route.params.id, layerId)
    ElMessage.success('删除成功')
    loadService() // 重新加载服务数据
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('删除失败: ' + (error.message || '未知错误'))
    }
  }
}

onMounted(loadService)
</script>

<style scoped>
.published-service-detail {
  padding: 20px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 20px;
}

.header-left h2 {
  margin: 0 0 10px 0;
  font-size: 24px;
  color: #303133;
}

.protocol-tags {
  display: flex;
  gap: 8px;
}

.header-right {
  display: flex;
  gap: 10px;
}

.endpoint-item {
  margin-bottom: 20px;
}

.endpoint-item:last-child {
  margin-bottom: 0;
}

.endpoint-title {
  font-weight: 600;
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  gap: 5px;
  color: #606266;
}

.endpoint-url {
  display: flex;
  gap: 8px;
}

.no-endpoints {
  text-align: center;
  padding: 40px 0;
}

.help-text {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
}

:deep(.el-card__header) {
  font-weight: 600;
  font-size: 16px;
}
</style>
