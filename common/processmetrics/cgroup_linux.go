//go:build linux

package processmetrics

import (
	"bytes"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"syscall"
)

const cgroupMount = "/sys/fs/cgroup"

func boundedKernelFile(name string, limit int64) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, errCgroupUnavailable
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || len(data) == 0 || int64(len(data)) > limit {
		return nil, errCgroupUnavailable
	}
	return data, nil
}

// The membership and mount root use the same cgroup namespace coordinates.
// A non-visible parent is never opened to guess the host's hierarchy.
func mountedCgroupPath(mountinfo []byte, group string) (string, error) {
	if len(mountinfo) == 0 || len(mountinfo) > 128<<10 {
		return "", errCgroupUnavailable
	}
	result := ""
	for _, line := range strings.Split(strings.TrimSuffix(string(mountinfo), "\n"), "\n") {
		parts := strings.Split(line, " - ")
		if len(parts) != 2 {
			return "", errCgroupUnavailable
		}
		left, right := strings.Fields(parts[0]), strings.Fields(parts[1])
		if len(left) < 6 || len(right) < 3 {
			return "", errCgroupUnavailable
		}
		if left[4] != cgroupMount {
			continue
		}
		root := left[3]
		if result != "" || right[0] != "cgroup2" || !strings.HasPrefix(root, "/") || path.Clean(root) != root || strings.ContainsAny(root, "\\\x00") {
			return "", errCgroupUnavailable
		}
		switch {
		case group == root:
			result = "."
		case root == "/":
			result = strings.TrimPrefix(group, "/")
		case strings.HasPrefix(group, root+"/"):
			result = strings.TrimPrefix(group, root+"/")
		default:
			return "", errCgroupUnavailable
		}
	}
	if result == "" {
		return "", errCgroupUnavailable
	}
	return result, nil
}

func inodeOf(info os.FileInfo) string {
	if info == nil {
		return ""
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return strconv.FormatUint(stat.Ino, 10)
}

// sampleSelfCgroup is optional. A failed cgroup read must never invalidate the
// independent process CPU/RSS sample or business registration/readiness.
func sampleSelfCgroup() *CgroupSample {
	cgroupNamespace, err := os.Stat("/proc/self/ns/cgroup")
	if err != nil {
		return nil
	}
	membership, err := boundedKernelFile("/proc/self/cgroup", cgroupFileLimit)
	if err != nil {
		return nil
	}
	group, err := selfCgroupPath(membership)
	if err != nil {
		return nil
	}
	mountinfo, err := boundedKernelFile("/proc/self/mountinfo", 128<<10)
	if err != nil {
		return nil
	}
	relative, err := mountedCgroupPath(mountinfo, group)
	if err != nil {
		return nil
	}
	root, err := os.OpenRoot(cgroupMount)
	if err != nil {
		return nil
	}
	defer root.Close()
	directory, err := root.Open(relative)
	if err != nil {
		return nil
	}
	defer directory.Close()
	var filesystem syscall.Statfs_t
	if syscall.Fstatfs(int(directory.Fd()), &filesystem) != nil || filesystem.Type != 0x63677270 {
		return nil
	}
	info, err := directory.Stat()
	if err != nil || !info.IsDir() {
		return nil
	}
	pidns, err := os.Stat("/proc/self/ns/pid")
	if err != nil {
		return nil
	}
	boot, err := boundedKernelFile("/proc/sys/kernel/random/boot_id", 64)
	if err != nil {
		return nil
	}
	// Anchor the two reads to this directory, even if its pathname is replaced.
	groupRoot, err := root.OpenRoot(relative)
	if err != nil {
		return nil
	}
	defer groupRoot.Close()
	anchored, err := groupRoot.Stat(".")
	if err != nil || !os.SameFile(info, anchored) {
		return nil
	}
	cpu, memory, err := readCgroupUsage(groupRoot.FS(), ".")
	if err != nil {
		return nil
	}
	after, err := boundedKernelFile("/proc/self/cgroup", cgroupFileLimit)
	if err != nil || !bytes.Equal(membership, after) {
		return nil
	}
	currentNamespace, err := os.Stat("/proc/self/ns/cgroup")
	if err != nil || !os.SameFile(cgroupNamespace, currentNamespace) {
		return nil
	}
	current, err := root.Stat(relative)
	if err != nil || !os.SameFile(info, current) {
		return nil
	}
	sample := &CgroupSample{BootID: strings.TrimSuffix(string(boot), "\n"), CgroupID: inodeOf(info), PIDNamespaceID: inodeOf(pidns), CPUSeconds: cpu, MemoryBytes: memory}
	if !sample.valid() {
		return nil
	}
	return sample
}
