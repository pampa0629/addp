//go:build linux

package processmetrics

import (
	"github.com/prometheus/procfs"
	"os"
)

func sampleSelf() (Sample, error) {
	fs, err := procfs.NewFS("/proc")
	if err != nil {
		return Sample{}, ErrSampleUnavailable
	}
	p, err := fs.Proc(os.Getpid())
	if err != nil {
		return Sample{}, ErrSampleUnavailable
	}
	s, err := p.Stat()
	if err != nil {
		return Sample{}, ErrSampleUnavailable
	}
	if s.RSS < 0 {
		return Sample{}, ErrSampleUnavailable
	}
	return Sample{CPUSeconds: s.CPUTime(), ResidentBytes: uint64(s.ResidentMemory())}, nil
}
