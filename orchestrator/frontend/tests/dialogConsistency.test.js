import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import test from 'node:test'

async function collectVueSources(directory) {
  const entries = await readdir(directory, { withFileTypes: true })
  const sources = []

  for (const entry of entries) {
    const path = resolve(directory, entry.name)
    if (entry.isDirectory()) {
      sources.push(...(await collectVueSources(path)))
    } else if (entry.name.endsWith('.vue')) {
      sources.push({ path, source: await readFile(path, 'utf8') })
    }
  }

  return sources
}

test('all orchestrator dialogs follow the shared contract', async () => {
  const vueSources = await collectVueSources(resolve('src'))

  for (const { path, source } of vueSources) {
    for (const dialogTag of source.match(/<el-dialog\b[\s\S]*?>/g) || []) {
      assert.match(dialogTag, /class="[^"]*\baddp-dialog\b[^"]*"/, `${path} 的 el-dialog 未接入 addp-dialog`)
      assert.match(dialogTag, /\bwidth="[^"]*calc\(100vw - 24px\)[^"]*"/, `${path} 的 el-dialog 缺少窄窗口宽度上限`)
    }

    if (source.includes('ElMessageBox.confirm(')) {
      assert.match(source, /addp-message-box/, `${path} 的确认框未接入 addp-message-box`)
      assert.match(source, /confirmButtonText:/, `${path} 的确认框未显式配置确认文案`)
      assert.match(source, /cancelButtonText:/, `${path} 的确认框未显式配置取消文案`)
    }
  }
})

test('orchestrator dialogs use shared responsive hierarchy', async () => {
  const form = await readFile(resolve('src/views/OrchestrationForm.vue'), 'utf8')
  const executionList = await readFile(resolve('src/views/ExecutionList.vue'), 'utf8')
  const executionRecords = await readFile(resolve('src/views/ExecutionRecords.vue'), 'utf8')

  assert.equal((form.match(/class="addp-dialog"/g) || []).length, 3)
  assert.doesNotMatch(form, /width="(?:520px|720px|60%)"/)
  assert.equal((form.match(/label-position="top"/g) || []).length, 2)
  assert.match(form, /jsonDialogClose/)
  assert.ok(form.indexOf('jsonDialogClose') < form.indexOf('downloadJsonBtn'))
  assert.ok(form.indexOf('downloadJsonBtn') < form.indexOf('copyJsonBtn'))
  assert.match(form, /height: clamp\(260px, 55vh, 520px\)/)
  assert.doesNotMatch(form, /\.json-actions/)
  assert.match(executionList, /class="addp-dialog"[\s\S]*width="min\(800px, calc\(100vw - 24px\)\)"/)
  assert.match(executionRecords, /class="addp-dialog"[\s\S]*width="min\(800px, calc\(100vw - 24px\)\)"/)
})

test('orchestrator confirmations use localized destructive actions', async () => {
  const dagEditor = await readFile(resolve('src/components/DAGEditor.vue'), 'utf8')
  const orchestrationList = await readFile(resolve('src/views/OrchestrationList.vue'), 'utf8')
  const zhCn = JSON.parse(await readFile(resolve('src/i18n/zh-cn.json'), 'utf8'))
  const en = JSON.parse(await readFile(resolve('src/i18n/en.json'), 'utf8'))

  for (const source of [dagEditor, orchestrationList]) {
    assert.match(source, /customClass: 'addp-message-box'/)
    assert.match(source, /confirmButtonClass: 'el-button--danger'/)
    assert.match(source, /confirmButtonText:/)
    assert.match(source, /cancelButtonText:/)
  }

  assert.equal(zhCn.orchestrator.dagEditor.clearConfirmAction, '清空')
  assert.equal(en.orchestrator.dagEditor.clearConfirmAction, 'Clear')
  assert.equal(zhCn.orchestrator.orchestrationList.deleteConfirmAction, '删除')
  assert.equal(en.orchestrator.orchestrationList.deleteConfirmAction, 'Delete')
})

test('orchestration execution requires confirmation and locks duplicate submissions', async () => {
  const source = await readFile(resolve('src/views/OrchestrationList.vue'), 'utf8')
  const zhCn = JSON.parse(await readFile(resolve('src/i18n/zh-cn.json'), 'utf8'))
  const en = JSON.parse(await readFile(resolve('src/i18n/en.json'), 'utf8'))

  assert.match(source, /:loading="executingId === scope\.row\.id"/)
  assert.match(source, /:disabled="executingId !== null"/)
  assert.match(source, /const executingId = ref\(null\)/)
  assert.match(source, /orchestrationList\.executeConfirmTitle/)
  assert.match(source, /orchestrationList\.executeConfirmMessage/)
  assert.match(source, /orchestrationList\.executeConfirmAction/)
  assert.match(source, /orchestrationList\.executeConfirmCancel/)
  assert.ok(source.indexOf('ElMessageBox.confirm(') < source.indexOf('orchestrationAPI.execute(row.id)'))

  assert.equal(zhCn.orchestrator.orchestrationList.executeConfirmAction, '执行')
  assert.equal(en.orchestrator.orchestrationList.executeConfirmAction, 'Execute')
})
