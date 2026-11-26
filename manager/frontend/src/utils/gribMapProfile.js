const GRIB_TEMPERATURE_MAP = 'ecmwf_ifs_01_temperature/v1'
const precipitationSourceUnitsPerMillimetre = new Map([
  ['ecmwf_ifs_01_precipitation/v1', 0.001],
  ['noaa_gfs_025_precipitation/v1', 1]
])

export function gribPrecipitationSourceUnitsPerMillimetre(profile) {
  return precipitationSourceUnitsPerMillimetre.get(profile)
}

export function isGRIBMapProfile(profile) {
  return profile === GRIB_TEMPERATURE_MAP || precipitationSourceUnitsPerMillimetre.has(profile)
}
