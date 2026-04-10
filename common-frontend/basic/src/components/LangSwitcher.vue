<template>
  <el-dropdown trigger="click" @command="handleCommand">
    <el-button circle :title="currentTitle">
      <span class="lang-icon">{{ currentLang === 'zh-cn' ? 'CN' : 'EN' }}</span>
    </el-button>
    <template #dropdown>
      <el-dropdown-menu>
        <el-dropdown-item
          v-for="lang in SUPPORTED_LANGUAGES"
          :key="lang.value"
          :command="lang.value"
          :class="{ 'is-active': locale === lang.value }"
        >
          {{ lang.label }}
        </el-dropdown-item>
      </el-dropdown-menu>
    </template>
  </el-dropdown>
</template>

<script setup>
import { computed } from 'vue'
import { useAddpI18n, SUPPORTED_LANGUAGES } from '../composables/useAddpI18n'

const { locale, switchLang } = useAddpI18n()

const currentLang = computed(() => locale.value)

const currentTitle = computed(() =>
  SUPPORTED_LANGUAGES.find(l => l.value === locale.value)?.label || 'Language'
)

const handleCommand = (lang) => {
  switchLang(lang)
}
</script>

<style scoped>
.lang-icon {
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.5px;
}

.is-active {
  background-color: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  font-weight: 600;
}
</style>
