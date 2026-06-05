package scanflow

import (
	"fmt"
	"strings"
)

const (
	ScanDepthBasic = "basic"
	ScanDepthDeep  = "deep"
)

func NormalizeScanDepth(scanDepth, defaultDepth string) (string, error) {
	if defaultDepth == "" {
		defaultDepth = ScanDepthBasic
	}
	if scanDepth == "" {
		scanDepth = defaultDepth
	}
	scanDepth = strings.ToLower(scanDepth)
	if scanDepth == "shallow" {
		return "", fmt.Errorf("unsupported scan depth %q: use basic or deep", scanDepth)
	}
	if scanDepth != ScanDepthBasic && scanDepth != ScanDepthDeep {
		return "", fmt.Errorf("unsupported scan depth %q: use basic or deep", scanDepth)
	}
	return scanDepth, nil
}
