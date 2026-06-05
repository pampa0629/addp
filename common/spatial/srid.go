package spatial

import (
	"regexp"
	"strconv"
	"strings"
)

const (
	SRIDWGS84       = 4326
	SRIDWebMercator = 3857
)

var (
	explicitEPSGPattern  = regexp.MustCompile(`(?i)^\s*(?:EPSG:|URN:OGC:DEF:CRS:EPSG::)(\d+)\s*$`)
	epsgAuthorityPattern = regexp.MustCompile(`(?i)AUTHORITY\s*\[\s*"EPSG"\s*,\s*"(\d+)"\s*\]`)
	wgs84UTMZonePattern  = regexp.MustCompile(`(?i)WGS(?:[_\s]+1984|\s+84)[_/\s-]*UTM[_\s-]*ZONE[_\s-]*(\d{1,2})([NS])\b`)
)

var (
	projectedCRSHints = []string{
		"PROJCS[",
		"PROJCRS[",
		"PROJECTION[",
	}
	webMercatorHints = []string{
		"WGS_1984_WEB_MERCATOR",
		"PSEUDO-MERCATOR",
		"WEB_MERCATOR",
		"WEB MERCATOR",
	}
	geographicWGS84Hints = []string{
		`GEOGCS["WGS 84"`,
		`GEOGCS["GCS_WGS_1984"`,
		`GEOGCRS["WGS 84"`,
		`GEOGCRS["WGS_1984"`,
		"GCS_WGS_1984",
		"WGS_1984",
		"WGS 84",
	}
)

// ParseSRID tries to infer a stable EPSG code from plain CRS text or WKT.
// It is intentionally conservative for projected WKT without an explicit,
// trustworthy projected identifier: in those cases it returns 0 rather than
// guessing a wrong geographic SRID.
func ParseSRID(crsText string) int {
	trimmed := strings.TrimSpace(crsText)
	if trimmed == "" {
		return 0
	}

	if srid := parseExplicitEPSG(trimmed); srid > 0 {
		return srid
	}

	upper := strings.ToUpper(trimmed)
	if hasAnyCRSHint(upper, webMercatorHints) {
		return SRIDWebMercator
	}

	if srid := parseWGS84UTMSRID(trimmed); srid > 0 {
		return srid
	}

	authorities := extractEPSGAuthorities(trimmed)
	if looksProjectedCRS(upper) {
		if len(authorities) > 1 {
			return authorities[len(authorities)-1]
		}
		if srid := lastEPSGAuthority(authorities); isLikelyProjectedSRID(srid) {
			return srid
		}
		return 0
	}

	if srid := lastEPSGAuthority(authorities); srid > 0 {
		return srid
	}

	if hasAnyCRSHint(upper, geographicWGS84Hints) {
		return SRIDWGS84
	}

	return 0
}

func parseExplicitEPSG(text string) int {
	matches := explicitEPSGPattern.FindStringSubmatch(strings.TrimSpace(text))
	if len(matches) != 2 {
		return 0
	}
	return parseSRIDInt(matches[1])
}

func extractEPSGAuthorities(text string) []int {
	matches := epsgAuthorityPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}

	srids := make([]int, 0, len(matches))
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		if srid := parseSRIDInt(match[1]); srid > 0 {
			srids = append(srids, srid)
		}
	}
	return srids
}

func parseWGS84UTMSRID(text string) int {
	matches := wgs84UTMZonePattern.FindStringSubmatch(strings.TrimSpace(text))
	if len(matches) != 3 {
		return 0
	}

	zone := parseSRIDInt(matches[1])
	if zone < 1 || zone > 60 {
		return 0
	}

	switch strings.ToUpper(matches[2]) {
	case "N":
		return 32600 + zone
	case "S":
		return 32700 + zone
	default:
		return 0
	}
}

func looksProjectedCRS(text string) bool {
	return hasAnyCRSHint(text, projectedCRSHints)
}

func hasAnyCRSHint(text string, hints []string) bool {
	for _, hint := range hints {
		if strings.Contains(text, hint) {
			return true
		}
	}
	return false
}

func isLikelyProjectedSRID(srid int) bool {
	switch {
	case srid == SRIDWebMercator:
		return true
	case srid >= 32601 && srid <= 32660:
		return true
	case srid >= 32701 && srid <= 32760:
		return true
	default:
		return false
	}
}

func lastEPSGAuthority(authorities []int) int {
	if len(authorities) == 0 {
		return 0
	}
	return authorities[len(authorities)-1]
}

func parseSRIDInt(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return value
}
