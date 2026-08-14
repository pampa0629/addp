import test from 'node:test'
import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'

const srcRoot = new URL('../src/', import.meta.url)

const collectVueFiles = async (directory) => {
  const entries = await readdir(directory, { withFileTypes: true })
  const files = await Promise.all(entries.map(async (entry) => {
    const url = new URL(entry.name, directory)
    if (entry.isDirectory()) {
      return collectVueFiles(new URL(`${entry.name}/`, directory))
    }
    return entry.name.endsWith('.vue') ? [url] : []
  }))
  return files.flat()
}

test('model dialogs use the shared responsive dialog contract', async () => {
  const dialogTags = []

  for (const file of await collectVueFiles(srcRoot)) {
    const source = await readFile(file, 'utf8')
    for (const match of source.matchAll(/<el-dialog\b[\s\S]*?>/g)) {
      dialogTags.push({ file: file.pathname, tag: match[0] })
    }
  }

  assert.ok(dialogTags.length > 0)
  for (const { file, tag } of dialogTags) {
    assert.match(tag, /class="addp-dialog"/, `${file} must use addp-dialog`)
    assert.match(
      tag,
      /width="min\(\d+px, calc\(100vw - \d+px\)\)"/,
      `${file} must constrain its dialog width to the viewport`
    )
  }
})
