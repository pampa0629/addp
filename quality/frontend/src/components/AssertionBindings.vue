<template>
  <div class="assertion-bindings">
    <el-alert :title="t('quality.assertion.bindingHint')" type="info" :closable="false" />
    <section v-for="(symbols, role) in inputs" :key="role">
      <strong>{{ role || t('quality.assertion.root') }}</strong>
      <el-form-item v-if="role" :label="t('quality.plan.referenceTable')">
        <el-select v-model="bindings.relations[role].table" @change="clearFields(role)">
          <el-option v-for="b in tables" :key="b.alias" :value="b.alias" :label="b.alias" />
        </el-select>
      </el-form-item>
      <el-form-item v-for="symbol in symbols" :key="symbol" :label="symbol">
        <el-select v-model="binding(role).fields[symbol]" filterable :allow-create="!tables.find(b => b.alias === binding(role).table)?.locator" default-first-option>
          <el-option v-for="f in fieldsForAlias(binding(role).table)" :key="f.column_name" :value="f.column_name" :label="f.column_name" />
        </el-select>
      </el-form-item>
    </section>
  </div>
</template>
<script setup>
import { computed } from 'vue';
import { useI18n } from 'vue-i18n';
import { assertionInputs } from '../utils/assertion';
const props = defineProps({ assertion: { type: Object, required: true }, bindings: { type: Object, required: true }, tables: { type: Array, required: true }, fieldsForAlias: { type: Function, required: true } });
const { t } = useI18n();
const inputs = computed(() => assertionInputs(props.assertion));
const binding = role => role ? props.bindings.relations[role] : props.bindings;
const clearFields = role => Object.keys(binding(role).fields).forEach(key => { binding(role).fields[key] = ''; });
</script>
<style scoped>
.assertion-bindings { display: grid; gap: 12px; }
.assertion-bindings .el-select { width: 100%; }
</style>
