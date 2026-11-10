<template>
  <router-view />
</template>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

#app {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
  background: var(--addp-bg-secondary) !important;
  color: var(--addp-text-primary);
  /* Apply the initial theme before text inherits a transitional color. */
  transition: none;
}

</style>
