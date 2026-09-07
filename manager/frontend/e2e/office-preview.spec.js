import { expect, test } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import JSZip from 'jszip'

const ENGINE = {
  id: 12,
  name: 'Business NFS',
  engine_type: 'nfs',
  lifecycle_state: 'active',
  connection_status: 'online'
}

const ROOT_LOCATOR = 'addp://engine/12/path/?type=root&node_id=200'
const DOC_LOCATOR = 'addp://engine/12/path/doc?type=directory&node_id=220'
const WORD_BINARY = readFileSync(resolve(
  import.meta.dirname,
  '../../../business/nfs/data/doc/wps.wps'
))
const DOCX_BINARY = await createDocxFixture()
const RTF_BINARY = Buffer.from(
  String.raw`{\rtf1\ansi\ansicpg936 \'d6\'d0\'d0\'c5\'b3\'f6\'b0\'e6\'c9\'e7\'ba\'cf\'bc\'af.txt\par \u21019?\u19994?\u22312?\u36335?\u19978?.epub}`,
  'ascii'
)

async function createDocxFixture() {
  const archive = new JSZip()
  archive.file('[Content_Types].xml', `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`)
  archive.folder('_rels').file('.rels', `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`)
  archive.folder('word').file('document.xml', `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>数据治理100问</w:t></w:r></w:p>
    <w:p><w:r><w:t>为什么要写《数据治理100问》</w:t></w:r></w:p>
    <w:sectPr/>
  </w:body>
</w:document>`)
  return archive.generateAsync({ type: 'nodebuffer', compression: 'DEFLATE' })
}

const CASES = [
  {
    kind: 'rtf',
    name: 'ZX书单.rtf',
    itemID: 1201,
    bytes: RTF_BINARY,
    expected: ['中信出版社合集.txt', '创业在路上.epub']
  },
  {
    kind: 'wps',
    name: '关于时空底座.wps',
    itemID: 1202,
    bytes: WORD_BINARY,
    expected: ['如果站高一点拉出来看', '超图想进入某个行业赛道']
  },
  {
    // WPS and legacy DOC use the same Word Binary File Format parser.
    kind: 'doc',
    name: 'legacy-word.doc',
    itemID: 1203,
    bytes: WORD_BINARY,
    expected: ['如果站高一点拉出来看', '超图想进入某个行业赛道']
  },
  {
    kind: 'docx',
    name: '000-为什么要写这本书.docx',
    itemID: 1204,
    bytes: DOCX_BINARY,
    expected: ['数据治理100问', '为什么要写《数据治理100问》']
  }
].map(item => ({
  ...item,
  locator: `addp://engine/${ENGINE.id}/path/doc/${item.name}?type=file&item_id=${item.itemID}`
}))

for (const documentCase of CASES) {
  test(`previews ${documentCase.kind.toUpperCase()} content without replacement characters`, async ({ page }) => {
    const browserErrors = []
    page.on('console', message => {
      if (message.type() === 'error') browserErrors.push(message.text())
    })
    page.on('pageerror', error => browserErrors.push(error.message))

    await installMockBackend(page, documentCase)
    await page.goto(`/data-explorer?locator=${encodeURIComponent(documentCase.locator)}`)

    const toolbar = page.getByRole('toolbar', { name: '文件预览工具栏' })
    const viewer = page.locator('.office-viewer-host')
    await expect(toolbar).toBeVisible()
    for (const expected of documentCase.expected) {
      await expect(viewer).toContainText(expected)
    }
    await expect(viewer).not.toContainText('�')
    expect(browserErrors).toEqual([])
  })
}

test('Office preview search and zoom controls operate on rendered content', async ({ page }) => {
  const documentCase = CASES.find(item => item.kind === 'docx')
  await installMockBackend(page, documentCase)
  await page.goto(`/data-explorer?locator=${encodeURIComponent(documentCase.locator)}`)

  const viewer = page.locator('.office-viewer-host')
  await expect(viewer).toContainText(documentCase.expected[0])

  await page.getByPlaceholder('搜索预览文本').fill('数据治理')
  await expect(page.locator('mark[data-addp-office-search]').first()).toBeVisible()
  await expect(page.locator('.office-search-count')).toContainText('处匹配')

  await expect(viewer).toHaveAttribute('style', /zoom:\s*1;/)
  await page.getByRole('button', { name: '放大', exact: true }).click()
  await expect(viewer).toHaveAttribute('style', /zoom:\s*1\.1;/)
  await expect(page.getByRole('button', { name: '重置缩放', exact: true })).toContainText('110%')
})

async function installMockBackend(page, documentCase) {
  const itemNode = fileNode(documentCase)
  const docNode = {
    id: DOC_LOCATOR,
    locator: DOC_LOCATOR,
    label: 'doc',
    type: 'directory',
    path: 'doc',
    hasChildren: true,
    loaded: true,
    children: [itemNode]
  }
  const rootNode = {
    id: ROOT_LOCATOR,
    locator: ROOT_LOCATOR,
    label: ENGINE.name,
    type: 'root',
    hasChildren: true,
    loaded: true,
    children: [docNode]
  }

  await page.addInitScript(() => {
    localStorage.setItem('addp-lang', 'zh-cn')
    localStorage.setItem('theme-mode', 'light')
  })

  await page.route('**/plugins/manifest.json', route => fulfillJSON(route, {
    scripts: ['/plugins/office-preview.js']
  }))
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname

    if (path === '/api/v1/system/refresh') {
      return fulfillJSON(route, { access_token: 'manager-e2e-token', expires_in: 3600 })
    }
    if (path === '/api/v1/system/users/me') {
      return fulfillJSON(route, { id: 1, username: 'manager-e2e' })
    }
    if (path === '/api/v1/system/auth/context') {
      return fulfillJSON(route, {
        context: { type: 'tenant' },
        authorization: { role_assignments: [{ permissions: [] }] }
      })
    }
    if (path === '/api/v1/manager/engines') {
      return fulfillJSON(route, { data: [ENGINE] })
    }
    if (path === `/api/v1/meta/resource-tree/${ENGINE.id}`) {
      return fulfillJSON(route, rootNode)
    }
    if (path === `/api/v1/meta/resource-tree/${ENGINE.id}/ancestors`) {
      return fulfillJSON(route, {
        target_locator: documentCase.locator,
        ancestors: [rootNode, docNode, itemNode]
      })
    }
    if (path === `/api/v1/meta/resource-tree/${ENGINE.id}/node`) {
      return fulfillJSON(route, {
        parent_locator: DOC_LOCATOR,
        children: [itemNode]
      })
    }
    if (path === '/api/v1/manager/preview') {
      return fulfillJSON(route, {
        preview_type: 'object',
        data: previewData(documentCase)
      })
    }

    return fulfillJSON(route, {})
  })
}

function fileNode(documentCase) {
  return {
    id: documentCase.locator,
    locator: documentCase.locator,
    label: documentCase.name,
    type: 'file',
    path: `doc/${documentCase.name}`,
    children: [],
    metadata: {
      item_id: documentCase.itemID,
      data_type: 'document',
      format: documentCase.kind
    }
  }
}

function previewData(documentCase) {
  return {
    mode: 'object',
    object: {
      name: documentCase.name,
      path: `doc/${documentCase.name}`,
      extension: documentCase.kind,
      size_bytes: documentCase.bytes.byteLength,
      content: {
        kind: documentCase.kind,
        frontend_renderer: 'office',
        preview_material: 'raw_binary',
        encoding: 'base64',
        data: documentCase.bytes.toString('base64'),
        metadata: { format: documentCase.kind }
      }
    }
  }
}

async function fulfillJSON(route, body, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body)
  })
}
