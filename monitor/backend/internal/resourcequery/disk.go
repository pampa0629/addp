package resourcequery

import (
	"strconv"
	"strings"
)

// A device is an observation dimension, not a physical disk or mount identity.
func diskExpression(s Scope, key string, dimensions Dimensions) (value, stamp, presence string) {
	name := "node_disk_read_bytes_total"
	if key == "node.disk.write_bytes_per_second" {
		name = "node_disk_written_bytes_total"
	}
	raw := s.selector(name)
	if len(dimensions) > 0 {
		raw = strings.TrimSuffix(raw, "}") + `,device=` + strconv.Quote(dimensions["device"]) + `}`
	}
	previous := raw + ` offset 1m`
	rate := `rate(` + raw + `[1m])`
	// One minute, unchanged source boot and one series per device. No summation
	// across device-mapper layers, partitions or different devices.
	boot := s.selector("node_boot_time_seconds")
	bootGuard := ` and on() (count(` + boot + `)==1) and on() (count(` + boot + ` offset 1m)==1)` +
		` and on() (max(` + boot + `)==max(` + boot + ` offset 1m))`
	guard := ` and (count_over_time(` + raw + `[1m])>=4)` +
		` and (resets(` + raw + `[1m])==0)` +
		` and (min_over_time(` + raw + `[1m])>=` + previous + `)` +
		` and (` + previous + `>=0)` +
		` and (timestamp(` + previous + `)>=time()-76)` +
		` and (timestamp(` + raw + `)>=time()-16)` +
		` and (` + rate + `>=0) and (` + rate + `<+Inf)` +
		` and on(device) (count by(device)(` + raw + `)==1)` +
		` and on(device) (count by(device)(` + previous + `)==1)` +
		` and on(device) (count by(device)(count_over_time(` + raw + `[1m]))==1)` +
		` and (timestamp(` + raw + `)== on() group_left max(timestamp(` + boot + `)))` +
		` and (timestamp(` + previous + `)== on() group_left max(timestamp(` + boot + ` offset 1m)))` + bootGuard
	eligible := `(` + rate + guard + `)`
	value = `max by(device)(` + eligible + `)`
	stamp = `max by(device)(timestamp(` + raw + `) and ` + eligible + `)`
	presence = `max by(device)(timestamp(` + raw + `))`
	return
}
