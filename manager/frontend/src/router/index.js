import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../store/auth'

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('../views/Login.vue'),
    meta: { title: '登录-addp' }
  },
  {
    path: '/',
    component: () => import('../components/Layout.vue'),
    meta: { requiresAuth: true, title: '数据管理-addp' },
    children: [
      {
        path: '',
        redirect: 'data-explorer'
      },
      {
		path: 'settings/embedding',
		name: 'EmbeddingConfiguration',
		component: () => import('../views/EmbeddingConfiguration.vue'),
		meta: { requiresAuth: true, title: '向量化配置-addp' }
	  },
	  {
        path: 'data-explorer',
        name: 'DataExplorer',
        component: () => import('../views/DataExplorer.vue'),
        meta: { requiresAuth: true, title: '数据管理-addp' }
      },
      {
        path: 'data-retrieval',
        name: 'DataRetrieval',
        component: () => import('../views/DataRetrieval.vue'),
        meta: { requiresAuth: true, title: '数据检索-addp' }
      },
      {
        path: 'vectorization-tasks',
        name: 'VectorizationTasks',
        component: () => import('../views/VectorizationTasks.vue'),
        meta: { requiresAuth: true, title: '向量化任务-addp' }
      },
      {
        path: 'spatial-quick-view/vector-tile-cache',
        name: 'TileCache',
        component: () => import('../views/TileCache.vue'),
        meta: { requiresAuth: true, title: '矢量快显 - 瓦片缓存-addp' }
      },
      {
        path: 'spatial-tasks/vector-tiles',
        name: 'VectorTileSet',
        component: () => import('../views/VectorTileSet.vue'),
        meta: { requiresAuth: true, title: '矢量瓦片任务-addp' }
      },
      {
        path: 'spatial-quick-view/vector-materialized-view',
        name: 'VectorMaterializedView',
        component: () => import('../views/VectorMaterializedView.vue'),
        meta: { requiresAuth: true, title: '矢量快显 - 物化视图-addp' }
      },
      {
        path: 'spatial-quick-view/raster-cog',
        name: 'RasterCOG',
        component: () => import('../views/RasterCOG.vue'),
        meta: { requiresAuth: true, title: '栅格快显 - COG-addp' }
      },
      {
        path: 'spatial-quick-view/raster-mosaic',
        name: 'RasterMosaic',
        component: () => import('../views/RasterMosaic.vue'),
        meta: { requiresAuth: true, title: '栅格快显 - Mosaic-addp' }
      },
      {
        path: 'spatial-quick-view/cad-preview',
        name: 'CADPreviewManagement',
        component: () => import('../views/CADPreviewManagement.vue'),
        meta: { requiresAuth: true, title: 'CAD 快显-addp' }
      },
      {
        path: 'model-3d-glb',
        name: 'Model3DGLB',
        component: () => import('../views/Model3DGLB.vue'),
        meta: { requiresAuth: true, title: '三维模型 GLB-addp' }
      },
      {
        path: 'model-3d-tiles',
        name: 'Model3DTiles',
        component: () => import('../views/Model3DTiles.vue'),
        meta: { requiresAuth: true, title: '三维快显 - 瓦片-addp' }
      },
      {
        path: 'gaussian-splat-ksplat',
        name: 'GaussianSplatKSplat',
        component: () => import('../views/GaussianSplatKSplat.vue'),
        meta: { requiresAuth: true, title: '3DGS KSplats-addp' }
      },
      {
        path: 'point-cloud-copc',
        name: 'PointCloudCOPC',
        component: () => import('../views/PointCloudCOPC.vue'),
        meta: { requiresAuth: true, title: '点云快显 - COPC-addp' }
      },
      {
        path: 'spatial-preview',
        name: 'SpatialPreview',
        component: () => import('../views/SpatialPreview.vue'),
        meta: { requiresAuth: true, title: '空间预览-addp' }
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.DEV ? '/' : '/manager/'),
  routes
})

// 路由守卫：支持两种运行模式（Console 嵌入 + 独立访问）
import { createAuthGuard } from '@common-ui'

router.beforeEach(createAuthGuard(useAuthStore, {
  moduleName: 'Manager',
  loginRouteName: 'Login'
}))

const DEFAULT_TITLE = '数据管理-addp'

router.afterEach((to) => {
  if (typeof document === 'undefined') {
    return
  }
  const pageTitle = typeof to.meta?.title === 'string' ? to.meta.title : DEFAULT_TITLE
  document.title = pageTitle
})

export default router
