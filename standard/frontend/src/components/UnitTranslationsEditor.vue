<template>
  <div class="unit-translations">
    <el-tabs v-model="activeLanguage" @tab-remove="removeLanguage">
      <el-tab-pane v-for="(text, language) in modelValue" :key="language" :label="language" :name="language" :closable="language !== 'zh-cn' && language !== 'en'">
        <el-form-item :label="t('standard.common.name')" required>
          <el-input :model-value="text.name" :maxlength="100" @update:model-value="update(language, 'name', $event)" />
        </el-form-item>
        <el-form-item v-if="withSymbol" :label="t('standard.unit.symbolLabel')">
          <el-input :model-value="text.symbol" :maxlength="30" @update:model-value="update(language, 'symbol', $event)" />
        </el-form-item>
        <el-form-item :label="t('standard.common.description')">
          <el-input :model-value="text.description" type="textarea" :rows="2" :maxlength="4096" @update:model-value="update(language, 'description', $event)" />
        </el-form-item>
      </el-tab-pane>
    </el-tabs>
    <div class="language-entry">
      <el-input v-model="newLanguage" :placeholder="t('standard.unit.languageCode')" />
      <el-button :disabled="!canAdd" @click="addLanguage">{{ t('standard.unit.addLanguage') }}</el-button>
    </div>
  </div>
</template>
<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
const props = defineProps({ modelValue: { type: Object, required: true }, withSymbol: Boolean })
const emit = defineEmits(['update:modelValue'])
const { t } = useI18n()
const activeLanguage = ref('zh-cn')
const newLanguage = ref('')
const language = computed(() => newLanguage.value.trim().toLowerCase())
const canAdd = computed(() => /^[a-z]{2,3}(-[a-z0-9]{2,8})*$/.test(language.value) && !props.modelValue[language.value] && Object.keys(props.modelValue).length < 64)
function update(language, field, value) {
  emit('update:modelValue', { ...props.modelValue, [language]: { ...props.modelValue[language], [field]: value } })
}
function removeLanguage(language) {
  if (language === 'zh-cn' || language === 'en') return
  const value = { ...props.modelValue }
  delete value[language]
  emit('update:modelValue', value)
  if (activeLanguage.value === language) activeLanguage.value = 'zh-cn'
}
function addLanguage() {
  if (!canAdd.value) return
  const code = language.value
  emit('update:modelValue', { ...props.modelValue, [code]: { name: '', description: '', symbol: '' } })
  activeLanguage.value = code
  newLanguage.value = ''
}
</script>
<style scoped>
.unit-translations { width: 100%; }
.language-entry { display: flex; gap: 8px; margin: 12px 0; }
</style>
