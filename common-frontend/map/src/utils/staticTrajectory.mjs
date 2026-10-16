import { geometryValue } from './geoJSONResult.mjs'

export const TRAJECTORY_POINT_LIMIT = 2000

export class TrajectoryValidationError extends Error {
  constructor(code, row = null) {
    super(code)
    this.code = code
    this.row = row
  }
}
const fail = (code, row) => { throw new TrajectoryValidationError(code, row) }

export function validateTrajectoryTable(table) {
  if (!table || !Array.isArray(table.rows) || !Array.isArray(table.columns)) fail('not_table')
  if (table.page !== 1 || table.has_more !== false || table.object?.content?.truncated === true) fail('incomplete')
  if (table.rows.length > TRAJECTORY_POINT_LIMIT) fail('limit')
  if (table.total != null && (!Number.isSafeInteger(table.total) || table.total !== table.rows.length)) fail('incomplete')
  if (!table.rows.length) fail('empty')
  return table
}

function numeric(value, row) {
  if ((typeof value !== 'number' && typeof value !== 'string') || String(value).trim() === '') fail('coordinate', row)
  if (typeof value === 'string' && !/^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?$/i.test(value.trim())) fail('coordinate', row)
  const number = Number(value)
  if (!Number.isFinite(number)) fail('coordinate', row)
  return number
}

export function trajectoryTime(value, encoding, row) {
  let time
  if (encoding === 'unix_s' || encoding === 'unix_ms') {
    if ((typeof value !== 'number' && typeof value !== 'string') || !/^-?\d+(?:\.\d+)?$/.test(String(value))) fail('time', row)
    const text = String(value)
    const negative = text.startsWith('-')
    const [whole, rawFraction = ''] = (negative ? text.slice(1) : text).split('.')
    const fraction = rawFraction.replace(/0+$/, '')
    if (fraction.length > (encoding === 'unix_s' ? 3 : 0)) fail('time', row)
    const magnitude = Number(whole)
    if (!Number.isSafeInteger(magnitude)) fail('time', row)
    const milliseconds = encoding === 'unix_s' ? magnitude * 1000 + Number(fraction.padEnd(3, '0')) : magnitude
    time = (negative ? -1 : 1) * milliseconds
    if (!Number.isSafeInteger(time)) fail('time', row)
  } else if (encoding === 'iso') {
    const match = typeof value === 'string' && value.match(/^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,3}))?(Z|[+-]\d{2}:\d{2})$/i)
    if (!match) fail('time', row)
    const [, year, month, day, hour, minute, second, , zone] = match
    const y = Number(year), m = Number(month), d = Number(day)
    const leap = y % 4 === 0 && (y % 100 !== 0 || y % 400 === 0)
    const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
    if (m < 1 || m > 12 || d < 1 || d > days[m - 1] || Number(hour) > 23 || Number(minute) > 59 || Number(second) > 59) fail('time', row)
    if (zone.toUpperCase() !== 'Z' && (Number(zone.slice(1, 3)) > 23 || Number(zone.slice(4)) > 59)) fail('time', row)
    time = Date.parse(value)
  } else fail('configuration')
  if (!Number.isFinite(time) || Math.abs(time) > 8640000000000000) fail('time', row)
  return time
}

export function buildStaticTrajectory(table, config, transformGeometry) {
  validateTrajectoryTable(table)
  const fields = config?.coordinate_mode === 'point'
    ? [config.device_field, config.time_field, config.geometry_field]
    : [config?.device_field, config?.time_field, config?.x_field, config?.y_field]
  if (!['columns', 'point'].includes(config?.coordinate_mode) || fields.some(field => !field || !table.columns.includes(field)) || new Set(fields).size !== fields.length) fail('configuration')
  if (typeof transformGeometry !== 'function') fail('crs')
  const groups = new Map()
  table.rows.forEach((row, index) => {
    const rowNumber = index + 1
    const device = row?.[config.device_field]
    if (!['string', 'number'].includes(typeof device) || String(device).trim() === '' || (typeof device === 'number' && !Number.isSafeInteger(device))) fail('device', rowNumber)
    const time = trajectoryTime(row?.[config.time_field], config.time_encoding, rowNumber)
    let source
    if (config.coordinate_mode === 'point') {
      const geometry = geometryValue(row?.[config.geometry_field])
      if (geometry?.type !== 'Point' || !Array.isArray(geometry.coordinates) || geometry.coordinates.length !== 2) fail('point', rowNumber)
      source = geometry.coordinates.map(value => numeric(value, rowNumber))
    } else source = [numeric(row?.[config.x_field], rowNumber), numeric(row?.[config.y_field], rowNumber)]
    let point
    try { point = transformGeometry({ type: 'Point', coordinates: source }) } catch { fail('coordinate', rowNumber) }
    const coordinates = point?.type === 'Point' && point.coordinates
    if (!Array.isArray(coordinates) || coordinates.length !== 2 || !coordinates.every(Number.isFinite) || Math.abs(coordinates[0]) > 180 || Math.abs(coordinates[1]) > 90) fail('coordinate', rowNumber)
    const key = JSON.stringify([typeof device, device])
    if (!groups.has(key)) groups.set(key, { device, points: [] })
    groups.get(key).points.push({ time, source, coordinates, row: rowNumber })
  })
  const tracks = [...groups.values()].map(group => {
    const points = [...group.points].sort((left, right) => left.time - right.time || left.row - right.row)
    points.forEach((point, index) => {
      const previous = points[index - 1]
      if (previous && previous.time === point.time && (previous.source[0] !== point.source[0] || previous.source[1] !== point.source[1])) fail('conflict', point.row)
    })
    return { device: group.device, count: points.length, start: new Date(points[0].time).toISOString(), end: new Date(points.at(-1).time).toISOString(), coordinates: points.map(point => point.coordinates) }
  })
  return {
    tracks,
    pointCount: table.rows.length,
    features: tracks.map((track, index) => ({
      type: 'Feature', id: String(index),
      geometry: track.coordinates.some(point => point[0] !== track.coordinates[0][0] || point[1] !== track.coordinates[0][1])
        ? { type: 'LineString', coordinates: track.coordinates }
        : { type: 'Point', coordinates: track.coordinates[0] },
      properties: { device: track.device, points: track.count, start: track.start, end: track.end }
    }))
  }
}
