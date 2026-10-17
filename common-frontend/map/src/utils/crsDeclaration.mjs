export const TEMPORARY_SOURCE_CRS_OPTIONS = Object.freeze(['EPSG:4326', 'EPSG:3857'])

export const canDeclareSourceCRS = (sourceCode, transformStatus) => (
  transformStatus !== 'engine_transformed' && (transformStatus === 'unknown_crs' || !sourceCode)
)

export const resolvePreviewSourceCRSDeclaration = (sourceCode, transformStatus, selection = '') => {
  if (!canDeclareSourceCRS(sourceCode, transformStatus)) return sourceCode
  return TEMPORARY_SOURCE_CRS_OPTIONS.includes(selection) ? selection : ''
}
