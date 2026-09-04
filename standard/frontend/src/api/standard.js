// Standard 模块 API（仅包含 Standard 模块自己的功能）
import client from './client'

// 注意：业务实体(entities)、逻辑表(logical-tables)、数仓分层(dw-layers)
// 属于 Model 模块，如需调用请使用跨模块调用

// ========== 业务域 API ==========
export const domainAPI = {
  list() { return client.get('/standard/domains') },
  create(data) { return client.post('/standard/domains', data) },
  get(id) { return client.get(`/standard/domains/${id}`) },
  update(id, data) { return client.put(`/standard/domains/${id}`, data) },
  delete(id, version) { return client.delete(`/standard/domains/${id}`, { data: { version } }) }
}

// ========== 标准集 API ==========
export const standardCollectionAPI = {
  list(params) { return client.get('/standard/collections', { params }) },
  create(data) { return client.post('/standard/collections', data) },
  get(id) { return client.get(`/standard/collections/${id}`) },
  delete(id, version) { return client.delete(`/standard/collections/${id}`, { data: { version } }) },
  listRevisions(id) { return client.get(`/standard/collections/${id}/revisions`) },
  listEvents(id, params) { return client.get(`/standard/collections/${id}/events`, { params }) },
  createRevision(id, data) { return client.post(`/standard/collections/${id}/revisions`, data) },
  updateRevision(id, revisionId, data) { return client.put(`/standard/collections/${id}/revisions/${revisionId}`, data) },
  submitRevision(id, revisionId, version) { return client.post(`/standard/collections/${id}/revisions/${revisionId}/submit`, { version }) },
  returnRevision(id, revisionId, version) { return client.post(`/standard/collections/${id}/revisions/${revisionId}/return`, { version }) },
  publishRevision(id, revisionId, version) { return client.post(`/standard/collections/${id}/revisions/${revisionId}/publish`, { version }) },
  replaceAssignments(id, data) { return client.put(`/standard/collections/${id}/assignments`, data) },
  listUserCandidates(params) { return client.get('/standard/collection-user-candidates', { params }) }
}

// ========== 业务术语 API ==========
export const glossaryAPI = {
  list(params) { return client.get('/standard/glossaries', { params }) },
  create(data) { return client.post('/standard/glossaries', data) },
  get(id) { return client.get(`/standard/glossaries/${id}`) },
  update(id, data) { return client.put(`/standard/glossaries/${id}`, data) },
  delete(id) { return client.delete(`/standard/glossaries/${id}`) },
  approve(id, version) { return client.post(`/standard/glossaries/${id}/approve`, { version }) },
  deprecate(id, version) { return client.post(`/standard/glossaries/${id}/deprecate`, { version }) },
  getElements(id) { return client.get(`/standard/glossaries/${id}/elements`) }
}

// ========== 数据元 API ==========
export const elementAPI = {
  list(params) { return client.get('/standard/elements', { params }) },
  create(data) { return client.post('/standard/elements', data) },
  get(id) { return client.get(`/standard/elements/${id}`) },
  update(id, data) { return client.put(`/standard/elements/${id}`, data) },
  delete(id) { return client.delete(`/standard/elements/${id}`) },
  listRevisions(id) { return client.get(`/standard/elements/${id}/revisions`) },
  createRevision(id, data) { return client.post(`/standard/elements/${id}/revisions`, data) },
  getRevision(id, revisionId) { return client.get(`/standard/elements/${id}/revisions/${revisionId}`) },
  updateRevision(id, revisionId, data) { return client.put(`/standard/elements/${id}/revisions/${revisionId}`, data) },
  submitRevision(id, revisionId, version) { return client.post(`/standard/elements/${id}/revisions/${revisionId}/submit`, { version }) },
  returnRevision(id, revisionId, version) { return client.post(`/standard/elements/${id}/revisions/${revisionId}/return`, { version }) },
  publishRevision(id, revisionId, version) { return client.post(`/standard/elements/${id}/revisions/${revisionId}/publish`, { version }) },
  withdrawRevision(id, revisionId, version) { return client.post(`/standard/elements/${id}/revisions/${revisionId}/withdraw`, { version }) },
  getQualityRules(id) { return client.get(`/standard/elements/${id}/quality-rules`) }
}

// ========== 码值集 API ==========
export const codeSetAPI = {
  list(params) { return client.get('/standard/code-sets', { params }) },
  create(data) { return client.post('/standard/code-sets', data) },
  get(id) { return client.get(`/standard/code-sets/${id}`) },
  update(id, data) { return client.put(`/standard/code-sets/${id}`, data) },
  delete(id) { return client.delete(`/standard/code-sets/${id}`) },
  listRevisions(id) { return client.get(`/standard/code-sets/${id}/revisions`) },
  createRevision(id, data) { return client.post(`/standard/code-sets/${id}/revisions`, data) },
  getRevision(id, revisionId) { return client.get(`/standard/code-sets/${id}/revisions/${revisionId}`) },
  updateRevision(id, revisionId, data) { return client.put(`/standard/code-sets/${id}/revisions/${revisionId}`, data) },
  submitRevision(id, revisionId, version) { return client.post(`/standard/code-sets/${id}/revisions/${revisionId}/submit`, { version }) },
  returnRevision(id, revisionId, version) { return client.post(`/standard/code-sets/${id}/revisions/${revisionId}/return`, { version }) },
  publishRevision(id, revisionId, version) { return client.post(`/standard/code-sets/${id}/revisions/${revisionId}/publish`, { version }) },
  withdrawRevision(id, revisionId, version) { return client.post(`/standard/code-sets/${id}/revisions/${revisionId}/withdraw`, { version }) },
  createItem(codeSetId, revisionId, data) { return client.post(`/standard/code-sets/${codeSetId}/revisions/${revisionId}/items`, data) },
  updateItem(codeSetId, revisionId, itemId, data) { return client.put(`/standard/code-sets/${codeSetId}/revisions/${revisionId}/items/${itemId}`, data) },
  deleteItem(codeSetId, revisionId, itemId, version) { return client.delete(`/standard/code-sets/${codeSetId}/revisions/${revisionId}/items/${itemId}`, { data: { version } }) }
}

// ========== 计量单位 API ==========
export const measurementCategoryAPI = {
  list() { return client.get('/standard/measurement-categories') },
  create(data) { return client.post('/standard/measurement-categories', data) },
  update(id, data) { return client.put(`/standard/measurement-categories/${id}`, data) },
  delete(id) { return client.delete(`/standard/measurement-categories/${id}`) }
}

export const unitAPI = {
  list(params) { return client.get('/standard/units', { params }) },
  get(id) { return client.get(`/standard/units/${id}`) },
  create(data) { return client.post('/standard/units', data) },
  update(id, data) { return client.put(`/standard/units/${id}`, data) },
  delete(id) { return client.delete(`/standard/units/${id}`) }
}

// ========== 指标 API ==========
export const metricCategoryAPI = {
  list() { return client.get('/standard/metric-categories') },
  create(data) { return client.post('/standard/metric-categories', data) },
  update(id, data) { return client.put(`/standard/metric-categories/${id}`, data) },
  delete(id) { return client.delete(`/standard/metric-categories/${id}`) }
}

export const metricAPI = {
  list(params) { return client.get('/standard/metrics', { params }) },
  get(id) { return client.get(`/standard/metrics/${id}`) },
  create(data) { return client.post('/standard/metrics', data) },
  update(id, data) { return client.put(`/standard/metrics/${id}`, data) },
  delete(id) { return client.delete(`/standard/metrics/${id}`) },
  approve(id, version) { return client.post(`/standard/metrics/${id}/approve`, { version }) },
  deprecate(id, version) { return client.post(`/standard/metrics/${id}/deprecate`, { version }) }
}

// ========== 标准文档 API ==========
export const documentAPI = {
  list(params) { return client.get('/standard/documents', { params }) },
  get(id) { return client.get(`/standard/documents/${id}`) },
  create(data) { return client.post('/standard/documents', data) },
  update(id, data) { return client.put(`/standard/documents/${id}`, data) },
  delete(id) { return client.delete(`/standard/documents/${id}`) },
  getMappings(id) { return client.get(`/standard/documents/${id}/mappings`) },
  setMappings(id, data) { return client.put(`/standard/documents/${id}/mappings`, data) },
  uploadFile(id, formData, version) { formData.append('version', String(version)); return client.post(`/standard/documents/${id}/upload`, formData, { headers: { 'Content-Type': 'multipart/form-data' } }) },
  download(id) { return client.get(`/standard/documents/${id}/download`, { responseType: 'blob' }) }
}

// ========== 标准项维度的文档关联 API ==========
export const elementDocumentAPI = {
  list(elementId) { return client.get(`/standard/elements/${elementId}/documents`) },
  create(elementId, data) { return client.post(`/standard/elements/${elementId}/documents`, data) },
  link(elementId, docId, version) { return client.post(`/standard/elements/${elementId}/documents/link`, { doc_id: docId, version }) },
  unlink(elementId, docId, version) { return client.delete(`/standard/elements/${elementId}/documents/${docId}`, { data: { version } }) },
  uploadFile(docId, formData, version) { return documentAPI.uploadFile(docId, formData, version) },
  download(docId) { return documentAPI.download(docId) }
}

export const glossaryDocumentAPI = {
  list(glossaryId) { return client.get(`/standard/glossaries/${glossaryId}/documents`) },
  create(glossaryId, data) { return client.post(`/standard/glossaries/${glossaryId}/documents`, data) },
  link(glossaryId, docId, version) { return client.post(`/standard/glossaries/${glossaryId}/documents/link`, { doc_id: docId, version }) },
  unlink(glossaryId, docId, version) { return client.delete(`/standard/glossaries/${glossaryId}/documents/${docId}`, { data: { version } }) },
  uploadFile(docId, formData, version) { return documentAPI.uploadFile(docId, formData, version) },
  download(docId) { return documentAPI.download(docId) }
}

export const metricDocumentAPI = {
  list(metricId) { return client.get(`/standard/metrics/${metricId}/documents`) },
  create(metricId, data) { return client.post(`/standard/metrics/${metricId}/documents`, data) },
  link(metricId, docId, version) { return client.post(`/standard/metrics/${metricId}/documents/link`, { doc_id: docId, version }) },
  unlink(metricId, docId, version) { return client.delete(`/standard/metrics/${metricId}/documents/${docId}`, { data: { version } }) },
  uploadFile(docId, formData, version) { return documentAPI.uploadFile(docId, formData, version) },
  download(docId) { return documentAPI.download(docId) }
}
