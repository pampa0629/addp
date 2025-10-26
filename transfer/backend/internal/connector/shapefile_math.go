package connector

import "math"

// almostEqual 判断两个浮点数是否在容差范围内相等
func almostEqual(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9
}

