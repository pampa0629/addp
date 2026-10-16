import { TRAJECTORY_POINT_LIMIT } from '@common-ui-map/utils/staticTrajectory.mjs'

export function canPreviewStaticTrajectory({ format, itemType, mode, childName, refPath }) {
  return mode === 'table' && ['file', 'object'].includes(itemType) &&
    ['csv', 'json', 'geojson'].includes(format) && !childName && !refPath
}

export async function readStaticTrajectoryTable(client, locator, signal) {
  const response = await client.get('/manager/preview', {
    params: { locator, page: 1, page_size: TRAJECTORY_POINT_LIMIT }, signal
  })
  return response?.preview_type === 'table' ? response.data : null
}
