import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { hasDistinctText } from '../src/utils/displayText.js'

assert.equal(hasDistinctText('数据加载', '数据加载', 'load'), false)
assert.equal(hasDistinctText('LOAD', 'load'), false)
assert.equal(hasDistinctText('读取空间数据', '数据加载', 'load'), true)
assert.equal(hasDistinctText('', '数据加载'), false)

const queryEditor = readFileSync(resolve(import.meta.dirname, '../src/views/QueryEditor.vue'), 'utf8')
const queryResult = readFileSync(resolve(import.meta.dirname, '../src/components/QueryResult.vue'), 'utf8')
const app = readFileSync(resolve(import.meta.dirname, '../src/App.vue'), 'utf8')
const zhCnMessages = JSON.parse(readFileSync(resolve(import.meta.dirname, '../src/i18n/zh-cn.json'), 'utf8'))
const enMessages = JSON.parse(readFileSync(resolve(import.meta.dirname, '../src/i18n/en.json'), 'utf8'))
assert.equal(
  /SELECT\s+1\b/i.test(queryEditor),
  false,
  'QueryEditor must not replace executable engine samples with SELECT 1'
)
assert.match(queryEditor, /t\('develop\.query\.saveQuery'\)/)
assert.equal(queryEditor.includes("t('develop.query.updateTask')"), false)
assert.equal(queryEditor.includes("t('develop.query.saveAsTask')"), false)
assert.equal(zhCnMessages.develop.query.saveQuery, '保存查询')
assert.equal(enMessages.develop.query.saveQuery, 'Save Query')
assert.equal(zhCnMessages.develop.queryResult.rowsCount, '已加载行数')
assert.equal(enMessages.develop.queryResult.rowsCount, 'Rows loaded')
assert.equal(zhCnMessages.develop.queryResult.truncated, '结果已截断，仅加载前 {limit} 行')
assert.equal(enMessages.develop.queryResult.truncated, 'Result truncated; only the first {limit} rows were loaded')
assert.match(queryResult, /result\.effect !== 'read' && result\.rows_affected !== undefined && result\.rows_affected !== null/)
assert.match(queryResult, /<DataPagination/)
assert.match(queryResult, /<TabularResultRenderer/)
assert.match(queryResult, /:rows="pagedRows"/)
assert.match(queryResult, /v-model:current-page="currentPage"/)
assert.match(queryResult, /:page-sizes="pageSizeOptions"/)
assert.match(queryResult, /\.result-table\s*\{[^}]*display:\s*flex;[^}]*overflow:\s*hidden;/s)
assert.equal(queryResult.includes('<el-table-v2'), false)
assert.equal(queryResult.includes('<el-pagination'), false)
assert.match(app, /<el-config-provider :locale="elementLocale">/)
assert.match(app, /locale\.value === 'zh-cn' \? zhCn : enLocale/)
assert.ok(queryEditor.indexOf('class="engine-select"') < queryEditor.indexOf('class="query-task-name"'))

console.log('displayText tests passed')
