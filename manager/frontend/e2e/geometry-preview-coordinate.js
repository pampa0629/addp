// Independent spherical-Mercator oracle for the preview's geographic handoff
// followed by the map projection. Source EWKB still uses exact original axes.
// Do not round these doubles or replace the comparison with an epsilon.
export function webMercatorPreviewCoordinate([x, y]) {
  const radius = 6378137
  const longitude = x / radius * (180 / Math.PI)
  const latitude = (Math.PI / 2 - 2 * Math.atan(Math.exp(-y / radius))) * (180 / Math.PI)
  return [Math.PI * radius * longitude / 180,
    radius * Math.log(Math.tan(Math.PI * (latitude + 90) / 360))]
}
