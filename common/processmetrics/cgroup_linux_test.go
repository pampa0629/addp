//go:build linux

package processmetrics

import "testing"

func TestCgroupMountCoordinatesNeverEscapeVisibleSubtree(t *testing.T) {
	for _, test := range []struct{ root, group, want string }{
		{"/", "/", "."}, {"/", "/docker/a", "docker/a"}, {"/docker/a", "/docker/a", "."}, {"/docker/a", "/docker/a/child", "child"},
	} {
		actual, err := mountedCgroupPath([]byte("1 0 0:1 "+test.root+" /sys/fs/cgroup ro - cgroup2 cgroup rw\n"), test.group)
		if err != nil || actual != test.want {
			t.Fatalf("mount coordinates: %s %s %s %v", test.root, test.group, actual, err)
		}
	}
	for _, line := range []string{
		"1 0 0:1 /other /sys/fs/cgroup ro - cgroup2 cgroup rw\n",
		"1 0 0:1 /.. /sys/fs/cgroup ro - cgroup2 cgroup rw\n",
		"1 0 0:1 / /sys/fs/cgroup ro - tmpfs tmpfs rw\n",
		"1 0 0:1 / /sys/fs/cgroup/other ro - cgroup2 cgroup rw\n",
		"bad\n", "",
		"1 0 0:1 / /sys/fs/cgroup ro - cgroup2 cgroup rw\n1 0 0:1 / /sys/fs/cgroup ro - cgroup2 cgroup rw\n",
	} {
		if _, err := mountedCgroupPath([]byte(line), "/own"); err == nil {
			t.Fatalf("invalid mount admitted %q", line)
		}
	}
}
