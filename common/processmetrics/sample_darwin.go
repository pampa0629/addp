//go:build darwin

package processmetrics

import (
	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
	"os"
	"runtime"
	"sync"
	"unsafe"
)

// proc_taskinfo layout in Apple's system SDK: six uint64 fields followed by twelve int32.
// Only current resident bytes are consumed; peak RSS is not a substitute.
type taskInfo struct {
	Virtual, Resident, User, System, ThreadsUser, ThreadsSystem uint64
	Counts                                                      [12]int32
}

var procOnce sync.Once
var procInfo func(int32, int32, uint64, unsafe.Pointer, int32) int32
var procLoadError error

func loadProcInfo() {
	h, err := purego.Dlopen("/usr/lib/libproc.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		procLoadError = ErrSampleUnavailable
		return
	}
	p, err := purego.Dlsym(h, "proc_pidinfo")
	if err != nil {
		_ = purego.Dlclose(h)
		procLoadError = ErrSampleUnavailable
		return
	}
	// One process-lifetime handle; no repeated loads or per-sample allocation.
	purego.RegisterFunc(&procInfo, p)
}
func readTaskInfo(read func(int32, int32, uint64, unsafe.Pointer, int32) int32) (uint64, error) {
	var info taskInfo
	size := int32(unsafe.Sizeof(info))
	returned := read(int32(os.Getpid()), 4, 0, unsafe.Pointer(&info), size)
	runtime.KeepAlive(&info)
	if returned != size {
		return 0, ErrSampleUnavailable
	}
	return info.Resident, nil
}
func sampleSelf() (Sample, error) {
	procOnce.Do(loadProcInfo)
	if procLoadError != nil {
		return Sample{}, ErrSampleUnavailable
	}
	rss, err := readTaskInfo(procInfo)
	if err != nil {
		return Sample{}, err
	}
	var usage unix.Rusage
	if unix.Getrusage(unix.RUSAGE_SELF, &usage) != nil {
		return Sample{}, ErrSampleUnavailable
	}
	cpu := float64(usage.Utime.Sec+usage.Stime.Sec) + float64(usage.Utime.Usec+usage.Stime.Usec)/1e6
	return Sample{CPUSeconds: cpu, ResidentBytes: rss}, nil
}
