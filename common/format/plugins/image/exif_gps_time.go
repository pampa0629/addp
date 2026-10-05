package image

import (
	"math/big"
	"time"
)

// GPS clock facts are UTC receiver observations, not camera shutter times.
func extractGPSTime(ifd *tiffIFD, gps map[string]interface{}, gpsValid bool) bool {
	var date time.Time
	valid := true
	if ifd.hasTag(29) {
		value, ok := exifText(ifd, 29)
		ok = ok && ifd.tags[29].count == 11 && len(value) == 10
		if ok {
			ok = value[4] == ':' && value[7] == ':' && exifDigits(value[:4]) && exifDigits(value[5:7]) && exifDigits(value[8:])
		}
		if ok {
			var err error
			date, err = time.Parse("2006:01:02", value)
			ok = err == nil && date.Year() > 0
		}
		valid = ok
		if ok {
			gps["date_stamp"] = value
		}
	}
	var elapsed time.Duration
	var ordinary bool
	if ifd.hasTag(7) {
		values, duration, normal, ok := exifGPSClock(ifd)
		valid = valid && ok
		if ok {
			gps["time_hms"] = values
			elapsed, ordinary = duration, normal
		}
	}
	if valid && gpsValid && gps["date_stamp"] != nil && ordinary {
		gps["date_time_utc"] = date.Add(elapsed).Format(time.RFC3339Nano)
	}
	return valid
}

func exifGPSClock(ifd *tiffIFD) ([]float64, time.Duration, bool, bool) {
	values, ok := exifRationalTriplet(ifd, 7)
	if !ok || values[0] >= 24 || values[1] >= 60 || values[2] >= 61 {
		return nil, 0, false, false
	}
	if values[2] >= 60 {
		// Preserve an explicit leap-second observation without inventing its
		// date validity or silently rolling it into the following day.
		if values[0] != 23 || values[1] != 59 {
			return nil, 0, false, false
		}
		return values, 0, false, true
	}
	// Use the original fractions for normalization. Summing float64 components
	// can round a value just below midnight into the next date.
	raw, _ := ifd.tagBytes(ifd.tags[7])
	total := new(big.Rat)
	for i, unit := range []int64{3600, 60, 1} {
		part := new(big.Rat).SetFrac64(int64(ifd.order.Uint32(raw[i*8:])), int64(ifd.order.Uint32(raw[i*8+4:])))
		part.Mul(part, new(big.Rat).SetInt64(unit))
		total.Add(total, part)
	}
	if total.Cmp(new(big.Rat).SetInt64(86400)) >= 0 {
		return nil, 0, false, false
	}
	nanos := new(big.Int).Mul(total.Num(), big.NewInt(int64(time.Second)))
	nanos.Quo(nanos, total.Denom())
	return values, time.Duration(nanos.Int64()), true, true
}
