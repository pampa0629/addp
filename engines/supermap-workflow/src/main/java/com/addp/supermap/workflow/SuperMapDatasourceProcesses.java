package com.addp.supermap.workflow;

import static com.addp.supermap.workflow.SuperMapModels.*;
import static com.addp.supermap.workflow.SuperMapRuntimeSupport.*;

import com.fasterxml.jackson.databind.JsonNode;
import com.supermap.sps.core.parameters.impls.SingleOutputImpl;
import java.nio.file.Path;

final class SuperMapDatasourceProcesses {
  public static final class OpenDatasourceProcess extends SuperMapBaseProcess {
    private final WorkflowExecutionContext context;
    private final SingleOutputImpl<SuperMapDatasourceRef> output;

    OpenDatasourceProcess(WorkflowExecutionContext context, JsonNode params) {
      super("datasource.open");
      this.context = context;
      addStringInput("path", paramText(params, "path"));
      addStringInput("alias", optionalText(params, "alias", ""));
      addBooleanInput("read_only", optionalBoolean(params, "read_only", true));
      this.output = addObjectOutput("datasource", SuperMapDatasourceRef.class);
    }

    @Override
    public boolean execute() {
      String path = String.valueOf(parameters.getInput("path").getValue());
      String alias = String.valueOf(parameters.getInput("alias").getValue());
      boolean readOnly = Boolean.TRUE.equals(parameters.getInput("read_only").getValue());
      output.setValue(context.openUdbx(path, alias, readOnly));
      return true;
    }
  }

  public static final class OpenPostgisDatasourceProcess extends SuperMapBaseProcess {
    private final WorkflowExecutionContext context;
    private final SingleOutputImpl<SuperMapDatasourceRef> output;

    OpenPostgisDatasourceProcess(WorkflowExecutionContext context, JsonNode params) {
      super("datasource.open_postgis");
      this.context = context;
      JsonNode connInfo = requireObject(params, "connection_info");
      addStringInput("server", postgisServer(connInfo));
      addStringInput("database", requireConnText(connInfo, "database"));
      addStringInput("user", requireConnText(connInfo, "user"));
      addStringInput("password", optionalConnText(connInfo, "password", ""));
      addStringInput("schema", optionalText(params, "schema", ""));
      addStringInput("table", optionalText(params, "table", ""));
      addStringInput("alias", optionalText(params, "alias", defaultPostgisAlias(params)));
      addBooleanInput("read_only", optionalBoolean(params, "read_only", true));
      this.output = addObjectOutput("datasource", SuperMapDatasourceRef.class);
    }

    @Override
    public boolean execute() {
      String server = String.valueOf(parameters.getInput("server").getValue());
      String database = String.valueOf(parameters.getInput("database").getValue());
      String user = String.valueOf(parameters.getInput("user").getValue());
      String password = String.valueOf(parameters.getInput("password").getValue());
      String schema = String.valueOf(parameters.getInput("schema").getValue());
      String table = String.valueOf(parameters.getInput("table").getValue());
      String alias = String.valueOf(parameters.getInput("alias").getValue());
      boolean readOnly = Boolean.TRUE.equals(parameters.getInput("read_only").getValue());
      output.setValue(
          context.openPostgis(server, database, user, password, schema, table, alias, readOnly));
      return true;
    }
  }

  public static final class CreateDatasourceProcess extends SuperMapBaseProcess {
    private final WorkflowExecutionContext context;
    private final SingleOutputImpl<SuperMapDatasourceRef> output;

    CreateDatasourceProcess(WorkflowExecutionContext context, JsonNode params) {
      super("datasource.create");
      this.context = context;
      JsonNode connInfo = requireObject(params, "connection_info");
      Path resolvedPath = resolveUdbxPath(connInfo, paramText(params, "path"));
      addStringInput("path", resolvedPath.toString());
      addStringInput("alias", optionalText(params, "alias", ""));
      addBooleanInput("overwrite", optionalBoolean(params, "overwrite", false));
      this.output = addObjectOutput("datasource", SuperMapDatasourceRef.class);
    }

    @Override
    public boolean execute() {
      String path = String.valueOf(parameters.getInput("path").getValue());
      String alias = String.valueOf(parameters.getInput("alias").getValue());
      boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());
      output.setValue(context.createUdbx(path, alias, overwrite));
      return true;
    }
  }
}
