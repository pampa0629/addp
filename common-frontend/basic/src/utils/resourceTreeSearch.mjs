export const minimumResourceTreeSearchLength = 2

export function isResourceTreeSearchReady(keyword) {
  return Array.from(String(keyword || '').trim()).length >= minimumResourceTreeSearchLength
}
