package service

import "time"

func notificationBackoff(initial, maximum time.Duration, attempt int) time.Duration {
	for i := 1; i < attempt && initial < maximum; i++ {
		if initial > maximum/2 {
			return maximum
		}
		initial *= 2
	}
	if initial > maximum {
		return maximum
	}
	return initial
}
