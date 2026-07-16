import { Catalog } from '@a2ui/web_core/v0_9'
import { z } from 'zod'

import ClarificationChoice from './components/ClarificationChoice.vue'
import WorkflowDag from './components/WorkflowDag.vue'

export const ADDP_A2UI_CATALOG_ID = 'addp.catalog/v1'

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

export function createAddpCatalog() {
  return new Catalog(ADDP_A2UI_CATALOG_ID, [WorkflowDagApi, ClarificationChoiceApi])
}
