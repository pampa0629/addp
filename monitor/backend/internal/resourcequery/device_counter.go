package resourcequery

import (
	"strconv"
	"strings"
)

// Device names identify source observations, never physical disks or NIC capacity.
func deviceObservationExpression(s Scope, key string, dimensions Dimensions) (value, stamp, presence string) {
	var name, completed string
	switch key {
	case "node.disk.read_bytes_per_second":
		name = "node_disk_read_bytes_total"
	case "node.disk.write_bytes_per_second":
		name = "node_disk_written_bytes_total"
	case "node.disk.io_busy_percent":
		name = "node_disk_io_time_seconds_total"
	case "node.disk.read_mean_duration_milliseconds":
		name, completed = "node_disk_read_time_seconds_total", "node_disk_reads_completed_total"
	case "node.disk.write_mean_duration_milliseconds":
		name, completed = "node_disk_write_time_seconds_total", "node_disk_writes_completed_total"
	case "node.network.receive_bytes_per_second":
		name = "node_network_receive_bytes_total"
	case "node.network.transmit_bytes_per_second":
		name = "node_network_transmit_bytes_total"
	}
	raw, eligible := deviceCounterRate(s, name, dimensions)
	result := eligible
	if completed != "" {
		count, countRate := deviceCounterRate(s, completed, dimensions)
		// Both counters must describe the same device and scrapes at both boundaries.
		aligned := `(max by(device)(timestamp(` + raw + `))==on(device) max by(device)(timestamp(` + count + `)))` +
			` and on(device) (max by(device)(timestamp(` + raw + ` offset 1m))==on(device) max by(device)(timestamp(` + count + ` offset 1m)))`
		result = `(1000*` + eligible + ` / on(device) (` + countRate + `>0)) and on(device) (` + aligned + `)`
	} else if key == "node.disk.io_busy_percent" {
		busy := `round(100*` + eligible + `*1000000000)/1000000000`
		result = `(` + busy + `) and (` + busy + `<=100)`
	}
	result = `(` + result + `)`
	value = `max by(device)(` + result + `)`
	stamp = `max by(device)(timestamp(` + raw + `) and on(device) ` + result + `)`
	presence = `max by(device)(timestamp(` + raw + `))`
	return
}

// All device counter derivatives use the same complete-minute witness contract.
func deviceCounterRate(s Scope, name string, dimensions Dimensions) (raw, eligible string) {
	raw = s.selector(name)
	if len(dimensions) > 0 {
		raw = strings.TrimSuffix(raw, "}") + `,device=` + strconv.Quote(dimensions["device"]) + `}`
	}
	previous := raw + ` offset 1m`
	rate := `rate(` + raw + `[1m])`
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
	eligible = `(` + rate + guard + `)`
	return
}
