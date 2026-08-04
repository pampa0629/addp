import test from 'node:test'
import assert from 'node:assert/strict'

import { normalizeCRSDefinition } from '../src/utils/crsDefinition.mjs'

const mysqlEPSG32650WKT = 'PROJCS["WGS 84 / UTM zone 50N",GEOGCS["WGS 84",DATUM["World Geodetic System 1984",SPHEROID["WGS 84",6378137,298.257223563,AUTHORITY["EPSG","7030"]],AUTHORITY["EPSG","6326"]],PRIMEM["Greenwich",0,AUTHORITY["EPSG","8901"]],UNIT["degree",0.017453292519943278,AUTHORITY["EPSG","9122"]],AXIS["Lat",NORTH],AXIS["Lon",EAST],AUTHORITY["EPSG","4326"]],PROJECTION["Transverse Mercator",AUTHORITY["EPSG","9807"]],PARAMETER["Latitude of natural origin",0,AUTHORITY["EPSG","8801"]],PARAMETER["Longitude of natural origin",117,AUTHORITY["EPSG","8802"]],PARAMETER["Scale factor at natural origin",0.9996,AUTHORITY["EPSG","8805"]],PARAMETER["False easting",500000,AUTHORITY["EPSG","8806"]],PARAMETER["False northing",0,AUTHORITY["EPSG","8807"]],UNIT["metre",1,AUTHORITY["EPSG","9001"]],AXIS["E",EAST],AXIS["N",NORTH],AUTHORITY["EPSG","32650"]]'

test('MySQL CRS WKT is normalized for proj4 parameter parsing', () => {
  const normalized = normalizeCRSDefinition(mysqlEPSG32650WKT, 'wkt')

  assert.doesNotMatch(normalized, /,AUTHORITY\[/)
  assert.match(normalized, /PROJECTION\["Transverse_Mercator"\]/)
  assert.match(normalized, /PARAMETER\["latitude_of_origin",0\]/)
  assert.match(normalized, /PARAMETER\["central_meridian",117\]/)
  assert.match(normalized, /PARAMETER\["scale_factor",0\.9996\]/)
  assert.match(normalized, /PARAMETER\["false_easting",500000\]/)
  assert.match(normalized, /PARAMETER\["false_northing",0\]/)
})
