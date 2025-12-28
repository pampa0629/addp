<template>
  <div class="data-query-container">
    <el-card>
      <template #header>
        <h3>数据查询</h3>
      </template>

      <el-form :model="queryForm" label-width="120px">
        <el-form-item label="引擎ID">
          <el-input-number v-model="queryForm.resource_id" :min="1" placeholder="请输入引擎ID" />
        </el-form-item>

        <el-form-item label="Schema">
          <el-input v-model="queryForm.schema" placeholder="例如: public" />
        </el-form-item>

        <el-form-item label="表名">
          <el-input v-model="queryForm.table" placeholder="请输入表名" />
        </el-form-item>

        <el-form-item label="查询字段">
          <el-input
            v-model="queryForm.columns"
            placeholder="用逗号分隔，留空查询所有字段"
          />
          <div class="form-tip">例如: id, name, city</div>
        </el-form-item>

        <el-form-item label="过滤条件">
          <el-input
            v-model="queryForm.filter"
            type="textarea"
            :rows="2"
            placeholder="SQL WHERE 条件，不包含 WHERE 关键字"
          />
          <div class="form-tip">例如: population > 1000000 AND city = 'Beijing'</div>
        </el-form-item>

        <el-form-item label="排序">
          <el-input v-model="queryForm.order_by" placeholder="例如: name ASC, id DESC" />
        </el-form-item>

        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="页码">
              <el-input-number v-model="queryForm.page" :min="1" style="width: 100%" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="每页数量">
              <el-input-number v-model="queryForm.page_size" :min="1" :max="1000" style="width: 100%" />
            </el-form-item>
          </el-col>
        </el-row>

        <el-form-item label="包含几何">
          <el-switch v-model="queryForm.geometry" />
        </el-form-item>

        <el-form-item v-if="queryForm.geometry" label="几何格式">
          <el-radio-group v-model="queryForm.geometry_type">
            <el-radio label="geojson">GeoJSON</el-radio>
            <el-radio label="wkt">WKT</el-radio>
          </el-radio-group>
        </el-form-item>

        <el-form-item>
          <el-button type="primary" :loading="querying" @click="handleQuery">查询</el-button>
          <el-button @click="handleReset">重置</el-button>
          <el-button v-if="queryResult" @click="handleExport">导出结果</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card v-if="queryResult" style="margin-top: 20px">
      <template #header>
        <div style="display: flex; justify-content: space-between; align-items: center">
          <span>查询结果 (共 {{ queryResult.total }} 条)</span>
          <span style="font-size: 14px; color: #909399">
            耗时: {{ queryResult.duration || 0 }}ms
          </span>
        </div>
      </template>

      <el-table :data="queryResult.rows" border max-height="500">
        <el-table-column
          v-for="col in queryResult.columns"
          :key="col.name"
          :prop="col.name"
          :label="col.name"
          :width="col.name.length > 20 ? 200 : undefined"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ formatValue(row[col.name]) }}
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-if="queryResult.total > queryForm.page_size"
        v-model:current-page="queryForm.page"
        :page-size="queryForm.page_size"
        :total="queryResult.total"
        layout="prev, pager, next, total"
        style="margin-top: 20px; justify-content: flex-end"
        @current-change="handleQuery"
      />
    </el-card>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import dataAPI from '../api/data'

const querying = ref(false)
const queryForm = ref({
  engine_id: null,
  schema: 'public',
  table: '',
  columns: '',
  filter: '',
  order_by: '',
  page: 1,
  page_size: 20,
  geometry: false,
  geometry_type: 'geojson'
})

const queryResult = ref(null)

const handleQuery = async () => {
  if (!queryForm.value.resource_id || !queryForm.value.table) {
    ElMessage.warning('请输入引擎ID和表名')
    return
  }

  try {
    querying.value = true

    const requestData = {
      engine_id: queryForm.value.resource_id,
      schema: queryForm.value.schema,
      table: queryForm.value.table,
      page: queryForm.value.page,
      page_size: queryForm.value.page_size
    }

    if (queryForm.value.columns) {
      requestData.columns = queryForm.value.columns.split(',').map(c => c.trim()).filter(c => c)
    }

    if (queryForm.value.filter) {
      requestData.filter = queryForm.value.filter
    }

    if (queryForm.value.order_by) {
      requestData.order_by = queryForm.value.order_by
    }

    if (queryForm.value.geometry) {
      requestData.geometry = true
      requestData.geometry_type = queryForm.value.geometry_type
    }

    const { data } = await dataAPI.query(requestData)
    queryResult.value = data
    ElMessage.success('查询成功')
  } catch (error) {
    ElMessage.error('查询失败: ' + (error.response?.data?.message || error.message))
  } finally {
    querying.value = false
  }
}

const handleReset = () => {
  queryForm.value = {
    engine_id: null,
    schema: 'public',
    table: '',
    columns: '',
    filter: '',
    order_by: '',
    page: 1,
    page_size: 20,
    geometry: false,
    geometry_type: 'geojson'
  }
  queryResult.value = null
}

const handleExport = () => {
  if (!queryResult.value) return

  const csvContent = [
    // 表头
    queryResult.value.columns.map(col => col.name).join(','),
    // 数据行
    ...queryResult.value.rows.map(row =>
      queryResult.value.columns.map(col => {
        const value = row[col.name]
        if (value === null || value === undefined) return ''
        if (typeof value === 'object') return JSON.stringify(value)
        return String(value).includes(',') ? `"${value}"` : value
      }).join(',')
    )
  ].join('\n')

  const blob = new Blob(['\ufeff' + csvContent], { type: 'text/csv;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `query-result-${Date.now()}.csv`
  a.click()
  URL.revokeObjectURL(url)
  ElMessage.success('导出成功')
}

const formatValue = (value) => {
  if (value === null || value === undefined) return '-'
  if (typeof value === 'object') return JSON.stringify(value)
  if (typeof value === 'string' && value.length > 100) {
    return value.substring(0, 100) + '...'
  }
  return String(value)
}
</script>

<style scoped>
.data-query-container {
  padding: 24px;
}

.form-tip {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
}

h3 {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
}
</style>
