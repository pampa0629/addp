<template>
  <div class="map-viewer">
    <!-- 左侧控制面板 -->
    <div class="sidebar">
      <div class="sidebar-header">
        <h2>MVT 快显系统</h2>
      </div>

      <!-- 数据源列表 -->
      <div class="datasources-panel">
        <h3>数据源列表</h3>
        <div v-if="loading" class="loading">加载中...</div>
        <div v-else-if="error" class="error">{{ error }}</div>
        <div v-else class="datasource-list">
          <div
            v-for="ds in dataSources"
            :key="ds.id"
            :class="['datasource-item', { active: activeDataSource === ds.id }]"
            @click="loadDataSource(ds)"
          >
            <div class="datasource-name">{{ ds.name }}</div>
            <div class="datasource-meta">{{ ds.type }}</div>
          </div>
        </div>
      </div>

      <!-- 缓存控制 -->
      <div class="cache-panel">
        <h3>缓存管理</h3>
        <button @click="handleClearCache" :disabled="clearing">
          {{ clearing ? '清空中...' : '清空所有缓存' }}
        </button>
        <button
          @click="handleClearDataSourceCache"
          :disabled="!activeDataSource || clearing"
        >
          清空当前数据源缓存
        </button>
      </div>

      <!-- 图例 -->
      <div v-if="activeDataSource" class="legend-panel">
        <h3>图例</h3>
        <div class="legend-item">
          <div class="legend-color" style="background: rgba(0, 136, 136, 0.8)"></div>
          <span>要素填充</span>
        </div>
      </div>
    </div>

    <!-- 地图容器 -->
    <div class="map-container" ref="mapContainer"></div>

    <!-- 加载提示 -->
    <div v-if="mapLoading" class="map-loading">
      <div class="spinner"></div>
      <div>加载瓦片中...</div>
    </div>
  </div>
</template>

<script>
import { ref, onMounted, onUnmounted } from 'vue'
import maplibregl from 'maplibre-gl'
import { getDataSources, clearCache } from '../api/datasources'
import config from '../config'

export default {
  name: 'MapViewer',
  setup() {
    const mapContainer = ref(null)
    const map = ref(null)
    const dataSources = ref([])
    const activeDataSource = ref(null)
    const loading = ref(true)
    const error = ref(null)
    const clearing = ref(false)
    const mapLoading = ref(false)

    // 初始化地图
    const initMap = () => {
      map.value = new maplibregl.Map({
        container: mapContainer.value,
        style: {
          version: 8,
          sources: {
            'osm': {
              type: 'raster',
              tiles: ['https://tile.openstreetmap.org/{z}/{x}/{y}.png'],
              tileSize: 512,
              attribution: '© OpenStreetMap contributors'
            }
          },
          layers: [
            {
              id: 'osm',
              type: 'raster',
              source: 'osm',
              minzoom: 0,
              maxzoom: 22
            }
          ]
        },
        center: [0, 0],
        zoom: 2,
        minZoom: config.minZoom,
        maxZoom: config.maxZoom
      })

      // 添加导航控件
      map.value.addControl(new maplibregl.NavigationControl(), 'top-right')

      // 添加缩放控件
      map.value.addControl(new maplibregl.ScaleControl(), 'bottom-left')

      // 地图加载完成
      map.value.on('load', () => {
        console.log('Map loaded')
      })

      // 监听瓦片加载
      map.value.on('dataloading', () => {
        mapLoading.value = true
      })

      map.value.on('idle', () => {
        mapLoading.value = false
      })
    }

    // 加载数据源列表
    const loadDataSourcesList = async () => {
      try {
        loading.value = true
        error.value = null
        const response = await getDataSources()
        dataSources.value = response.datasources || []
      } catch (err) {
        error.value = '加载数据源失败: ' + err.message
        console.error('Failed to load datasources:', err)
      } finally {
        loading.value = false
      }
    }

    // 加载数据源到地图
    const loadDataSource = (ds) => {
      if (!map.value) return

      // 移除之前的图层
      if (activeDataSource.value) {
        const oldId = activeDataSource.value
        if (map.value.getLayer(`${oldId}-fill`)) {
          map.value.removeLayer(`${oldId}-fill`)
        }
        if (map.value.getLayer(`${oldId}-line`)) {
          map.value.removeLayer(`${oldId}-line`)
        }
        if (map.value.getSource(oldId)) {
          map.value.removeSource(oldId)
        }
      }

      // 添加新的矢量瓦片源
      map.value.addSource(ds.id, {
        type: 'vector',
        tiles: [`${config.tileBaseUrl}/${ds.id}/{z}/{x}/{y}.mvt`],
        minzoom: ds.tile_config.min_zoom,
        maxzoom: ds.tile_config.max_zoom
      })

      // 添加填充图层
      map.value.addLayer({
        id: `${ds.id}-fill`,
        type: 'fill',
        source: ds.id,
        'source-layer': ds.id,
        paint: {
          'fill-color': '#088',
          'fill-opacity': 0.8
        }
      })

      // 添加轮廓线图层
      map.value.addLayer({
        id: `${ds.id}-line`,
        type: 'line',
        source: ds.id,
        'source-layer': ds.id,
        paint: {
          'line-color': '#000',
          'line-width': 1
        }
      })

      // 添加点击事件
      map.value.on('click', `${ds.id}-fill`, (e) => {
        if (!e.features || !e.features.length) return

        const feature = e.features[0]
        const properties = feature.properties

        // 构建弹窗内容
        let html = '<div style="max-width: 300px;">'
        for (const [key, value] of Object.entries(properties)) {
          html += `<div style="margin-bottom: 5px;"><strong>${key}:</strong> ${value}</div>`
        }
        html += '</div>'

        new maplibregl.Popup()
          .setLngLat(e.lngLat)
          .setHTML(html)
          .addTo(map.value)
      })

      // 鼠标悬停样式
      map.value.on('mouseenter', `${ds.id}-fill`, () => {
        map.value.getCanvas().style.cursor = 'pointer'
      })

      map.value.on('mouseleave', `${ds.id}-fill`, () => {
        map.value.getCanvas().style.cursor = ''
      })

      // 如果数据源有extent信息，飞到数据范围
      if (ds.extent && ds.extent.length === 4) {
        const [minLng, minLat, maxLng, maxLat] = ds.extent
        console.log('Fitting map to bounds:', ds.extent)
        map.value.fitBounds(
          [[minLng, minLat], [maxLng, maxLat]],
          {
            padding: 50,
            maxZoom: 15,
            duration: 1000
          }
        )
      }

      activeDataSource.value = ds.id
    }

    // 清空所有缓存
    const handleClearCache = async () => {
      if (!confirm('确定要清空所有缓存吗？')) return

      try {
        clearing.value = true
        await clearCache()
        alert('缓存已清空')
      } catch (err) {
        alert('清空缓存失败: ' + err.message)
      } finally {
        clearing.value = false
      }
    }

    // 清空当前数据源缓存
    const handleClearDataSourceCache = async () => {
      if (!activeDataSource.value) return
      if (!confirm(`确定要清空 ${activeDataSource.value} 的缓存吗？`)) return

      try {
        clearing.value = true
        await clearCache(activeDataSource.value)
        alert('缓存已清空')
      } catch (err) {
        alert('清空缓存失败: ' + err.message)
      } finally {
        clearing.value = false
      }
    }

    onMounted(() => {
      initMap()
      loadDataSourcesList()
    })

    onUnmounted(() => {
      if (map.value) {
        map.value.remove()
      }
    })

    return {
      mapContainer,
      dataSources,
      activeDataSource,
      loading,
      error,
      clearing,
      mapLoading,
      loadDataSource,
      handleClearCache,
      handleClearDataSourceCache
    }
  }
}
</script>

<style scoped>
.map-viewer {
  width: 100%;
  height: 100%;
  display: flex;
  position: relative;
}

.sidebar {
  width: 300px;
  height: 100%;
  background: #ffffff;
  border-right: 1px solid #e0e0e0;
  overflow-y: auto;
  padding: 20px;
  box-shadow: 2px 0 8px rgba(0, 0, 0, 0.1);
}

.sidebar-header {
  margin-bottom: 20px;
}

.sidebar-header h2 {
  font-size: 18px;
  color: #333;
  margin: 0;
}

.datasources-panel,
.cache-panel,
.legend-panel {
  margin-bottom: 30px;
}

.datasources-panel h3,
.cache-panel h3,
.legend-panel h3 {
  font-size: 14px;
  color: #666;
  margin-bottom: 10px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.loading,
.error {
  padding: 10px;
  border-radius: 4px;
  font-size: 14px;
}

.loading {
  background: #e3f2fd;
  color: #1976d2;
}

.error {
  background: #ffebee;
  color: #c62828;
}

.datasource-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.datasource-item {
  padding: 12px;
  background: #f5f5f5;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.2s;
}

.datasource-item:hover {
  background: #e0e0e0;
  transform: translateX(2px);
}

.datasource-item.active {
  background: #1976d2;
  color: white;
}

.datasource-name {
  font-weight: 500;
  margin-bottom: 4px;
}

.datasource-meta {
  font-size: 12px;
  opacity: 0.7;
}

.cache-panel button {
  width: 100%;
  padding: 10px;
  margin-bottom: 8px;
  background: #1976d2;
  color: white;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  font-size: 14px;
  transition: background 0.2s;
}

.cache-panel button:hover:not(:disabled) {
  background: #1565c0;
}

.cache-panel button:disabled {
  background: #ccc;
  cursor: not-allowed;
}

.legend-panel {
  border-top: 1px solid #e0e0e0;
  padding-top: 15px;
}

.legend-item {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}

.legend-color {
  width: 24px;
  height: 24px;
  border-radius: 4px;
  border: 1px solid #ccc;
}

.map-container {
  flex: 1;
  height: 100%;
}

.map-loading {
  position: absolute;
  top: 20px;
  left: 50%;
  transform: translateX(-50%);
  background: rgba(0, 0, 0, 0.7);
  color: white;
  padding: 12px 24px;
  border-radius: 20px;
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 14px;
  z-index: 1000;
}

.spinner {
  width: 16px;
  height: 16px;
  border: 2px solid rgba(255, 255, 255, 0.3);
  border-top-color: white;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
