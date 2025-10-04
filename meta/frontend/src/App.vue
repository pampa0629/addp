<template>
  <div id="app">
    <el-container style="height: 100vh">
      <el-header v-if="!isLoginPage" style="background: #545c64; color: #fff; padding: 0 20px; display: flex; align-items: center; justify-content: space-between;">
        <div style="display: flex; align-items: center;">
          <h2 style="margin: 0">ADDP Meta - 元数据管理</h2>
        </div>
        <el-menu
          mode="horizontal"
          :router="true"
          background-color="#545c64"
          text-color="#fff"
          active-text-color="#ffd04b"
          style="border: none; flex: 1; margin-left: 50px;"
        >
          <el-menu-item index="/metadata">元数据浏览</el-menu-item>
          <el-menu-item index="/datasources">数据源列表</el-menu-item>
        </el-menu>
        <div>
          <el-dropdown>
            <span style="color: #fff; cursor: pointer;">
              {{ username }} <el-icon style="margin-left: 5px;"><ArrowDown /></el-icon>
            </span>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item @click="handleLogout">退出登录</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </el-header>
      <el-main :style="{ padding: isLoginPage ? '0' : '20px' }">
        <router-view />
      </el-main>
    </el-container>
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowDown } from '@element-plus/icons-vue'

const router = useRouter()
const route = useRoute()

const isLoginPage = computed(() => route.path === '/meta/login')

// 使用ref使username响应式
const username = ref('Guest')

// 从localStorage获取用户名的函数
const getUsernameFromStorage = () => {
  const user = localStorage.getItem('user')
  if (user && user !== 'undefined' && user !== 'null') {
    try {
      const userData = JSON.parse(user)
      return userData.username || 'Guest'
    } catch (e) {
      return 'Guest'
    }
  }
  return 'Guest'
}

// 初始化username
username.value = getUsernameFromStorage()

// 监听路由变化，更新username（登录成功后会跳转路由）
watch(() => route.path, () => {
  username.value = getUsernameFromStorage()
})

const handleLogout = () => {
  localStorage.removeItem('token')
  localStorage.removeItem('user')
  username.value = 'Guest'  // 立即更新username
  ElMessage.success('已退出登录')
  router.push('/login')
}
</script>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

#app {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
}

body, html {
  height: 100%;
  overflow: hidden;
}
</style>
