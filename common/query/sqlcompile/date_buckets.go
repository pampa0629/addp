package sqlcompile

import (
	"strconv"
	"strings"

	"github.com/addp/common/query/plan"
)

// dateBuckets returns the result and its independent violation relation. The
// bound is a contract, not a row limit: an oversized range must fail even when
// every generated bucket is later filtered out.
func (b *relationBuilder) dateBuckets(n plan.Node) (string, string, error) {
	d := n.DateBuckets
	scope := b.scope(n)
	start, err := CompileExpression(d.Start, scope, b.expression)
	if err != nil {
		return "", "", err
	}
	end, err := CompileExpression(d.End, scope, b.expression)
	if err != nil {
		return "", "", err
	}
	input := b.names[n.ID] + "_bounds"
	rangeName := b.names[n.ID] + "_range"
	offsets := b.names[n.ID] + "_offsets"
	var inherited []string
	for _, x := range []CheckedExpression{start, end} {
		if x.Invalid != "" {
			inherited = append(inherited, "("+x.Invalid+")")
		}
	}
	bad := "FALSE"
	if len(inherited) > 0 {
		bad = strings.Join(inherited, " OR ")
	}
	if err := b.add(input, "SELECT "+start.SQL+" AS "+b.q("start_date")+", "+end.SQL+" AS "+b.q("end_date")+", ("+bad+") AS "+b.q("invalid")); err != nil {
		return "", "", err
	}
	startRef, endRef := b.q(input)+"."+b.q("start_date"), b.q(input)+"."+b.q("end_date")
	s, e := b.expression.DateParts(startRef), b.expression.DateParts(endRef)
	// Count intersecting months directly; subtracting a day from end would
	// underflow at 0001-01-01 for an invalid/reversed range.
	count := "((" + e.Year + " - " + s.Year + ") * 12 + (" + e.Month + " - " + s.Month + ") + CASE WHEN " + e.Day + " > 1 THEN 1 ELSE 0 END)"
	valid := "COALESCE((" + endRef + " > " + startRef + ") AND (" + count + " BETWEEN 1 AND " + strconv.Itoa(d.MaxMonths) + "), FALSE)"
	if err := b.add(rangeName, "SELECT "+startRef+" AS "+b.q("start_date")+", "+count+" AS "+b.q("months")+", "+valid+" AS "+b.q("valid")+", "+b.q(input)+"."+b.q("invalid")+" AS "+b.q("invalid")+" FROM "+b.q(input)); err != nil {
		return "", "", err
	}
	var rows []string
	for i := 0; i < d.MaxMonths; i++ {
		rows = append(rows, "SELECT "+strconv.Itoa(i)+" AS "+b.q("offset"))
	}
	if err := b.add(offsets, strings.Join(rows, " UNION ALL ")); err != nil {
		return "", "", err
	}
	r := b.q(rangeName)
	offset := b.q(offsets) + "." + b.q("offset")
	selected := r + "." + b.q("valid") + " AND " + offset + " < " + r + "." + b.q("months")
	// Guard inside the native operation, not only in WHERE: optimizers may
	// evaluate the unselected offsets, including those beyond December 9999.
	safeOffset := "CASE WHEN " + selected + " THEN " + offset + " ELSE NULL END"
	value := b.expression.ShiftMonths(b.expression.MonthStart(r+"."+b.q("start_date")), safeOffset)
	query := "SELECT " + value + " AS " + b.q(d.Name) + " FROM " + r + " CROSS JOIN " + b.q(offsets) + " WHERE " + selected
	check := "SELECT 1 AS " + b.q("bad") + " FROM " + r + " WHERE " + r + "." + b.q("invalid") + " OR NOT " + r + "." + b.q("valid")
	return query, check, nil
}
