<template>
  <MapViewer />
</template>

<script>
import MapViewer from './components/MapViewer.vue'

export default {
  name: 'App',
  components: {
    MapViewer
  }
}
</script>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

html, body {
  width: 100%;
  height: 100%;
  overflow: hidden;
}

#app {
  width: 100%;
  height: 100%;
}
</style>
