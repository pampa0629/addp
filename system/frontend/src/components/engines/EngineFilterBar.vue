<template>
  <div class="filter-bar">
    <el-form :inline="true">
      <el-form-item label="引擎类型">
        <el-select
          v-model="selectedEngineType"
          placeholder="全部"
          clearable
          style="width: 200px"
          @change="$emit('update:selectedEngineType', selectedEngineType)"
        >
          <el-option label="全部" value="" />
          <el-option label="PostgreSQL" value="postgresql" />
          <el-option label="MySQL" value="mysql" />
          <el-option label="Doris" value="doris" />
          <el-option label="ClickHouse" value="clickhouse" />
          <el-option label="MongoDB" value="mongodb" />
          <el-option label="Spark SQL" value="spark_sql" />
          <el-option label="MinIO" value="minio" />
          <el-option label="S3" value="s3" />
        </el-select>
      </el-form-item>

      <el-form-item label="能力分组">
        <el-select
          v-model="selectedCategory"
          placeholder="全部"
          clearable
          style="width: 150px"
          @change="$emit('update:selectedCategory', selectedCategory)"
        >
          <el-option label="全部" value="" />
          <el-option label="存储引擎" value="storage" />
          <el-option label="计算引擎" value="compute" />
        </el-select>
      </el-form-item>

      <el-form-item>
        <el-button type="primary" :icon="Search" @click="$emit('filter')">
          筛选
        </el-button>
        <el-button :icon="Refresh" @click="$emit('reset')">
          重置
        </el-button>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { Search, Refresh } from '@element-plus/icons-vue'

const props = defineProps({
  modelValueEngineType: {
    type: String,
    default: ''
  },
  modelValueCategory: {
    type: String,
    default: ''
  }
})

const emit = defineEmits(['update:selectedEngineType', 'update:selectedCategory', 'filter', 'reset'])

const selectedEngineType = ref(props.modelValueEngineType)
const selectedCategory = ref(props.modelValueCategory)

watch(() => props.modelValueEngineType, (val) => {
  selectedEngineType.value = val
})

watch(() => props.modelValueCategory, (val) => {
  selectedCategory.value = val
})
</script>

<style scoped>
.filter-bar {
  margin-bottom: 20px;
}
</style>
