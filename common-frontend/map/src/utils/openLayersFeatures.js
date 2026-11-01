import { markRaw, toRaw } from 'vue'
import Feature from 'ol/Feature.js'
import GeoJSON from 'ol/format/GeoJSON.js'

const originals = new WeakMap()

/** Read geometry under a collision-free internal key; keep every business key unchanged. */
export function readMapFeatures(features, options) {
  return features.map((original) => {
    const properties = original.properties || {}
    let geometryName = '__addpGeometry'
    while (Object.hasOwn(properties, geometryName)) geometryName += '_'
    const feature = markRaw(new GeoJSON({ geometryName }).readFeature(original, options))
    originals.set(feature, original)
    return feature
  })
}

export function mapFeatureOriginal(feature) {
  return originals.get(toRaw(feature))
}

/** Feature stores geometry as a property; MVT RenderFeature stores it separately. */
export function mapFeatureProperties(feature) {
  const properties = { ...feature.getProperties() }
  const geometryName = feature.getGeometryName?.()
  if (geometryName !== undefined) delete properties[geometryName]
  return properties
}

/** OL Feature.clone constructs default geometry before applying a custom geometry name. */
export function cloneMapFeature(feature) {
  const clone = markRaw(new Feature())
  clone.setGeometryName(feature.getGeometryName())
  clone.setProperties(mapFeatureProperties(feature))
  clone.setGeometry(feature.getGeometry()?.clone())
  clone.setStyle(feature.getStyle())
  const original = mapFeatureOriginal(feature)
  if (original) originals.set(clone, original)
  return clone
}
