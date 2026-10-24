package processmetrics

import (
	"io/fs"
	"math"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func cgroupFixture(cpu, memory string) fstest.MapFS {
	return fstest.MapFS{"own/cpu.stat": {Data: []byte(cpu)}, "own/memory.current": {Data: []byte(memory)}}
}

func TestCgroupUsageReadsWholeGroupAndPreservesRealZero(t *testing.T) {
	for _, usage := range []struct {
		cpu, memory string
		wantCPU     float64
		wantMemory  uint64
	}{
		{"usage_usec 1250000\nuser_usec 1000000\nsystem_usec 250000\nnr_periods 4\n", "67108864\n", 1.25, 64 << 20},
		{"usage_usec 0\n", "0\n", 0, 0},
	} {
		cpu, memory, err := readCgroupUsage(cgroupFixture(usage.cpu, usage.memory), "own")
		if err != nil || cpu != usage.wantCPU || memory != usage.wantMemory {
			t.Fatalf("group sample: %v %v %v", cpu, memory, err)
		}
	}
}

func TestDeploymentSamplerDoesNotReadGroupsUnlessSelected(t *testing.T) {
	reads := 0
	group := func() *CgroupSample { reads++; return nil }
	for _, enabled := range []bool{false, true} {
		before := reads
		sample, err := deploymentSampler(enabled, func() (Sample, error) { return Sample{CPUSeconds: 1, ResidentBytes: 4096}, nil }, group)()
		if err != nil || sample.CPUSeconds != 1 || sample.ResidentBytes != 4096 || sample.Cgroup != nil {
			t.Fatal("optional group failure changed process sample")
		}
		if enabled && reads != before+1 || !enabled && reads != before {
			t.Fatal("deployment collection boundary ignored")
		}
	}
	before := reads
	if _, err := deploymentSampler(true, func() (Sample, error) { return Sample{}, ErrSampleUnavailable }, group)(); err == nil || reads != before {
		t.Fatal("group sampled after process source failed")
	}
}

func TestCgroupUsageRejectsMissingMalformedAndUnboundedFiles(t *testing.T) {
	for name, mutate := range map[string]func(fstest.MapFS){
		"missing cpu":             func(f fstest.MapFS) { delete(f, "own/cpu.stat") },
		"missing memory":          func(f fstest.MapFS) { delete(f, "own/memory.current") },
		"missing usage":           func(f fstest.MapFS) { f["own/cpu.stat"].Data = []byte("user_usec 1\n") },
		"duplicate usage":         func(f fstest.MapFS) { f["own/cpu.stat"].Data = []byte("usage_usec 1\nusage_usec 2\n") },
		"duplicate other field":   func(f fstest.MapFS) { f["own/cpu.stat"].Data = []byte("usage_usec 1\nuser_usec 1\nuser_usec 1\n") },
		"negative cpu":            func(f fstest.MapFS) { f["own/cpu.stat"].Data = []byte("usage_usec -1\n") },
		"cpu decimal":             func(f fstest.MapFS) { f["own/cpu.stat"].Data = []byte("usage_usec 1.25\n") },
		"cpu overflow":            func(f fstest.MapFS) { f["own/cpu.stat"].Data = []byte("usage_usec 18446744073709551616\n") },
		"non numeric other field": func(f fstest.MapFS) { f["own/cpu.stat"].Data = []byte("usage_usec 1\nuser_usec nan\n") },
		"oversized cpu":           func(f fstest.MapFS) { f["own/cpu.stat"].Data = []byte(strings.Repeat("x", cgroupFileLimit+1)) },
		"oversized memory":        func(f fstest.MapFS) { f["own/memory.current"].Data = []byte(strings.Repeat("9", cgroupFileLimit+1)) },
		"memory extra value":      func(f fstest.MapFS) { f["own/memory.current"].Data = []byte("1\n2\n") },
		"memory unavailable":      func(f fstest.MapFS) { f["own/memory.current"].Data = []byte("max\n") },
		"memory unsafe integer":   func(f fstest.MapFS) { f["own/memory.current"].Data = []byte("9007199254740992\n") },
		"memory noncanonical":     func(f fstest.MapFS) { f["own/memory.current"].Data = []byte("01\n") },
		"directory":               func(f fstest.MapFS) { f["own/cpu.stat"].Mode = fs.ModeDir },
		"symlink":                 func(f fstest.MapFS) { f["own/cpu.stat"].Mode = fs.ModeSymlink },
	} {
		t.Run(name, func(t *testing.T) {
			files := cgroupFixture("usage_usec 1\n", "1\n")
			mutate(files)
			if _, _, err := readCgroupUsage(files, "own"); err == nil {
				t.Fatal("invalid read admitted")
			}
		})
	}
	for _, group := range []string{"", "/own", "../own", "own/../own", "own/./", "own/"} {
		if _, _, err := readCgroupUsage(cgroupFixture("usage_usec 1\n", "1\n"), group); err == nil {
			t.Fatalf("unsafe group %q", group)
		}
	}
}

func TestSelfCgroupPathRequiresSingleCanonicalV2Membership(t *testing.T) {
	for _, group := range []string{"/", "/docker/example.scope", "/user.slice/user-1000.slice"} {
		actual, err := selfCgroupPath([]byte("0::" + group + "\n"))
		if err != nil || actual != group {
			t.Fatalf("membership %q: %s %v", group, actual, err)
		}
	}
	for _, data := range []string{"", "0::relative\n", "0::/..\n", "0::/a/../b\n", "0:://a\n", "0::/a/\n", "0::/a\n0::/b\n", "1:cpu:/a\n", "1:cpu:/a\n0::/a\n", "0::/a\x00\n", strings.Repeat("x", cgroupFileLimit+1)} {
		if _, err := selfCgroupPath([]byte(data)); err == nil {
			t.Fatalf("invalid membership admitted: %q", data)
		}
	}
}

func TestCgroupCollectorSeparatesGroupUsageFromProcessRSS(t *testing.T) {
	sample := Sample{CPUSeconds: 0.5, ResidentBytes: 4096, Cgroup: &CgroupSample{BootID: "10000000-0000-4000-8000-000000000001", CgroupID: "101", PIDNamespaceID: "202", CPUSeconds: 8, MemoryBytes: 64 << 20}}
	registry := prometheus.NewRegistry()
	registry.MustRegister(newCollector("monitor", "worker", "own", time.Unix(100, 0), func() (Sample, error) { return sample, nil }))
	families, err := registry.Gather()
	if err != nil || len(families) != 7 {
		t.Fatalf("gather: %v %v", len(families), err)
	}
	values := map[string]float64{}
	for _, family := range families {
		m := family.Metric[0]
		if strings.HasPrefix(family.GetName(), "addp_cgroup_") {
			if len(m.Label) != 3 {
				t.Fatal("only kernel identity labels allowed")
			}
			for _, label := range m.Label {
				switch label.GetName() {
				case "kernel_boot_id":
					if label.GetValue() != sample.Cgroup.BootID {
						t.Fatal("boot mismatch")
					}
				case "cgroup_id":
					if label.GetValue() != "101" {
						t.Fatal("group mismatch")
					}
				case "pid_namespace_id":
					if label.GetValue() != "202" {
						t.Fatal("namespace mismatch")
					}
				default:
					t.Fatal("unexpected identity label")
				}
			}
		}
		if m.Counter != nil {
			values[family.GetName()] = m.Counter.GetValue()
		} else {
			values[family.GetName()] = m.Gauge.GetValue()
		}
	}
	if values["process_resident_memory_bytes"] != 4096 || values["addp_cgroup_memory_current_bytes"] != 64<<20 || values["process_cpu_seconds_total"] != 0.5 || values["addp_cgroup_cpu_usage_seconds_total"] != 8 {
		t.Fatalf("mixed process and group: %v", values)
	}
	// No cached value survives missing or invalid optional scope evidence.
	for _, mutate := range []func(){
		func() { sample.Cgroup.CgroupID = "0" },
		func() { sample.Cgroup.CgroupID = "01" },
		func() { sample.Cgroup.BootID = "bad" },
		func() { sample.Cgroup.CPUSeconds = math.NaN() },
		func() { sample.Cgroup = nil },
	} {
		group := CgroupSample{BootID: "10000000-0000-4000-8000-000000000001", CgroupID: "101", PIDNamespaceID: "202", CPUSeconds: 0, MemoryBytes: 0}
		sample.Cgroup = &group
		mutate()
		families, err = registry.Gather()
		if err != nil || len(families) != 4 {
			t.Fatalf("optional source polluted process sample: %d %v", len(families), err)
		}
	}
}
