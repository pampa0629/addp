package service

import (
	"fmt"
	"github.com/addp/common/datatype"
	"github.com/addp/common/query/plan"
	"github.com/addp/model/internal/models"
	"sort"
)

// buildMetricPlan expresses only Model's metric semantics. Physical bindings and
// the selected native compiler are resolved separately by the publication owner.
func buildMetricPlan(contract models.MetricContract, bindings metricPlanBindings) (plan.Plan, error) {
	b := metricPlanBuilder{p: plan.Plan{SchemaVersion: plan.SchemaVersion, SemanticProfile: plan.SemanticProfile}, columns: map[models.MetricFieldReference]string{}, scans: map[int64]plan.NodeID{}}
	overlap := contract.Operation == "directional_overlap"
	if (!overlap && contract.Operation != "count_distinct") || (overlap && len(contract.Filters) != 0) || contract.Subject.RelationID != 0 {
		return plan.Plan{}, invalidRequest()
	}
	identity, ok := bindings.Relations[contract.SubjectRelationID]
	if !ok || contract.SubjectRelationID <= 0 || identity.SourceField != contract.Subject.FieldID {
		return plan.Plan{}, invalidRequest()
	}
	key, ok := identity.Target.Fields[identity.TargetField]
	if !ok || !singleMetricKey(identity.Target, identity.TargetField) || key.Nullable || key.DataType != "string" {
		return plan.Plan{}, invalidRequest()
	}
	refs := []models.MetricFieldReference{contract.Subject, contract.Distinct, contract.Time}
	required := map[models.MetricFieldReference]string{}
	require := func(ref models.MetricFieldReference, typ string) bool {
		if previous, ok := required[ref]; ok && previous != typ {
			return false
		}
		required[ref] = typ
		return true
	}
	if !require(contract.Subject, "string") || !require(contract.Time, "date") {
		return plan.Plan{}, invalidRequest()
	}
	if contract.SubjectLabel != nil {
		if contract.SubjectLabel.RelationID != contract.SubjectRelationID || !require(*contract.SubjectLabel, "string") {
			return plan.Plan{}, invalidRequest()
		}
		refs = append(refs, *contract.SubjectLabel)
	}
	seenFilters := map[models.MetricFieldReference]bool{}
	for _, f := range contract.Filters {
		if seenFilters[f.Field] {
			return plan.Plan{}, invalidRequest()
		}
		seenFilters[f.Field] = true
		refs = append(refs, f.Field)
		if !require(f.Field, "bool") {
			return plan.Plan{}, invalidRequest()
		}
	}
	for _, id := range sortedMetricRelationIDs(bindings.Relations) {
		r := bindings.Relations[id]
		left, leftOK := bindings.Fact.Fields[r.SourceField]
		right, rightOK := r.Target.Fields[r.TargetField]
		if !leftOK || !rightOK || !singleMetricKey(r.Target, r.TargetField) || right.Nullable || left.DataType != right.DataType {
			return plan.Plan{}, invalidRequest()
		}
		refs = append(refs, models.MetricFieldReference{FieldID: r.SourceField}, models.MetricFieldReference{FieldID: r.TargetField, RelationID: id})
	}
	fields := map[int64][]datatype.FieldInfo{}
	for _, ref := range refs {
		if _, exists := b.columns[ref]; exists {
			continue
		}
		source := bindings.Fact
		if ref.RelationID != 0 {
			r, ok := bindings.Relations[ref.RelationID]
			if !ok {
				return plan.Plan{}, invalidRequest()
			}
			source = r.Target
		}
		f, ok := source.Fields[ref.FieldID]
		if !ok || f.ID <= 0 || f.ColumnName == "" || (required[ref] != "" && f.DataType != required[ref]) {
			return plan.Plan{}, invalidRequest()
		}
		name := fmt.Sprintf("r%d_f%d", ref.RelationID, ref.FieldID)
		b.columns[ref] = name
		// Scan nullability is conservative; required data are checked independently.
		typ := datatype.FieldType(f.DataType)
		field := datatype.FieldInfo{Name: name, Type: typ, Nullable: true}
		if typ == datatype.FieldTypeDecimal {
			field.Precision = 38
			field.Scale = 18
		}
		fields[ref.RelationID] = append(fields[ref.RelationID], field)
	}
	ids := append([]int64{0}, sortedMetricRelationIDs(bindings.Relations)...)
	for _, id := range ids {
		fs := fields[id]
		sort.Slice(fs, func(i, j int) bool { return fs[i].Name < fs[j].Name })
		name := plan.NodeID(fmt.Sprintf("source_%d", id))
		b.p.Nodes = append(b.p.Nodes, plan.Node{ID: name, Op: "scan", Scan: &plan.Scan{Source: plan.SourceID(name), Fields: fs}})
		b.scans[id] = name
	}
	parameters, output, stable := metricPlanSignature(contract.Operation)
	for _, param := range parameters {
		p := plan.Parameter{Name: param.Name, Type: param.Type, Required: param.Required}
		for _, option := range param.Options {
			p.Allowed = append(p.Allowed, plan.Literal{Type: param.Type, Text: option.Value.(string)})
		}
		b.p.Parameters = append(b.p.Parameters, p)
	}
	for i := range output {
		if output[i].Type == datatype.FieldTypeDecimal {
			output[i].Precision = 38
			output[i].Scale = 18
		}
	}
	b.p.Output = plan.OutputContract{Fields: output, StableKey: stable}
	fact := b.scans[0]
	subject := b.ref(fact, contract.Subject)
	predicate := metricOp("eq", subject, metricParam("subject_id"))
	if overlap {
		predicate = metricOp("or", predicate, metricOp("eq", subject, metricParam("comparison_id")))
	}
	for _, f := range contract.Filters {
		if f.Field.RelationID == 0 {
			predicate = metricOp("and", predicate, metricOp("eq", b.ref(fact, f.Field), metricBool(f.Value)))
		}
	}
	relevant := b.filter(fact, predicate)
	// Validate every selected identity even when there are no matching fact rows.
	identityScan := b.scans[contract.SubjectRelationID]
	identityRef := models.MetricFieldReference{FieldID: identity.TargetField, RelationID: contract.SubjectRelationID}
	selectedIdentity := metricOp("eq", b.ref(identityScan, identityRef), metricParam("subject_id"))
	if overlap {
		selectedIdentity = metricOp("or", selectedIdentity, metricOp("eq", b.ref(identityScan, identityRef), metricParam("comparison_id")))
	}
	persons := b.filter(identityScan, selectedIdentity)
	counts := b.aggregate(persons, []plan.Projection{{Name: "identity_key", Expr: b.ref(persons, identityRef)}}, []plan.Measure{{Name: "identity_count", Op: "count_rows"}})
	b.assert(b.filter(counts, metricOp("gt", metricCol(counts, "identity_count"), metricInt("1"))), "metric_identity_not_unique")
	// Each declared dimension must match once, and required time/member data must
	// be present. These assertions are intentionally outside date/result filters.
	for _, id := range sortedMetricRelationIDs(bindings.Relations) {
		r := bindings.Relations[id]
		scan := b.scans[id]
		measures := []plan.Measure{{Name: fmt.Sprintf("match_%d", id), Op: "count_rows"}}
		var values []string
		for i, ref := range []models.MetricFieldReference{contract.Time, contract.Distinct} {
			if ref.RelationID == id {
				name := fmt.Sprintf("present_%d_%d", id, i)
				value := b.ref(scan, ref)
				measures = append(measures, plan.Measure{Name: name, Op: "count_value", Value: &value})
				values = append(values, name)
			}
		}
		grouped := b.aggregate(scan, []plan.Projection{{Name: fmt.Sprintf("key_%d", id), Expr: b.ref(scan, models.MetricFieldReference{FieldID: r.TargetField, RelationID: id})}}, measures)
		joined := b.join(relevant, grouped, "left", metricOp("eq", b.ref(relevant, models.MetricFieldReference{FieldID: r.SourceField}), metricCol(grouped, fmt.Sprintf("key_%d", id))))
		count := metricCol(joined, measures[0].Name)
		bad := metricOp("or", metricOp("is_null", count), metricOp("ne", count, metricInt("1")))
		for _, name := range values {
			bad = metricOp("or", bad, metricOp("ne", metricCol(joined, name), metricInt("1")))
		}
		b.assert(b.filter(joined, bad), "metric_dimension_invalid")
	}
	for _, ref := range []models.MetricFieldReference{contract.Time, contract.Distinct} {
		if ref.RelationID == 0 {
			b.assert(b.filter(relevant, metricOp("is_null", b.ref(relevant, ref))), "metric_value_required")
		}
	}
	data := relevant
	for _, id := range sortedMetricRelationIDs(bindings.Relations) {
		r := bindings.Relations[id]
		scan := b.scans[id]
		data = b.join(data, scan, "inner", metricOp("eq", b.ref(data, models.MetricFieldReference{FieldID: r.SourceField}), b.ref(scan, models.MetricFieldReference{FieldID: r.TargetField, RelationID: id})))
	}
	when := b.ref(data, contract.Time)
	inRange := metricOp("and", metricOp("ge", when, metricParam("start_date")), metricOp("lt", when, metricParam("end_date")))
	for _, f := range contract.Filters {
		if f.Field.RelationID != 0 {
			inRange = metricOp("and", inRange, metricOp("eq", b.ref(data, f.Field), metricBool(f.Value)))
		}
	}
	data = b.filter(data, inRange)
	bucket := metricOp("case", metricOp("eq", metricParam("grain"), metricText("month")), metricOp("month_start", b.ref(data, contract.Time)), metricParam("start_date"))
	members := b.project(data, []plan.Projection{{Name: "person", Expr: b.ref(data, contract.Subject)}, {Name: "member_bucket", Expr: bucket}, {Name: "member", Expr: b.ref(data, contract.Distinct)}})
	members = b.add(plan.Node{Op: "distinct", Distinct: &plan.Unary{Input: members}})
	months := b.add(plan.Node{Op: "date_buckets", DateBuckets: &plan.DateBuckets{Start: metricParam("start_date"), End: metricParam("end_date"), Name: "bucket", MaxMonths: 120}})
	monthly := b.filter(months, metricOp("eq", metricParam("grain"), metricText("month")))
	unit := b.add(plan.Node{Op: "constant_rows", ConstantRows: &plan.ConstantRows{Fields: []datatype.FieldInfo{{Name: "unit", Type: datatype.FieldTypeBool}}, Rows: [][]plan.Literal{{{Type: datatype.FieldTypeBool, Text: "true"}}}}})
	total := b.filter(unit, metricOp("eq", metricParam("grain"), metricText("total")))
	total = b.project(total, []plan.Projection{{Name: "bucket", Expr: metricParam("start_date")}})
	buckets := b.add(plan.Node{Op: "union_all", UnionAll: &plan.UnionAll{Inputs: []plan.NodeID{monthly, total}}})
	if !overlap {
		counted := b.aggregate(members, []plan.Projection{{Name: "count_bucket", Expr: metricCol(members, "member_bucket")}}, []plan.Measure{{Name: "metric_count", Op: "count_rows"}})
		stats := b.join(buckets, counted, "left", metricOp("eq", metricCol(buckets, "bucket"), metricCol(counted, "count_bucket")))
		stats = b.join(stats, persons, "cross", plan.Expr{})
		b.p.Root = b.project(stats, []plan.Projection{{Name: "subject_id", Expr: metricParam("subject_id")}, {Name: "bucket", Expr: metricCol(stats, "bucket")}, {Name: "value", Expr: metricOp("coalesce", metricCol(stats, "metric_count"), metricInt("0"))}})
	} else {
		a := b.filter(members, metricOp("eq", metricCol(members, "person"), metricParam("subject_id")))
		z := b.filter(members, metricOp("eq", metricCol(members, "person"), metricParam("comparison_id")))
		ac := b.aggregate(a, []plan.Projection{{Name: "a_bucket", Expr: metricCol(a, "member_bucket")}}, []plan.Measure{{Name: "a_count", Op: "count_rows"}})
		zc := b.aggregate(z, []plan.Projection{{Name: "z_bucket", Expr: metricCol(z, "member_bucket")}}, []plan.Measure{{Name: "z_count", Op: "count_rows"}})
		zr := b.project(z, []plan.Projection{{Name: "z_member", Expr: metricCol(z, "member")}, {Name: "z_member_bucket", Expr: metricCol(z, "member_bucket")}})
		shared := b.join(a, zr, "inner", metricOp("and", metricOp("eq", metricCol(a, "member"), metricCol(zr, "z_member")), metricOp("eq", metricCol(a, "member_bucket"), metricCol(zr, "z_member_bucket"))))
		shared = b.aggregate(shared, []plan.Projection{{Name: "shared_bucket", Expr: metricCol(shared, "member_bucket")}}, []plan.Measure{{Name: "shared_count", Op: "count_rows"}})
		stats := b.join(buckets, ac, "left", metricOp("eq", metricCol(buckets, "bucket"), metricCol(ac, "a_bucket")))
		stats = b.join(stats, zc, "left", metricOp("eq", metricCol(stats, "bucket"), metricCol(zc, "z_bucket")))
		stats = b.join(stats, shared, "left", metricOp("eq", metricCol(stats, "bucket"), metricCol(shared, "shared_bucket")))
		// Existence checks use unique projected rows, so identities cannot multiply stats.
		for _, name := range []string{"subject_id", "comparison_id"} {
			exists := b.filter(identityScan, metricOp("eq", b.ref(identityScan, identityRef), metricParam(name)))
			exists = b.project(exists, []plan.Projection{{Name: "exists_" + name, Expr: metricParam(name)}})
			stats = b.join(stats, exists, "cross", plan.Expr{})
		}
		roles := b.add(plan.Node{Op: "constant_rows", ConstantRows: &plan.ConstantRows{Fields: []datatype.FieldInfo{{Name: "direction", Type: datatype.FieldTypeString}}, Rows: [][]plan.Literal{{{Type: datatype.FieldTypeString, Text: "forward"}}, {{Type: datatype.FieldTypeString, Text: "reverse"}}}}})
		roles = b.filter(roles, metricOp("or", metricOp("eq", metricCol(roles, "direction"), metricText("forward")), metricOp("eq", metricParam("directions"), metricText("both"))))
		stats = b.join(stats, roles, "cross", plan.Expr{})
		forward := metricOp("eq", metricCol(stats, "direction"), metricText("forward"))
		aCount := metricOp("coalesce", metricCol(stats, "a_count"), metricInt("0"))
		zCount := metricOp("coalesce", metricCol(stats, "z_count"), metricInt("0"))
		denom := metricOp("case", forward, aCount, zCount)
		other := metricOp("case", forward, zCount, aCount)
		sharedCount := metricOp("coalesce", metricCol(stats, "shared_count"), metricInt("0"))
		value := metricOp("case", metricOp("eq", denom, metricInt("0")), metricLiteral(datatype.FieldTypeDecimal, "0"), metricOp("divide", sharedCount, denom))
		b.p.Root = b.project(stats, []plan.Projection{{Name: "subject_id", Expr: metricOp("case", forward, metricParam("subject_id"), metricParam("comparison_id"))}, {Name: "bucket", Expr: metricCol(stats, "bucket")}, {Name: "value", Expr: value}, {Name: "comparison_id", Expr: metricOp("case", forward, metricParam("comparison_id"), metricParam("subject_id"))}, {Name: "direction", Expr: metricCol(stats, "direction")}, {Name: "subject_count", Expr: denom}, {Name: "comparison_count", Expr: other}, {Name: "shared_count", Expr: sharedCount}})
	}
	if contract.SubjectLabel != nil {
		// Enrich only the computed rows, preserving metric grain and stable keys.
		roles := []string{"subject"}
		if overlap {
			roles = append(roles, "comparison")
		}
		for _, role := range roles {
			labelName := role + "_label"
			lookup := b.project(persons, []plan.Projection{
				{Name: "label_identity", Expr: b.ref(persons, identityRef)},
				{Name: labelName, Expr: b.ref(persons, *contract.SubjectLabel)},
			})
			root := b.p.Root
			joined := b.join(root, lookup, "left", metricOp("eq", metricCol(root, role+"_id"), metricCol(lookup, "label_identity")))
			projection := make([]plan.Projection, 0, len(b.p.Output.Fields)+1)
			for _, field := range b.p.Output.Fields {
				projection = append(projection, plan.Projection{Name: field.Name, Expr: metricCol(joined, field.Name)})
			}
			projection = append(projection, plan.Projection{Name: labelName, Expr: metricCol(joined, labelName)})
			b.p.Root = b.project(joined, projection)
			b.p.Output.Fields = append(b.p.Output.Fields, datatype.FieldInfo{Name: labelName, Type: datatype.FieldTypeString, Nullable: true})
		}
	}
	if err := plan.Validate(b.p); err != nil {
		return plan.Plan{}, fmt.Errorf("build metric plan: %w", err)
	}
	return b.p, nil
}

type metricPlanBuilder struct {
	p       plan.Plan
	columns map[models.MetricFieldReference]string
	scans   map[int64]plan.NodeID
}

func (b *metricPlanBuilder) add(n plan.Node) plan.NodeID {
	n.ID = plan.NodeID(fmt.Sprintf("metric_%d", len(b.p.Nodes)))
	b.p.Nodes = append(b.p.Nodes, n)
	return n.ID
}
func (b *metricPlanBuilder) filter(input plan.NodeID, e plan.Expr) plan.NodeID {
	return b.add(plan.Node{Op: "filter", Filter: &plan.Filter{Input: input, Predicate: e}})
}
func (b *metricPlanBuilder) project(input plan.NodeID, c []plan.Projection) plan.NodeID {
	return b.add(plan.Node{Op: "project", Project: &plan.Project{Input: input, Columns: c}})
}
func (b *metricPlanBuilder) aggregate(input plan.NodeID, g []plan.Projection, m []plan.Measure) plan.NodeID {
	return b.add(plan.Node{Op: "aggregate", Aggregate: &plan.Aggregate{Input: input, Groups: g, Measures: m}})
}
func (b *metricPlanBuilder) join(left, right plan.NodeID, kind string, on plan.Expr) plan.NodeID {
	j := &plan.Join{Left: left, Right: right, Kind: kind}
	if kind != "cross" {
		j.On = &on
	}
	return b.add(plan.Node{Op: "join", Join: j})
}
func (b *metricPlanBuilder) assert(id plan.NodeID, code string) {
	b.p.Assertions = append(b.p.Assertions, plan.Assertion{Violation: id, Code: code})
}
func (b *metricPlanBuilder) ref(id plan.NodeID, ref models.MetricFieldReference) plan.Expr {
	return metricCol(id, b.columns[ref])
}
func metricCol(id plan.NodeID, name string) plan.Expr {
	return plan.Expr{Op: "column", Column: &plan.ColumnRef{Input: id, Name: name}}
}
func metricOp(op string, args ...plan.Expr) plan.Expr { return plan.Expr{Op: op, Args: args} }
func metricParam(name string) plan.Expr               { return plan.Expr{Op: "parameter", Parameter: name} }
func metricLiteral(t datatype.FieldType, v string) plan.Expr {
	return plan.Expr{Op: "literal", Literal: &plan.Literal{Type: t, Text: v}}
}
func metricText(v string) plan.Expr { return metricLiteral(datatype.FieldTypeString, v) }
func metricInt(v string) plan.Expr  { return metricLiteral(datatype.FieldTypeBigInt, v) }
func metricBool(v bool) plan.Expr {
	if v {
		return metricLiteral(datatype.FieldTypeBool, "true")
	}
	return metricLiteral(datatype.FieldTypeBool, "false")
}
