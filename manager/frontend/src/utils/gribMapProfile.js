export const GRIB_TEMPERATURE_MAP = 'ecmwf_ifs_01_temperature/v1'
export const GRIB_PRECIPITATION_MAP = 'ecmwf_ifs_01_precipitation/v1'

export function isGRIBMapProfile(profile) {
  return profile === GRIB_TEMPERATURE_MAP || profile === GRIB_PRECIPITATION_MAP
}
