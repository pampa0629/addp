// Preview geometry and the OL view both use EPSG:3857. The direct handoff
// preserves original doubles, including micro-segments. No geographic roundtrip.
export function webMercatorPreviewCoordinate([x, y]) {
  return [x, y]
}

// A GeometryCollection stays one feature; transform each member's coordinates.
export function webMercatorPreviewGeometry(geometry) {
  if (geometry.type === 'GeometryCollection') {
    return { type: geometry.type, geometries: geometry.geometries.map(webMercatorPreviewGeometry) }
  }
  const coordinates = value => typeof value[0] === 'number'
    ? webMercatorPreviewCoordinate(value) : value.map(coordinates)
  return { type: geometry.type, coordinates: coordinates(geometry.coordinates) }
}
