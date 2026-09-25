"""Build a deterministic metric fixture through public owner APIs on a disposable deployment."""

import time
from importlib import import_module

API = import_module("scripts.utils.online-api")
SCAN = import_module("scripts.test.security-transfer-protection-online")
REQUIRED_PERMISSIONS = {
    "meta.scan_task.execute", "meta.scan_task.read", "meta.catalog.read",
    "standard.metric.create", "standard.metric.read", "standard.metric.update", "standard.metric.publish",
    "model.dw_layer.create", "model.logical_model.create", "model.logical_model.read", "model.logical_model.update",
}
QUERY = {"parameters": {"subject_id": "A", "start_date": "2026-01-01",
                        "end_date": "2027-01-01", "grain": "total"},
         "select": ["value"], "page": {"limit": 10}}
EXPECTED_DATA = [{"value": 2}]
GROUPED_QUERY = {"select": ["group_key", "value"], "page": {"limit": 10}}
GROUPED_EXPECTED_DATA = [
    {"group_key": "", "value": "0.750000000000000003"},
    {"group_key": "长沙", "value": "9043526590.462176100000000010"},
    {"group_key": "永州", "value": "11848801485.818872040000000007"},
]
NAMESPACES = {"postgresql": ("public", "schema"), "tidb": ("metric_fixture", "database")}


def prepare(client, engine_id, tenant_id, engine_type, report, checkpoint):
    """Scan and publish count and exact grouped-decimal metrics for one source."""
    if engine_type not in NAMESPACES:
        raise API.SuiteError("unsupported metric fixture engine type")
    namespace, namespace_type = NAMESPACES[engine_type]
    try:
        report["scan_execution_id"] = SCAN.wait_for_scan(client, engine_id, time.monotonic() + 180)
    except SCAN.SuiteError as error:
        raise API.SuiteError("metric source scan did not complete successfully") from error
    report["fixture_resources"] = []
    checkpoint()

    def create(path, body):
        result = client.request("POST", path, (201,), body).payload
        identity = API.require_positive_int(result, "id")
        report["fixture_resources"].append({"path": path, "id": identity})
        checkpoint()
        API.assert_tenant(result, tenant_id, path)
        return result

    layer = "metric_online"
    create("/api/v1/model/dw-layers", {"layer_code": layer, "layer_name": "Metric Online " + engine_type})
    metric_path = "/api/v1/standard/metrics"
    definition = create(metric_path, {
        "scope_type": "tenant_common", "code": "online_metric_count_" + engine_type, "metric_type": "atomic",
        "effective_from": "2020-01-01T00:00:00Z",
        "name": "Online distinct leader events", "definition": "Count distinct led events per person",
        "statistical_caliber": "Distinct events, leader=true, within [start_date,end_date)",
    })
    definition_id = API.require_positive_int(definition, "id")
    definition_revision_id = API.require_positive_int(definition["draft_revision"], "id")
    for action in ("submit", "publish"):
        definition = client.request("POST", f"{metric_path}/{definition_id}/revisions/{definition_revision_id}/{action}",
                                    (200,), {"version": API.require_positive_int(definition, "version")}).payload
    if definition.get("current_revision", {}).get("status") != "published":
        raise API.SuiteError("fixture Standard definition was not published")

    tables = {}
    layouts = {
        "people": [("person_id", "string", True)],
        "events": [("event_id", "string", True), ("event_date", "date", False)],
        "facts": [("row_id", "bigint", True), ("person_id", "string", False),
                  ("event_id", "string", False), ("leader", "bool", False)],
    }
    for kind, fields in layouts.items():
        name = "metric_" + kind
        table = create("/api/v1/model/logical-tables", {
            "name": name, "code": name, "table_type": "fact" if kind == "facts" else "dimension",
            "layer": layer, "scd_type": 0,
            "grain_description": "One participation record" if kind == "facts" else "",
            "materialization": {"target_parent_locator":
                                f"addp://engine/{engine_id}/path/{namespace}?type={namespace_type}",
                                "target_name": name},
        })
        path = f"/api/v1/model/logical-tables/{table['id']}"
        columns = {}
        for column, data_type, primary in fields:
            result = client.request("POST", path + "/fields", (201,), {
                "version": table["version"], "name": column, "column_name": column,
                "data_type": data_type, "nullable": False, "is_pk": primary, "field_role": "regular",
            }).payload
            columns[column] = API.require_positive_int(result["field"], "id")
            table["version"] = API.require_positive_int(result, "version")
        tables[kind] = (table, columns, path)
        if kind != "facts":
            client.request("POST", path + "/approve", (200,), {"version": table["version"]})

    fact, fields, fact_path = tables["facts"]
    relations = {}
    for kind, column in (("people", "person_id"), ("events", "event_id")):
        target, columns, _ = tables[kind]
        relation = client.request("POST", fact_path + "/dimension-relations", (201,), {
            "version": fact["version"], "source_field": fields[column], "target_table": target["id"],
            "target_field": columns[column], "relation_type": "fk",
        }).payload
        relations[kind] = API.require_positive_int(relation["relation"], "id")
        fact["version"] = API.require_positive_int(relation, "version")
    client.request("POST", fact_path + "/approve", (200,), {"version": fact["version"]})
    implementation = create("/api/v1/model/metric-implementations", {
        "fact_table_id": fact["id"], "metric_definition_id": definition_id, "name": "Hosted metric fixture",
    })
    implementation_id = API.require_positive_int(implementation, "id")
    path = f"/api/v1/model/metric-implementations/{implementation_id}"
    draft = client.request("PUT", path + "/draft", (200,), {
        "version": implementation["version"], "metric_definition_revision_id": definition_revision_id,
        "contract": {"operation": "count_distinct", "subject": {"field_id": fields["person_id"]},
                     "subject_relation_id": relations["people"], "distinct": {"field_id": fields["event_id"]},
                     "time": {"field_id": tables["events"][1]["event_date"], "relation_id": relations["events"]},
                     "filters": [{"field": {"field_id": fields["leader"]}, "value": True}]},
    }).payload
    if len(draft.get("revisions", [])) != 1 or draft["revisions"][0].get("status") != "draft":
        raise API.SuiteError("fixture must start with one draft revision")
    revision_id = API.require_positive_int(draft["revisions"][0], "id")
    client.request("POST", f"{path}/revisions/{revision_id}/publish", (200,), {"version": draft["version"]})

    area_definition = create(metric_path, {
        "scope_type": "tenant_common", "code": "online_metric_area_" + engine_type,
        "metric_type": "atomic", "effective_from": "2020-01-01T00:00:00Z",
        "name": "Online area by city", "definition": "Sum prepared square meters by city",
        "statistical_caliber": "Current parcel snapshot, exact decimal(38,18) square meters; null city excluded",
    })
    area_definition_id = API.require_positive_int(area_definition, "id")
    area_definition_revision_id = API.require_positive_int(area_definition["draft_revision"], "id")
    for action in ("submit", "publish"):
        area_definition = client.request(
            "POST", f"{metric_path}/{area_definition_id}/revisions/{area_definition_revision_id}/{action}",
            (200,), {"version": API.require_positive_int(area_definition, "version")},
        ).payload
    if area_definition.get("current_revision", {}).get("status") != "published":
        raise API.SuiteError("grouped area Standard definition was not published")

    area_table = create("/api/v1/model/logical-tables", {
        "name": "metric_areas", "code": "metric_areas", "table_type": "fact", "layer": layer,
        "scd_type": 0, "grain_description": "One prepared parcel area",
        "materialization": {"target_parent_locator":
                            f"addp://engine/{engine_id}/path/{namespace}?type={namespace_type}",
                            "target_name": "metric_areas"},
    })
    area_path = f"/api/v1/model/logical-tables/{area_table['id']}"
    area_fields = {}
    for column, data_type, primary, nullable, role in (
            ("parcel_id", "bigint", True, False, "regular"),
            ("city", "string", False, True, "degenerate_dim"),
            ("area_m2", "decimal", False, False, "measure_additive")):
        result = client.request("POST", area_path + "/fields", (201,), {
            "version": area_table["version"], "name": column, "column_name": column,
            "data_type": data_type, "nullable": nullable, "is_pk": primary, "field_role": role,
        }).payload
        area_fields[column] = API.require_positive_int(result["field"], "id")
        area_table["version"] = API.require_positive_int(result, "version")
    client.request("POST", area_path + "/approve", (200,), {"version": area_table["version"]})
    area_implementation = create("/api/v1/model/metric-implementations", {
        "fact_table_id": area_table["id"], "metric_definition_id": area_definition_id,
        "name": "Hosted grouped decimal fixture",
    })
    area_implementation_id = API.require_positive_int(area_implementation, "id")
    area_impl_path = f"/api/v1/model/metric-implementations/{area_implementation_id}"
    area_draft = client.request("PUT", area_impl_path + "/draft", (200,), {
        "version": area_implementation["version"],
        "metric_definition_revision_id": area_definition_revision_id,
        "contract": {"operation": "sum_decimal_by_group",
                     "group": {"field_id": area_fields["city"]},
                     "measure": {"field_id": area_fields["area_m2"]}},
    }).payload
    if len(area_draft.get("revisions", [])) != 1 or area_draft["revisions"][0].get("status") != "draft":
        raise API.SuiteError("grouped area fixture must start with one draft revision")
    area_revision_id = API.require_positive_int(area_draft["revisions"][0], "id")
    client.request("POST", f"{area_impl_path}/revisions/{area_revision_id}/publish", (200,),
                   {"version": area_draft["version"]})
    report["grouped_fixture"] = {"implementation_id": area_implementation_id, "revision_id": area_revision_id}
    report["history_retention"] = "during_run_only; deployment_destroyed_on_exit; evidence_archived_by_actions"
    checkpoint()
    return implementation_id, revision_id
