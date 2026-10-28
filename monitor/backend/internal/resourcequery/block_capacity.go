package resourcequery

import (
	"strconv"
	"strings"
)

// The textfile's source time is explicit. Scrape timestamps alone cannot prove
// freshness, and old device series in lookback must not join the current frame.
func blockCapacityFrame(s Scope) (string, string, string) {
	gauge := func(name string) (string, string) {
		raw := s.selector("addp_block_capacity_" + name)
		guard := " and (count(" + raw + ")==1)"
		return "(max(" + raw + ")" + guard + ")", "(max(timestamp(" + raw + "))" + guard + ")"
	}
	success, scrape := gauge("success")
	supported, supportStamp := gauge("supported")
	sampled, ss := gauge("sample_time_seconds")
	boot, bs := gauge("boot_time_seconds")
	count, cs := gauge("device_count")
	native := s.selector("node_boot_time_seconds")
	textfile := s.selector("node_textfile_scrape_error")
	capacity := s.selector("addp_block_capacity_bytes")
	current := "(" + capacity + " and (timestamp(" + capacity + ")==scalar(" + scrape + ")))"
	guard := " and (count(" + textfile + ")==1) and (max(" + textfile + ")==0) and (max(timestamp(" + textfile + "))==" + scrape + ") and (" + supported + "==1) and (" + supportStamp + "==" + scrape + ") and (" + success + "==1) and (" + ss + "==" + scrape + ") and (" + bs + "==" + scrape + ") and (" + cs + "==" + scrape + ")" +
		" and (count(" + native + ")==1) and (max(" + native + ")==" + boot + ") and (max(timestamp(" + native + "))==" + scrape + ")" +
		" and (" + sampled + ">=" + boot + ") and (" + sampled + "<=time()) and (" + sampled + "==floor(" + sampled + "))" +
		" and (" + count + ">=0) and (" + count + "<=100) and (" + count + "==floor(" + count + "))" +
		" and ((count(" + current + ") or vector(0))==" + count + ")" +
		" and ((count(count by(device)(" + current + ")) or vector(0))==" + count + ")"
	eligible := "(" + current + " and (" + current + ">0) and (" + current + "<=9007199254740991) and (" + current + "==floor(" + current + ")))"
	guard += " and ((count(" + eligible + ") or vector(0))==" + count + ")"
	return current, sampled, guard
}

func blockCapacityExpression(s Scope, dimensions Dimensions) (string, string, string) {
	raw, sampled, guard := blockCapacityFrame(s)
	// Match a closed dimension only after validating the entire unfiltered frame.
	if len(dimensions) > 0 {
		selector := strings.TrimSuffix(s.selector("addp_block_capacity_bytes"), "}") + ",device=" + strconv.Quote(dimensions["device"]) + ",kind=" + strconv.Quote(dimensions["kind"]) + "}"
		raw = "(" + raw + " and " + selector + ")"
	}
	group := "(device,kind)"
	values := "(max by" + group + "(" + raw + ") and on" + group + " (count by" + group + "(" + raw + ")==1))"
	valid := " and on() (" + sampled + guard + ")"
	values = "(" + values + " and (" + values + ">0) and (" + values + "<=9007199254740991) and (" + values + "==floor(" + values + "))" + valid + ")"
	stamp := "(" + values + " * 0 + on() group_left() (" + sampled + guard + "))"
	return values, stamp, stamp
}
