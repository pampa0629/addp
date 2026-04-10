<template>
  <el-dropdown trigger="click" @command="handleCommand">
    <el-button circle :title="currentTitle">
      <span class="lang-icon">{{ lang === 'zh-cn' ? 'CN' : 'EN' }}</span>
    </el-button>
    <template #dropdown>
      <el-dropdown-menu>
        <el-dropdown-item
          v-for="item in languages"
          :key="item.value"
          :command="item.value"
          :class="{ 'is-active': lang === item.value }"
        >
          {{ item.label }}
        </el-dropdown-item>
      </el-dropdown-menu>
    </template>
  </el-dropdown>
</template>

<script setup>
import { computed } from 'vue'
import { useLangStore } from '../store/lang'

const languages = [
  { value: 'zh-cn', label: '中文' },
  { value: 'en', label: 'English' }
]

const langStore = useLangStore()
const lang = computed(() => langStore.lang)

const currentTitle = computed(() =>
  languages.find(l => l.value === lang.value)?.label || 'Language'
)

const handleCommand = (newLang) => {
  langStore.setLang(newLang)
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
