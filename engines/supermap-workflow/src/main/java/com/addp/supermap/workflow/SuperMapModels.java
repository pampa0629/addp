package com.addp.supermap.workflow;

import static com.addp.supermap.workflow.SuperMapWorkflowRuntime.MAPPER;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.supermap.data.DatasetVector;
import com.supermap.data.Datasource;
import com.supermap.data.DatasourceConnectionInfo;
import com.supermap.data.EngineType;
import com.supermap.data.FieldInfo;
import com.supermap.data.FieldInfos;
import com.supermap.data.PrjCoordSys;
import com.supermap.data.Rectangle2D;
import com.supermap.data.Workspace;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Instant;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

final class SuperMapModels {
  public static final class WorkflowExecutionContext implements AutoCloseable {
    private final List<Workspace> workspaces = new ArrayList<>();

    SuperMapDatasourceRef openUdbx(String path, String alias, boolean readOnly) {
      Path file = Path.of(path);
      if (!Files.isRegularFile(file)) {
        throw new IllegalArgumentException("UDBX file does not exist: " + path);
      }
      Workspace workspace = new Workspace();
      workspaces.add(workspace);
      DatasourceConnectionInfo info = connectionInfo(path, alias, readOnly);
      Datasource datasource = workspace.getDatasources().open(info);
      info.dispose();
      if (datasource == null || !datasource.isOpened()) {
        throw new IllegalStateException("failed to open UDBX datasource: " + path);
      }
      return new SuperMapDatasourceRef(path, datasource);
    }

    SuperMapDatasourceRef createUdbx(String path, String alias, boolean overwrite) {
      Path file = Path.of(path);
      try {
        if (file.getParent() != null) {
          Files.createDirectories(file.getParent());
        }
        if (Files.exists(file)) {
          if (!overwrite) {
            throw new IllegalArgumentException("UDBX file already exists: " + path);
          }
          Files.delete(file);
        }
      } catch (IOException ex) {
        throw new IllegalStateException("failed to prepare UDBX datasource path: " + path, ex);
      }

      Workspace workspace = new Workspace();
      workspaces.add(workspace);
      DatasourceConnectionInfo info = connectionInfo(path, alias, false);
      Datasource datasource = workspace.getDatasources().create(info);
      info.dispose();
      if (datasource == null || !datasource.isOpened()) {
        throw new IllegalStateException("failed to create UDBX datasource: " + path);
      }
      return new SuperMapDatasourceRef(path, datasource);
    }

    SuperMapDatasourceRef openPostgis(
        String server,
        String database,
        String user,
        String password,
        String schema,
        String table,
        String alias,
        boolean readOnly) {
      Workspace workspace = new Workspace();
      workspaces.add(workspace);
      DatasourceConnectionInfo info = new DatasourceConnectionInfo();
      info.setEngineType(EngineType.PGGIS);
      info.setServer(server);
      info.setDatabase(database);
      info.setUser(user);
      info.setPassword(password);
      info.setAlias(alias == null || alias.isBlank() ? "postgis" : alias);
      info.setReadOnly(readOnly);
      if (schema != null && !schema.isBlank()) {
        Map<String, String> attributes = new HashMap<>();
        attributes.put("Schema", schema);
        info.setExtendAttribute(attributes);
      }
      Datasource datasource = workspace.getDatasources().open(info);
      info.dispose();
      if (datasource == null || !datasource.isOpened()) {
        throw new IllegalStateException(
            "failed to open PostGIS datasource: " + server + "/" + database);
      }
      if (readOnly
          && table != null
          && !table.isBlank()
          && !datasource.getDatasets().contains(table)) {
        throw new IllegalArgumentException("PostGIS dataset not found: " + schema + "." + table);
      }
      return new SuperMapDatasourceRef("postgis://" + server + "/" + database, datasource);
    }

    SuperMapSpatialWorkspaceRef enablePostgisWorkspace(
        String server, String database, String user, String password, String alias) {
      Workspace workspace = new Workspace();
      workspaces.add(workspace);
      DatasourceConnectionInfo info =
          postgisConnectionInfo(server, database, user, password, alias, false);
      boolean created = true;
      Datasource datasource;
      try {
        datasource = workspace.getDatasources().create(info);
      } catch (RuntimeException ex) {
        created = false;
        datasource = workspace.getDatasources().open(info);
        if (datasource == null || !datasource.isOpened()) {
          throw ex;
        }
      } finally {
        info.dispose();
      }
      if (datasource == null || !datasource.isOpened()) {
        throw new IllegalStateException(
            "failed to enable PostGIS datasource: " + server + "/" + database);
      }
      return new SuperMapSpatialWorkspaceRef(
          "postgis://" + server + "/" + database, datasource, !created);
    }

    private DatasourceConnectionInfo connectionInfo(String path, String alias, boolean readOnly) {
      DatasourceConnectionInfo info = new DatasourceConnectionInfo();
      info.setEngineType(EngineType.UDBX);
      info.setServer(path);
      info.setAlias(
          alias == null || alias.isBlank() ? Path.of(path).getFileName().toString() : alias);
      info.setReadOnly(readOnly);
      return info;
    }

    private DatasourceConnectionInfo postgisConnectionInfo(
        String server,
        String database,
        String user,
        String password,
        String alias,
        boolean readOnly) {
      DatasourceConnectionInfo info = new DatasourceConnectionInfo();
      info.setEngineType(EngineType.PGGIS);
      info.setServer(server);
      info.setDatabase(database);
      info.setUser(user);
      info.setPassword(password);
      info.setAlias(alias == null || alias.isBlank() ? "postgis" : alias);
      info.setReadOnly(readOnly);
      return info;
    }

    @Override
    public void close() {
      for (Workspace workspace : workspaces) {
        try {
          workspace.close();
          workspace.dispose();
        } catch (Exception ignored) {
          // Best-effort cleanup after the response has been summarized.
        }
      }
      workspaces.clear();
    }
  }

  public static final class SuperMapSpatialWorkspaceRef {
    final String path;
    final Datasource datasource;
    final boolean alreadyEnabled;

    SuperMapSpatialWorkspaceRef(String path, Datasource datasource, boolean alreadyEnabled) {
      this.path = path;
      this.datasource = datasource;
      this.alreadyEnabled = alreadyEnabled;
    }

    ObjectNode toJson() {
      ObjectNode node = MAPPER.createObjectNode();
      node.put("kind", "supermap_spatial_workspace");
      node.put("ecosystem", "supermap");
      node.put("workspace_kind", "sdx+");
      node.put("state", "enabled");
      node.put("already_enabled", alreadyEnabled);
      node.put("path", path);
      node.put("alias", datasource.getAlias());
      node.put("engine_type", String.valueOf(datasource.getEngineType()));
      node.put("dataset_count", datasource.getDatasets().getCount());
      return node;
    }
  }

  public static final class SuperMapDatasourceRef {
    final String path;
    final Datasource datasource;

    SuperMapDatasourceRef(String path, Datasource datasource) {
      this.path = path;
      this.datasource = datasource;
    }

    ObjectNode toJson() {
      ObjectNode node = MAPPER.createObjectNode();
      node.put("kind", "supermap_datasource");
      node.put("path", path);
      node.put("alias", datasource.getAlias());
      node.put("engine_type", String.valueOf(datasource.getEngineType()));
      node.put("dataset_count", datasource.getDatasets().getCount());
      return node;
    }
  }

  public static final class SuperMapDatasetRef {
    final SuperMapDatasourceRef datasourceRef;
    final DatasetVector dataset;

    SuperMapDatasetRef(SuperMapDatasourceRef datasourceRef, DatasetVector dataset) {
      this.datasourceRef = datasourceRef;
      this.dataset = dataset;
    }

    ObjectNode toJson() {
      ObjectNode node = MAPPER.createObjectNode();
      node.put("kind", "supermap_dataset");
      node.put("datasource_path", datasourceRef.path);
      node.put("datasource_alias", datasourceRef.datasource.getAlias());
      node.put("dataset_name", dataset.getName());
      node.put("dataset_type", String.valueOf(dataset.getType()));
      node.put("record_count", dataset.getRecordCount());
      return node;
    }
  }

  public static final class DatasetInfoSummary {
    final String name;
    final String datasetType;
    final int recordCount;
    final int fieldCount;
    final Rectangle2D bounds;
    final PrjCoordSys prjCoordSys;
    final FieldInfos fieldInfos;

    DatasetInfoSummary(
        String name,
        String datasetType,
        int recordCount,
        int fieldCount,
        Rectangle2D bounds,
        PrjCoordSys prjCoordSys,
        FieldInfos fieldInfos) {
      this.name = name;
      this.datasetType = datasetType;
      this.recordCount = recordCount;
      this.fieldCount = fieldCount;
      this.bounds = bounds;
      this.prjCoordSys = prjCoordSys;
      this.fieldInfos = fieldInfos;
    }

    static DatasetInfoSummary from(DatasetVector dataset) {
      return new DatasetInfoSummary(
          dataset.getName(),
          String.valueOf(dataset.getType()),
          dataset.getRecordCount(),
          dataset.getFieldCount(),
          dataset.getBounds(),
          dataset.getPrjCoordSys(),
          dataset.getFieldInfos());
    }

    ObjectNode toJson() {
      ObjectNode node = MAPPER.createObjectNode();
      node.put("kind", "supermap_dataset_info");
      node.put("dataset_name", name);
      node.put("dataset_type", datasetType);
      node.put("record_count", recordCount);
      node.put("field_count", fieldCount);
      node.set("bounds", boundsJson(bounds));
      node.set("prj_coord_sys", prjCoordSysJson(prjCoordSys));
      ArrayNode fields = node.putArray("fields");
      if (fieldInfos != null) {
        for (int i = 0; i < fieldInfos.getCount(); i++) {
          FieldInfo field = fieldInfos.get(i);
          ObjectNode fieldNode = fields.addObject();
          fieldNode.put("name", field.getName());
          fieldNode.put("caption", field.getCaption());
          fieldNode.put("type", String.valueOf(field.getType()));
          fieldNode.put("required", field.isRequired());
          fieldNode.put("system", field.isSystemField());
          fieldNode.put("max_length", field.getMaxLength());
          fieldNode.put("precision", field.getPrecision());
          fieldNode.put("scale", field.getScale());
        }
      }
      return node;
    }

    private ObjectNode boundsJson(Rectangle2D bounds) {
      ObjectNode node = MAPPER.createObjectNode();
      if (bounds == null || bounds.isEmpty()) {
        node.put("empty", true);
        return node;
      }
      node.put("empty", false);
      node.put("left", bounds.getLeft());
      node.put("bottom", bounds.getBottom());
      node.put("right", bounds.getRight());
      node.put("top", bounds.getTop());
      node.put("width", bounds.getWidth());
      node.put("height", bounds.getHeight());
      return node;
    }

    private ObjectNode prjCoordSysJson(PrjCoordSys prjCoordSys) {
      ObjectNode node = MAPPER.createObjectNode();
      if (prjCoordSys == null) {
        node.put("defined", false);
        return node;
      }
      node.put("defined", true);
      node.put("name", prjCoordSys.getName());
      node.put("epsg", prjCoordSys.getEPSGCode());
      node.put("type", String.valueOf(prjCoordSys.getType()));
      node.put("coord_unit", String.valueOf(prjCoordSys.getCoordUnit()));
      node.put("distance_unit", String.valueOf(prjCoordSys.getDistanceUnit()));
      return node;
    }
  }

  public static final class QueryResult {
    final String datasetName;
    final String attributeFilter;
    final int recordCount;

    QueryResult(String datasetName, String attributeFilter, int recordCount) {
      this.datasetName = datasetName;
      this.attributeFilter = attributeFilter;
      this.recordCount = recordCount;
    }

    ObjectNode toJson() {
      ObjectNode node = MAPPER.createObjectNode();
      node.put("kind", "supermap_query_result");
      node.put("dataset_name", datasetName);
      node.put("attribute_filter", attributeFilter);
      node.put("record_count", recordCount);
      return node;
    }
  }

  record UdbxSchemaState(List<String> missingTables, List<String> missingRegisterColumns) {
    boolean current() {
      return missingTables.isEmpty() && missingRegisterColumns.isEmpty();
    }
  }

  static final class ExecutionRecord {
    final String status;
    final JsonNode result;
    final JsonNode allResults;
    final String error;
    final String errorCode;
    final String details;
    final String startedAt;
    final double executionTimeMs;
    final String message;

    private ExecutionRecord(ObjectNode response, String status) {
      this.status = status;
      this.result = response.path("final_result");
      this.allResults = response.path("all_results");
      this.error = response.path("error").asText(null);
      this.errorCode = response.path("error_code").asText(null);
      this.details = response.path("details").asText(null);
      this.startedAt = Instant.now().toString();
      this.executionTimeMs = response.path("execution_time_ms").asDouble(0);
      this.message = "success".equals(status) ? "执行完成" : this.error;
    }

    static ExecutionRecord success(ObjectNode response) {
      return new ExecutionRecord(response, "success");
    }

    static ExecutionRecord failed(ObjectNode response) {
      return new ExecutionRecord(response, "failed");
    }
  }

  static final class DependencyCheck {
    final boolean available;
    final List<String> missing;
    final String details;

    DependencyCheck(boolean available, List<String> missing, String details) {
      this.available = available;
      this.missing = missing;
      this.details = details;
    }

    static DependencyCheck missing(String details) {
      return new DependencyCheck(false, List.of(), details);
    }
  }
}
