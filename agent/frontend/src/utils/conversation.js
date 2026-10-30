import { copyNavigationDraft } from '@common-ui/utils/navigationDraft.mjs'
import { sessionResults } from './workspace'

export function messageText(message) {
  if (message.role !== 'user' && message.role !== 'assistant') return ''
  if (Array.isArray(message.parts) && message.parts.length) {
    return message.parts.filter(part => part.type === 'text' && typeof part.text === 'string').map(part => part.text).join('\n\n')
  }
  return typeof message.content === 'string' ? message.content : ''
}

export const messageIdentity = message => String(message.protocol_message_id || message.id)

export function pendingSessionInteractions(messages) {
  const interactions = new Map()
  for (const message of messages) {
    for (const part of message.parts || []) {
      if (part.type === 'interaction_ref') interactions.set(part.interaction_id, { ...part, source: messageIdentity(message) })
    }
  }
  return [...interactions.values()].filter(part => part.status === 'pending')
}

export function conversationMarkdown(title, messages, t) {
  const sections = [`# ${String(title).replace(/[\r\n]/g, ' ')}`, t('agent.workspace.exportNotice')]
  for (const message of messages) {
    if (!['user', 'assistant'].includes(message.role)) continue
    const text = messageText(message)
    const results = sessionResults([message])
    if (!text && !results.length) continue
    sections.push(`## ${t(`agent.workspace.roles.${message.role}`)}`, text)
    if (results.length) sections.push(results.map(result => `- ${t('agent.workspace.exportSource')}: ${t(`agent.workspace.resultKinds.${result.kind}`)}`).join('\n'))
  }
  return sections.filter(Boolean).join('\n\n') + '\n'
}

// Only explicit fields of already persisted presentations are listed, never prose matching.
export function conversationResources(messages) {
  const items = new Map()
  for (const message of messages) {
    for (const part of message.parts || []) {
      if (part.type !== 'presentation_ref' || part.protocol !== 'a2ui' || part.interaction_id) continue
      for (const operation of part.content?.operations || []) {
        for (const component of operation.updateComponents?.components || []) {
          if (component.component !== 'TransferDraftSnapshot' || typeof component.parameters !== 'string') continue
          let parameters
          try { parameters = JSON.parse(component.parameters) } catch { continue }
          for (const locator of [parameters?.config?.source?.locator, parameters?.config?.target?.parent_locator]) {
            if (typeof locator === 'string' && locator.startsWith('addp://') && locator.length <= 2048) {
              items.set(locator, { locator, source: messageIdentity(message) })
            }
          }
        }
      }
    }
  }
  return [...items.values()]
}


export function createMessageDraft(text) {
  if (typeof text !== 'string' || !text.trim()) throw new Error('invalid_message_draft')
  return copyNavigationDraft({ kind: 'agent.message-draft/v1', text })
}
export function readMessageDraft(value) {
  if (!value || value.kind !== 'agent.message-draft/v1' || Object.keys(value).some(key => !['kind', 'text'].includes(key))) throw new Error('invalid_message_draft')
  return createMessageDraft(value.text).text
}
