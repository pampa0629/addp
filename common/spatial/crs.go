package spatial

import (
	"fmt"
	"strings"
)

type CRS struct {
	SRID int
	Text string
}

func normalizeSourceCRS(sourceCRS string, sourceSRID int) CRS {
	return normalizeCRS(sourceCRS, sourceSRID)
}

func normalizeTargetCRS(targetSRID int) CRS {
	if targetSRID == 0 {
		targetSRID = SRIDWGS84
	}
	return normalizeCRS("", targetSRID)
}

func normalizeCRS(crsText string, srid int) CRS {
	trimmed := strings.TrimSpace(crsText)
	if trimmed == "" && srid > 0 {
		trimmed = fmt.Sprintf("EPSG:%d", srid)
	}

	return CRS{
		SRID: srid,
		Text: trimmed,
	}
}

func (c CRS) IsZero() bool {
	return c.SRID == 0 && c.Text == ""
}

func (c CRS) Label() string {
	if c.SRID > 0 {
		return fmt.Sprintf("EPSG:%d", c.SRID)
	}
	if c.Text != "" {
		return c.Text
	}
	return "unknown"
}

func isNoopTransform(sourceCRS, targetCRS CRS) bool {
	if sourceCRS.SRID > 0 && sourceCRS.SRID == targetCRS.SRID {
		return true
	}
	if sourceCRS.Text == "" || targetCRS.Text == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(sourceCRS.Text), strings.TrimSpace(targetCRS.Text))
}
