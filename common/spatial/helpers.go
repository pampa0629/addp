package spatial

func cloneEndss(src [][]int) [][]int {
	if src == nil {
		return nil
	}
	dst := make([][]int, len(src))
	for i := range src {
		if src[i] == nil {
			continue
		}
		dst[i] = append([]int(nil), src[i]...)
	}
	return dst
}
