import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'

async function collectVueSources(directory) {
  const entries = await readdir(directory, { withFileTypes: true })
  const sources = []

  for (const entry of entries) {
    const url = new URL(entry.isDirectory() ? `${entry.name}/` : entry.name, directory)
    if (entry.isDirectory()) {
      sources.push(...(await collectVueSources(url)))
    } else if (entry.name.endsWith('.vue')) {
      sources.push({ path: decodeURIComponent(url.pathname), source: await readFile(url, 'utf8') })
    }
  }

  return sources
}

const [notebook, taskManagement, approvalDetail, saveQueryDialog, zhCnSource, enSource, sharedTheme, styleGuide] = await Promise.all([
  readFile(new URL('../src/views/NotebookEditor.vue', import.meta.url), 'utf8'),
  readFile(new URL('../src/views/TaskManagement.vue', import.meta.url), 'utf8'),
  readFile(new URL('../src/views/ApprovalDetail.vue', import.meta.url), 'utf8'),
  readFile(new URL('../src/components/SaveQueryDialog.vue', import.meta.url), 'utf8'),
  readFile(new URL('../src/i18n/zh-cn.json', import.meta.url), 'utf8'),
  readFile(new URL('../src/i18n/en.json', import.meta.url), 'utf8'),
  readFile(new URL('../../../common-frontend/basic/src/styles/theme.css', import.meta.url), 'utf8'),
  readFile(new URL('../../../common-frontend/docs/addp前端风格设计规范.md', import.meta.url), 'utf8')
])

const zhCn = JSON.parse(zhCnSource)
const en = JSON.parse(enSource)
const vueSources = await collectVueSources(new URL('../src/', import.meta.url))

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

assert.equal((notebook.match(/class="addp-dialog"/g) || []).length, 4)
assert.equal((notebook.match(/label-position="top"/g) || []).length, 4)
assert.doesNotMatch(notebook, /width="(?:520px|600px)"/)

assert.equal((taskManagement.match(/class="addp-dialog"/g) || []).length, 1)
assert.equal((taskManagement.match(/label-position="top"/g) || []).length, 1)
assert.doesNotMatch(taskManagement, /width="(?:500px|600px)"/)

for (const source of [notebook, taskManagement]) {
  assert.match(source, /customClass: 'addp-message-box'/)
  assert.match(source, /confirmButtonClass: 'el-button--danger'/)
  assert.match(source, /confirmButtonText:/)
  assert.match(source, /cancelButtonText:/)
}

assert.match(saveQueryDialog, /class="addp-dialog"/)
assert.match(saveQueryDialog, /width="min\(600px, calc\(100vw - 24px\)\)"/)
assert.match(saveQueryDialog, /label-position="top"/)
assert.match(saveQueryDialog, /dialogTitle:[\s\S]*initialValue:/)
assert.match(saveQueryDialog, /name: initialValue\.name \|\| ''/)
assert.match(saveQueryDialog, /tags: Array\.isArray\(initialValue\.tags\)/)

assert.match(approvalDetail, /customClass: 'addp-message-box'/)
assert.match(approvalDetail, /decision === 'rejected'[\s\S]*confirmButtonClass: 'el-button--danger'/)

assert.equal(zhCn.develop.notebook.deleteConfirmAction, '删除')
assert.equal(en.develop.notebook.deleteConfirmAction, 'Delete')
assert.equal(zhCn.develop.notebook.confirm, undefined)
assert.equal(en.develop.notebook.confirm, undefined)

assert.match(sharedTheme, /\.addp-dialog \.el-dialog__footer \{[\s\S]*justify-content: flex-end/)
assert.match(sharedTheme, /\.addp-message-box \.el-message-box__btns \{[\s\S]*justify-content: flex-end/)
assert.match(sharedTheme, /\.addp-dag-focus-region:focus-visible \{[\s\S]*box-shadow:/)
assert.match(styleGuide, /按钮默认作为一组整体右对齐/)
assert.match(styleGuide, /主操作始终位于最右侧/)
assert.match(styleGuide, /表单弹窗优先聚焦首个主要输入字段/)

console.log('dialog consistency tests passed')
