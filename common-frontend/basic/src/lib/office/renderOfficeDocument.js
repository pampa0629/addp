import DOMPurify from 'dompurify'
import * as mammoth from 'mammoth'
import { parseLegacyWordDocument, renderLegacyWordDocument } from './legacyWord'
import { extractRtfText } from './rtf'

function renderPlainText(container, text, className) {
  const article = document.createElement('article')
  article.className = `addp-office-document ${className}`
  const content = document.createElement('pre')
  content.className = 'addp-office-plain-text'
  content.textContent = text
  article.append(content)
  container.replaceChildren(article)
}

async function renderDocx(container, arrayBuffer) {
  const result = await mammoth.convertToHtml(
    { arrayBuffer },
    {
      convertImage: mammoth.images.imgElement(async image => ({
        src: `data:${image.contentType};base64,${await image.read('base64')}`
      }))
    }
  )
  const article = document.createElement('article')
  article.className = 'addp-office-document addp-office-docx'
  article.innerHTML = DOMPurify.sanitize(result.value, {
    USE_PROFILES: { html: true },
    ADD_DATA_URI_TAGS: ['img']
  })
  container.replaceChildren(article)
}

export async function renderOfficeDocument(container, arrayBuffer, kind) {
  if (!(container instanceof HTMLElement)) {
    throw new TypeError('Office preview container is required')
  }

  switch (String(kind || '').trim().toLowerCase()) {
    case 'docx':
      await renderDocx(container, arrayBuffer)
      return
    case 'rtf':
      renderPlainText(container, extractRtfText(arrayBuffer), 'addp-office-rtf')
      return
    case 'doc':
    case 'wps':
      renderLegacyWordDocument(container, parseLegacyWordDocument(arrayBuffer))
      return
    default:
      throw new Error(`Unsupported Office document kind: ${kind}`)
  }
}
