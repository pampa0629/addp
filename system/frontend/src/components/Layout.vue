<template>
  <!-- 当在 iframe 中时，只显示内容区域 -->
  <div v-if="isInIframe" class="content-only">
    <slot></slot>
  </div>

  <!-- 独立访问时，显示完整布局 -->
  <el-container v-else class="layout-container">
    <el-header class="header">
      <div class="header-left">
        <el-icon :size="24" style="margin-right: 10px">
          <Platform />
        </el-icon>
        <h1>全域数据平台 - System</h1>
      </div>
      <div class="header-right">
        <el-dropdown>
          <span class="user-dropdown">
            <el-icon><User /></el-icon>
            {{ user?.username }}
            <el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item @click="handleLogout">
                <el-icon><SwitchButton /></el-icon>
                退出登录
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </el-header>

    <el-container>
      <el-aside width="200px" class="sidebar">
        <el-menu
          :default-active="activeMenu"
          :default-openeds="['system']"
          router
          class="el-menu-vertical"
        >
          <el-menu-item index="/">
            <el-icon><HomeFilled /></el-icon>
            <span>系统概览</span>
          </el-menu-item>

          <el-sub-menu index="system">
            <template #title>
              <el-icon><Setting /></el-icon>
              <span>系统管理</span>
            </template>
            <el-menu-item index="/users" @click="handleMenuClick('system', 'users')">
              <el-icon><User /></el-icon>
              <span>用户管理</span>
            </el-menu-item>
            <el-menu-item index="/logs" @click="handleMenuClick('system', 'logs')">
              <el-icon><Document /></el-icon>
              <span>日志管理</span>
            </el-menu-item>
            <el-menu-item index="/resources" @click="handleMenuClick('system', 'resources')">
              <el-icon><Connection /></el-icon>
              <span>存储引擎管理</span>
            </el-menu-item>
            <el-menu-item index="/developer" @click="handleMenuClick('system', 'developer')">
              <el-icon><Monitor /></el-icon>
              <span>开发中心</span>
            </el-menu-item>
          </el-sub-menu>
        </el-menu>
      </el-aside>

      <el-main class="main-content">
        <slot></slot>
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { computed, ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '../store/auth'
import {
  Platform,
  User,
  ArrowDown,
  SwitchButton,
  Setting,
  Document,
  Connection,
  DataAnalysis,
  HomeFilled,
  Monitor
} from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()

// 检测是否在 iframe 中
const isInIframe = ref(false)

// 使用 onMounted 确保在 DOM 挂载后检测
onMounted(() => {
  isInIframe.value = window.self !== window.top
  console.log('System Layout - isInIframe (onMounted):', isInIframe.value)
  console.log('System Layout - window.self:', window.self)
  console.log('System Layout - window.top:', window.top)
})

const user = computed(() => authStore.user)
const activeMenu = computed(() => route.path)

const handleMenuClick = (section, subsection) => {
  // 菜单点击处理，路由已通过 index 属性处理
  console.log('Menu clicked:', section, subsection)
}

const handleLogout = () => {
  authStore.logout()
  ElMessage.success('退出成功')
  router.push('/login')
}
</script>

<style scoped>
.layout-container {
  height: 100vh;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  background: #fff;
  border-bottom: 1px solid #e8e8e8;
  padding: 0 20px;
}

.header-left {
  display: flex;
  align-items: center;
}

.header-left h1 {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  color: #303133;
}

.header-right {
  display: flex;
  align-items: center;
}

.user-dropdown {
  display: flex;
  align-items: center;
  gap: 5px;
  cursor: pointer;
  color: #606266;
  padding: 8px 12px;
  border-radius: 4px;
  transition: all 0.3s;
}

.user-dropdown:hover {
  background: #f5f7fa;
}

.sidebar {
  background: #fff;
  border-right: 1px solid #e8e8e8;
}

.el-menu-vertical {
  border-right: none;
  height: 100%;
}

.main-content {
  background: #f0f2f5;
  padding: 20px;
}

/* iframe 模式样式 */
.content-only {
  width: 100%;
  height: auto;
  min-height: 100vh;
  padding: 20px;
  margin: 0;
  background: #f0f2f5;
  overflow: visible;
  box-sizing: border-box;
}
</style>