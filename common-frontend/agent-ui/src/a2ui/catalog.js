import { Catalog } from '@a2ui/web_core/v0_9'
import { z } from 'zod'

import ClarificationChoice from './components/ClarificationChoice.vue'
import ApprovalRequest from './components/ApprovalRequest.vue'
import MapView from './components/MapView.vue'
import ResourcePicker from './components/ResourcePicker.vue'
import TablePreview from './components/TablePreview.vue'
import WorkflowDag from './components/WorkflowDag.vue'

export const ADDP_A2UI_CATALOG_ID = 'addp.catalog/v1'
const A2UI_COMPONENT_MAX_BYTES = 500 * 1024

const WorkflowDagApi = {
  name: 'WorkflowDag',
  schema: z.object({
    workflow: z.record(z.string(), z.unknown()),
    height: z.number().int().min(240).max(800).optional()
  }),
  render: WorkflowDag
}

const ClarificationChoiceApi = {
  name: 'ClarificationChoice',
  schema: z.object({
    interactionId: z.string().uuid(),
    prompt: z.string().min(1),
    options: z.array(z.object({
      label: z.string(),
      value: z.unknown(),
      candidate: z.record(z.string(), z.unknown()).optional()
    }))
  }),
  render: ClarificationChoice
}

const ApprovalRequestApi = {
  name: 'ApprovalRequest',
  schema: z.object({
    interactionId: z.string().uuid(),
    owner: z.string().min(1),
    ownerInteractionId: z.string().min(1),
    openUrl: z.string().min(1),
    requestFingerprint: z.string().regex(/^[0-9a-f]{64}$/),
    requestSummary: z.record(z.string(), z.unknown()),
    expiresAt: z.string().nullable().optional()
  }),
  render: ApprovalRequest
}

const JsonScalar = z.union([z.string().max(2000), z.number().finite(), z.boolean(), z.null()])

const TablePreviewApi = {
  name: 'TablePreview',
  schema: z.object({
    columns: z.array(z.string().min(1).max(200)).max(50),
    rows: z.array(z.record(z.string(), JsonScalar)).max(100),
    total: z.number().int().min(0),
    truncated: z.boolean()
  }).strict(),
  render: TablePreview
}

function boundedCoordinates(value, depth = 0, state = { count: 0 }) {
  if (!Array.isArray(value) || value.length === 0 || depth > 4) return false
  for (const item of value) {
    if (typeof item === 'number') {
      if (!Number.isFinite(item)) return false
      state.count += 1
    } else if (!boundedCoordinates(item, depth + 1, state)) {
      return false
    }
    if (state.count > 5000) return false
  }
  return true
}

const GeoJsonGeometry = z.object({
  type: z.enum(['Point', 'MultiPoint', 'LineString', 'MultiLineString', 'Polygon', 'MultiPolygon']),
  coordinates: z.unknown()
}).strict().refine(geometry => boundedCoordinates(geometry.coordinates), {
  message: 'geometry coordinates must be finite and bounded'
})

const MapViewApi = {
  name: 'MapView',
  schema: z.object({
    crs: z.literal('EPSG:4326'),
    features: z.array(z.object({
      type: z.literal('Feature'),
      id: z.union([z.string(), z.number()]).optional(),
      geometry: GeoJsonGeometry,
      properties: z.record(z.string().max(200), JsonScalar).refine(value => Object.keys(value).length <= 50, {
        message: 'feature properties exceed 50 fields'
      })
    }).strict()).max(200),
    height: z.number().int().min(240).max(600).optional(),
    truncated: z.boolean()
  }).strict().refine(value => {
    const state = { count: 0 }
    return value.features.every(feature => boundedCoordinates(feature.geometry.coordinates, 0, state))
  }, {
    message: 'map coordinates exceed 5000 finite values'
  }),
  render: MapView
}

const ResourcePickerApi = {
  name: 'ResourcePicker',
  schema: z.object({
    interactionId: z.string().uuid(),
    prompt: z.string().min(1).max(2000),
    options: z.array(z.object({
      label: z.string().min(1).max(500),
      value: z.string().max(2048).startsWith('addp://'),
      candidate: z.object({
        locator: z.string().max(2048).startsWith('addp://'),
        engine_id: z.number().int().positive().optional(),
        name: z.string().max(2000).optional(),
        full_name: z.string().max(2000).optional(),
        asset_type: z.string().max(2000).optional(),
        item_type: z.string().max(2000).optional()
      }).strict()
    }).strict().refine(option => option.value === option.candidate.locator, {
      message: 'resource option value must equal candidate locator'
    })).min(1).max(50)
  }).strict(),
  render: ResourcePicker
}

export function validateCatalogComponent(catalog, component) {
  const implementation = catalog.components.get(component.type)
  if (!implementation) {
    throw new Error(`A2UI component is not registered: ${component.type}`)
  }
  const result = implementation.schema.safeParse(component.properties)
  if (!result.success) {
    throw new Error(`A2UI component validation failed: ${component.type}: ${result.error.message}`)
  }
  if (new TextEncoder().encode(JSON.stringify(component.properties)).length > A2UI_COMPONENT_MAX_BYTES) {
    throw new Error(`A2UI component validation failed: ${component.type}: component exceeds ${A2UI_COMPONENT_MAX_BYTES} bytes`)
  }
}

export function createAddpCatalog() {
  return new Catalog(ADDP_A2UI_CATALOG_ID, [
    WorkflowDagApi,
    ClarificationChoiceApi,
    ApprovalRequestApi,
    MapViewApi,
    TablePreviewApi,
    ResourcePickerApi
  ])
}
