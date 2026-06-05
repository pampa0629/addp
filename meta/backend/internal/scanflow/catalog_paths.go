package scanflow

import "context"

func ResolveCatalogScanPaths(
	ctx context.Context,
	emptyMessage string,
	paths []string,
	fallback []string,
	listFallback func(context.Context) ([]string, error),
	reporter ProgressReporter,
) ([]string, error) {
	resolved := paths
	if len(resolved) == 0 {
		resolved = fallback
	}
	if len(resolved) == 0 && listFallback != nil {
		listed, err := listFallback(ctx)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, listed...)
	}
	if len(resolved) == 0 {
		if reporter != nil {
			reporter.Message(emptyMessage)
			reporter.SetTotal(0)
		}
		return nil, nil
	}
	if reporter != nil {
		reporter.SetTotal(len(resolved))
	}
	return resolved, nil
}

func ScanDepthOrDefault(scanDepth, defaultDepth string) string {
	if scanDepth == "" {
		return defaultDepth
	}
	return scanDepth
}
