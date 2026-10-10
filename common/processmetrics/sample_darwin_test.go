//go:build darwin

package processmetrics

import (
	"os"
	"testing"
	"unsafe"
)

func TestTaskInfoRequiresExactNativeReturnLength(t *testing.T) {
	if unsafe.Sizeof(taskInfo{}) != 96 {
		t.Fatal("unexpected libproc ABI")
	}
	for _, result := range []int32{-1, 0, 95, 97} {
		rss, err := readTaskInfo(func(int32, int32, uint64, unsafe.Pointer, int32) int32 { return result })
		if err == nil || rss != 0 {
			t.Fatalf("invalid length %d accepted", result)
		}
	}
	rss, err := readTaskInfo(func(pid, flavor int32, arg uint64, buffer unsafe.Pointer, size int32) int32 {
		if pid != int32(os.Getpid()) || flavor != 4 || arg != 0 || size != 96 {
			t.Fatal("must sample self with exact flavor")
		}
		(*taskInfo)(buffer).Resident = 4096
		return size
	})
	if err != nil || rss != 4096 {
		t.Fatalf("valid native sample: %d %v", rss, err)
	}
}
