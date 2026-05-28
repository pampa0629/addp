/**
 * 预览组件入口
 *
 * 这些组件包含重型依赖（marked、mammoth、jszip、dompurify 等），
 * 单独提供入口，供需要预览功能的模块按需引入。
 *
 * 用法（以 manager 为例）：
 *   import { MarkdownPreview, PdfPreview } from '@common-ui/previews'
 *
 * 使用此入口的前端模块需要在 package.json 中安装以下依赖：
 *   - marked (MarkdownPreview)
 *   - dompurify (MarkdownPreview)
 *   - mermaid (MarkdownPreview)
 *   - jszip (DocxPreview, PptxPreview)
 *   - mammoth (DocxPreview)
 *
 * 并在 vite.config.js 的 resolve.dedupe 中添加这些包名。
 */

export { default as TextPreview } from './components/previews/TextPreview.vue'
export { default as UnsupportedPreview } from './components/previews/UnsupportedPreview.vue'
export { default as JsonPreview } from './components/previews/JsonPreview.vue'
export { default as MarkdownPreview } from './components/previews/MarkdownPreview.vue'
export { default as ContainerPreview } from './components/previews/ContainerPreview.vue'
export { default as VideoPreview } from './components/previews/VideoPreview.vue'
export { default as PdfPreview } from './components/previews/PdfPreview.vue'
export { default as DocxPreview } from './components/previews/DocxPreview.vue'
export { default as PptxPreview } from './components/previews/PptxPreview.vue'
export { default as ObjectCatalogPreview } from './components/previews/ObjectCatalogPreview.vue'
