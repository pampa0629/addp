package com.addp.supermap.workflow;

import static com.addp.supermap.workflow.SuperMapModels.*;
import static com.addp.supermap.workflow.SuperMapRuntimeSupport.*;
import static com.addp.supermap.workflow.SuperMapWorkflowRuntime.*;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.nio.file.Files;
import java.nio.file.Path;

final class SuperMapDirectOperatorService {
  static ObjectNode enablePostgis(JsonNode params) {
    try (WorkflowExecutionContext context = new WorkflowExecutionContext()) {
      JsonNode connInfo = requireObject(params, "connection_info");
      String server = postgisServer(connInfo);
      String database = requireConnText(connInfo, "database");
      String user = requireConnText(connInfo, "user");
      String password = optionalConnText(connInfo, "password", "");
      String alias = optionalText(params, "alias", "supermap_sdx");
      return context.enablePostgisWorkspace(server, database, user, password, alias).toJson();
    }
  }

  static ObjectNode upgradeUdbx(JsonNode params) {
    JsonNode connInfo = requireObject(params, "connection_info");
    Path path = resolveUdbxPath(connInfo, paramText(params, "path"));
    String alias = optionalText(params, "alias", path.getFileName().toString());
    UdbxSchemaState before = inspectUdbxSchema(path);
    if (!before.current() && !Files.isWritable(path)) {
      throw new IllegalArgumentException("UDBX file is not writable: " + path);
    }

    int datasetCount;
    boolean readOnly = before.current();
    try (WorkflowExecutionContext context = new WorkflowExecutionContext()) {
      SuperMapDatasourceRef datasource = context.openUdbx(path.toString(), alias, readOnly);
      datasetCount = datasource.datasource.getDatasets().getCount();
    }

    UdbxSchemaState after = inspectUdbxSchema(path);
    if (!after.current()) {
      throw new IllegalStateException(
          "UDBX schema upgrade did not produce the current schema; missing tables="
              + after.missingTables()
              + ", missing SmRegister columns="
              + after.missingRegisterColumns());
    }

    ObjectNode result = MAPPER.createObjectNode();
    result.put("kind", "supermap_udbx_upgrade");
    result.put("path", path.toString());
    result.put("alias", alias);
    result.put("dataset_count", datasetCount);
    result.put("schema_current", true);
    result.put("changed", !before.current());
    return result;
  }
}
