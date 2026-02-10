<template>
  <div id="app">
    <!-- 如果是登录页，直接显示路由视图 -->
    <router-view v-if="isLoginPage" />

    <!-- 如果在 iframe 中，不显示侧边栏 -->
    <div v-else-if="isInIframe" class="iframe-content">
      <router-view />
    </div>

    <!-- 独立运行时使用布局 -->
    <el-container v-else class="layout-container">
      <el-aside width="200px" class="layout-aside">
        <div class="logo">
          <h3>数据服务</h3>
        </div>

        <el-menu
          :default-active="$route.path"
          router
          background-color="#304156"
          text-color="#bfcbd9"
          active-text-color="#409EFF"
        >
          <el-sub-menu index="services">
            <template #title>
              <el-icon><Connection /></el-icon>
              <span>数据服务</span>
            </template>

            <el-menu-item index="/query-services">
              <el-icon><Search /></el-icon>
              <span>查询服务</span>
            </el-menu-item>

            <el-menu-item index="/tile">
              <el-icon><Grid /></el-icon>
              <span>瓦片服务</span>
            </el-menu-item>

            <el-menu-item index="/published-services">
              <el-icon><Upload /></el-icon>
              <span>服务发布</span>
            </el-menu-item>

            <el-menu-item index="/services">
              <el-icon><Link /></el-icon>
              <span>服务注册</span>
            </el-menu-item>

            <el-menu-item index="/catalog">
              <el-icon><FolderOpened /></el-icon>
              <span>服务目录</span>
            </el-menu-item>
          </el-sub-menu>
        </el-menu>
      </el-aside>

      <el-main class="layout-main">
        <router-view />
      </el-main>
    </el-container>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { Connection, Link, Upload, FolderOpened, Search, Grid } from '@element-plus/icons-vue'

const route = useRoute()

// 判断是否是登录页
const isLoginPage = computed(() => route.path === '/login')

// 判断是否在 iframe 中运行
const isInIframe = computed(() => {
  try {
    return window.self !== window.top
  } catch (e) {
    return true
  }
})
</script>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

#app {
  height: 100vh;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
}

.layout-container {
  height: 100vh;
}

.layout-aside {
  background-color: #304156;
  overflow-y: auto;
}

.logo {
  height: 60px;
  display: flex;
  align-items: center;
  justify-content: center;
  background-color: #2b3a4a;
  color: #fff;
  margin-bottom: 10px;
}

.logo h3 {
  font-size: 18px;
  font-weight: 600;
}

.layout-main {
  background-color: #f0f2f5;
  padding: 0;
  overflow-y: auto;
}

/* iframe 内容样式 */
.iframe-content {
  height: 100vh;
  background-color: #f0f2f5;
  overflow-y: auto;
}

/* 菜单样式调整 */
.el-menu {
  border-right: none;
}

.el-sub-menu__title {
  font-weight: 500;
}

/* 滚动条样式 */
.layout-aside::-webkit-scrollbar {
  width: 6px;
}

.layout-aside::-webkit-scrollbar-thumb {
  background-color: rgba(255, 255, 255, 0.2);
  border-radius: 3px;
}

.layout-aside::-webkit-scrollbar-thumb:hover {
  background-color: rgba(255, 255, 255, 0.3);
}
</style>
