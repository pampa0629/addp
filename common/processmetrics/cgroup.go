package processmetrics

import (
	"errors"
	"io"
	"io/fs"
	"math"
	"path"
	"strconv"
	"strings"

	"github.com/addp/common/models"
)

var errCgroupUnavailable = errors.New("self cgroup sample unavailable")

const cgroupFileLimit = 8 << 10

// CgroupSample describes the current kernel boundary, not a container or quota.
// Deployment evidence must independently establish the container association.
type CgroupSample struct {
	BootID, CgroupID, PIDNamespaceID string
	CPUSeconds                       float64
	MemoryBytes                      uint64
}

func (s *CgroupSample) valid() bool {
	if s == nil {
		return false
	}
	identity := models.CgroupIdentity{BootID: s.BootID, CgroupID: s.CgroupID, PIDNamespaceID: s.PIDNamespaceID}
	return identity.Validate() == nil &&
		s.CPUSeconds >= 0 && !math.IsNaN(s.CPUSeconds) && !math.IsInf(s.CPUSeconds, 0) && s.MemoryBytes <= 1<<53-1
}

func readCgroupFile(files fs.FS, name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, errCgroupUnavailable
	}
	f, err := files.Open(name)
	if err != nil {
		return nil, errCgroupUnavailable
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errCgroupUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(f, cgroupFileLimit+1))
	if err != nil || len(data) == 0 || len(data) > cgroupFileLimit {
		return nil, errCgroupUnavailable
	}
	return data, nil
}

func cgroupUnsigned(value string) (uint64, error) {
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil || strconv.FormatUint(n, 10) != value {
		return 0, errCgroupUnavailable
	}
	return n, nil
}

// readCgroupUsage reads only the selected group's two fixed files. It neither
// enumerates processes/descendants nor interprets visible limits as allocation.
func readCgroupUsage(files fs.FS, group string) (float64, uint64, error) {
	if !fs.ValidPath(group) {
		return 0, 0, errCgroupUnavailable
	}
	data, err := readCgroupFile(files, path.Join(group, "cpu.stat"))
	if err != nil {
		return 0, 0, err
	}
	seen := map[string]bool{}
	var usage uint64
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || seen[fields[0]] || len(fields[0]) > 64 {
			return 0, 0, errCgroupUnavailable
		}
		// Linux CONFIG_SCHED_CORE emits core_sched.force_idle_usec. Dots
		// separate accounting namespaces; they are not filesystem paths.
		for _, segment := range strings.Split(fields[0], ".") {
			if segment == "" {
				return 0, 0, errCgroupUnavailable
			}
			for _, r := range segment {
				if !(r >= 'a' && r <= 'z' || r == '_') {
					return 0, 0, errCgroupUnavailable
				}
			}
		}
		value, err := cgroupUnsigned(fields[1])
		if err != nil {
			return 0, 0, err
		}
		seen[fields[0]] = true
		if fields[0] == "usage_usec" {
			usage = value
		}
	}
	if !seen["usage_usec"] {
		return 0, 0, errCgroupUnavailable
	}
	data, err = readCgroupFile(files, path.Join(group, "memory.current"))
	if err != nil {
		return 0, 0, err
	}
	memory, err := cgroupUnsigned(strings.TrimSuffix(string(data), "\n"))
	if err != nil || memory > 1<<53-1 {
		return 0, 0, errCgroupUnavailable
	}
	return float64(usage) / 1e6, memory, nil
}

func selfCgroupPath(data []byte) (string, error) {
	if len(data) == 0 || len(data) > cgroupFileLimit {
		return "", errCgroupUnavailable
	}
	line := strings.TrimSuffix(string(data), "\n")
	// v1/hybrid layouts and multiple records are outside this source contract.
	if !strings.HasPrefix(line, "0::/") || strings.ContainsAny(line, "\n\r\x00") {
		return "", errCgroupUnavailable
	}
	group := strings.TrimPrefix(line, "0::")
	if path.Clean(group) != group || group != "/" && !fs.ValidPath(strings.TrimPrefix(group, "/")) {
		return "", errCgroupUnavailable
	}
	return group, nil
}
