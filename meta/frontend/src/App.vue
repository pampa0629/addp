<template>
  <div id="app">
    <router-view />
  </div>
</template>

<script setup>
// Meta模块嵌入在Portal中，不需要自己的导航和用户菜单
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
