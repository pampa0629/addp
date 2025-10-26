<template>
  <StandaloneLayout v-if="isStandalone" />
  <router-view v-else />
</template>

<script setup>
import { ref } from 'vue'
import StandaloneLayout from '@/components/StandaloneLayout.vue'

const isStandalone = ref(false)

if (typeof window !== 'undefined') {
  isStandalone.value = window.self === window.top
}
</script>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

#app {
  font-family: 'Helvetica Neue', Helvetica, 'PingFang SC', 'Hiragino Sans GB',
    'Microsoft YaHei', '微软雅黑', Arial, sans-serif;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}
</style>
