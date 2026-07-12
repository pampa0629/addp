package com.addp.supermap.workflow;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import com.supermap.analyst.spatialanalyst.BufferAnalyst;
import com.supermap.analyst.spatialanalyst.BufferAnalystParameter;
import com.supermap.analyst.spatialanalyst.BufferEndType;
import com.supermap.analyst.spatialanalyst.BufferRadiusUnit;
import com.supermap.analyst.spatialanalyst.DissolveParameter;
import com.supermap.analyst.spatialanalyst.DissolveType;
import com.supermap.analyst.spatialanalyst.Generalization;
import com.supermap.analyst.spatialanalyst.OverlayAnalyst;
import com.supermap.analyst.spatialanalyst.OverlayAnalystParameter;
import com.supermap.data.CoordSysTransMethod;
import com.supermap.data.CoordSysTransParameter;
import com.supermap.data.CoordSysTranslator;
import com.supermap.data.CursorType;
import com.supermap.data.Dataset;
import com.supermap.data.DatasetType;
import com.supermap.data.DatasetVector;
import com.supermap.data.DatasetVectorInfo;
import com.supermap.data.Datasource;
import com.supermap.data.DatasourceConnectionInfo;
import com.supermap.data.EncodeType;
import com.supermap.data.EngineType;
import com.supermap.data.FieldInfo;
import com.supermap.data.FieldInfos;
import com.supermap.data.PrjCoordSys;
import com.supermap.data.QueryParameter;
import com.supermap.data.Rectangle2D;
import com.supermap.data.Recordset;
import com.supermap.data.SpatialRelationType;
import com.supermap.data.Workspace;
import com.supermap.sps.core.executor.IWorkflowExecutor;
import com.supermap.sps.core.executor.WorkflowExecutors;
import com.supermap.sps.core.parameters.ISingleDataDefinition;
import com.supermap.sps.core.parameters.impls.ConstantValueProvider;
import com.supermap.sps.core.parameters.impls.DefaultSingleDataDefinition;
import com.supermap.sps.core.parameters.impls.ParametersImpl;
import com.supermap.sps.core.parameters.impls.SingleInputImpl;
import com.supermap.sps.core.parameters.impls.SingleOutputImpl;
import com.supermap.sps.core.workflow.IDataItem;
import com.supermap.sps.core.workflow.IProcess;
import com.supermap.sps.core.workflow.IProcessItem;
import com.supermap.sps.core.workflow.IWorkflow;
import com.supermap.sps.core.workflow.impls.AbstractProcess;
import com.supermap.sps.core.workflow.impls.WorkflowFactory;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.net.URLDecoder;
import java.nio.file.InvalidPathException;
import java.nio.charset.StandardCharsets;
import java.nio.file.DirectoryStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.time.Instant;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.HashMap;
import java.util.HexFormat;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.TimeUnit;

public final class SuperMapWorkflowRuntime {
    private static final ObjectMapper MAPPER = new ObjectMapper();
    private static final String ENGINE_TYPE = "supermap_workflow";
    private static final String SERVICE_NAME = "supermap-workflow-engine";
    private static final String VERSION = "0.3.0";
    private static final String SPS_FACTORY = "addp_supermap_workflow";
    private static final List<String> CURRENT_UDBX_TABLES = List.of(
            "SmAdditionalInfo",
            "SmAttributeRule",
            "SmGroupItems",
            "SmPyramidColumns"
    );
    private static final List<String> CURRENT_UDBX_REGISTER_COLUMNS = List.of(
            "SmGroupID",
            "SmRelationship",
            "SmSubTypes"
    );
    private static final Instant STARTED_AT = Instant.now();
    private static final Map<String, ExecutionRecord> EXECUTIONS = new ConcurrentHashMap<>();

    private SuperMapWorkflowRuntime() {
    }

    public static void main(String[] args) throws Exception {
        int port = Integer.parseInt(System.getenv().getOrDefault("PORT", "8103"));
        HttpServer server = HttpServer.create(new InetSocketAddress("0.0.0.0", port), 0);
        server.createContext("/health", SuperMapWorkflowRuntime::handleHealth);
        server.createContext("/api/operators", SuperMapWorkflowRuntime::handleOperators);
        server.createContext("/api/workflow", SuperMapWorkflowRuntime::handleWorkflow);
        server.createContext("/api/operators/", SuperMapWorkflowRuntime::handleDirectOperator);
        server.createContext("/api/executions/", SuperMapWorkflowRuntime::handleExecutionStatus);
        server.start();
        System.out.printf("%s listening on http://0.0.0.0:%d%n", SERVICE_NAME, port);
    }

    private static void handleHealth(HttpExchange exchange) throws IOException {
        if (!method(exchange, "GET")) {
            sendJson(exchange, 405, failed("METHOD_NOT_ALLOWED", "Only GET is supported"));
            return;
        }
        String superMapBin = System.getenv().getOrDefault("SUPERMAP_OBJECTSJAVA_BIN", "");
        String gpaLibDir = System.getenv().getOrDefault("SUPERMAP_GPA_LIB_DIR", "");
        DependencyCheck objectsJavaCheck = checkDependency(
                superMapBin,
                List.of(
                        "com.supermap.data.jar",
                        "com.supermap.analyst.spatialanalyst.jar",
                        "libWrapjCore.so",
                        "libWrapjAnalyst.so",
                        "libWrapjMaritime.so"
                ),
                List.of()
        );
        DependencyCheck gpaLibsCheck = checkDependency(
                gpaLibDir,
                List.of(),
                List.of(
                        "gpa-sps-core-*.jar",
                        "jackson-databind-*.jar",
                        "hutool-all-*.jar",
                        "log4j-core-*.jar"
                )
        );
        DependencyCheck sqliteCheck = Files.isExecutable(Path.of("/usr/bin/sqlite3"))
                ? new DependencyCheck(true, List.of(), "")
                : DependencyCheck.missing("executable does not exist: /usr/bin/sqlite3");

        ObjectNode dependencies = MAPPER.createObjectNode();
        dependencies.set("objectsjava", dependencyJson("SUPERMAP_OBJECTSJAVA_BIN", superMapBin, objectsJavaCheck));
        dependencies.set("gpa_libs", dependencyJson("SUPERMAP_GPA_LIB_DIR", gpaLibDir, gpaLibsCheck));
        dependencies.set("sqlite3", dependencyJson("", "/usr/bin/sqlite3", sqliteCheck));

        ObjectNode response = MAPPER.createObjectNode();
        response.put("status", objectsJavaCheck.available && gpaLibsCheck.available && sqliteCheck.available ? "healthy" : "degraded");
        response.put("service", SERVICE_NAME);
        response.put("version", VERSION);
        response.put("uptime", Math.max(0, Instant.now().getEpochSecond() - STARTED_AT.getEpochSecond()));
        response.put("operators_count", operators().size());
        response.set("dependencies", dependencies);
        sendJson(exchange, 200, response);
    }

    private static ObjectNode dependencyJson(String env, String path, DependencyCheck check) {
        ObjectNode node = MAPPER.createObjectNode();
        if (env != null && !env.isBlank()) {
            node.put("env", env);
        }
        node.put("path", path);
        node.put("available", check.available);
        ArrayNode missing = node.putArray("missing");
        check.missing.forEach(missing::add);
        if (!check.details.isBlank()) {
            node.put("details", check.details);
        }
        return node;
    }

    private static DependencyCheck checkDependency(String rawPath, List<String> requiredFiles, List<String> requiredGlobs) {
        if (rawPath == null || rawPath.isBlank()) {
            return DependencyCheck.missing("path is empty");
        }
        Path dir = Path.of(rawPath);
        if (!Files.isDirectory(dir)) {
            return DependencyCheck.missing("directory does not exist: " + rawPath);
        }

        List<String> missing = new ArrayList<>();
        for (String file : requiredFiles) {
            if (!Files.isRegularFile(dir.resolve(file))) {
                missing.add(file);
            }
        }
        for (String glob : requiredGlobs) {
            if (!hasGlobMatch(dir, glob)) {
                missing.add(glob);
            }
        }
        if (!missing.isEmpty()) {
            return new DependencyCheck(false, missing, "missing required runtime files");
        }
        return new DependencyCheck(true, List.of(), "");
    }

    private static boolean hasGlobMatch(Path dir, String glob) {
        try (DirectoryStream<Path> stream = Files.newDirectoryStream(dir, glob)) {
            for (Path ignored : stream) {
                return true;
            }
            return false;
        } catch (IOException ex) {
            return false;
        }
    }

    private static void handleOperators(HttpExchange exchange) throws IOException {
        if (!method(exchange, "GET")) {
            sendJson(exchange, 405, failed("METHOD_NOT_ALLOWED", "Only GET is supported"));
            return;
        }
        String category = queryParam(exchange, "category");
        ArrayNode resultOperators = MAPPER.createArrayNode();
        for (ObjectNode operator : operators().values()) {
            if (category == null || category.equals(operator.path("category").asText())) {
                resultOperators.add(operator);
            }
        }

        ObjectNode response = MAPPER.createObjectNode();
        response.put("status", "success");
        response.set("operators", resultOperators);
        response.put("count", resultOperators.size());
        sendJson(exchange, 200, response);
    }

    private static void handleWorkflow(HttpExchange exchange) throws IOException {
        if (!method(exchange, "POST")) {
            sendJson(exchange, 405, failed("METHOD_NOT_ALLOWED", "Only POST is supported"));
            return;
        }

        long started = System.nanoTime();
        String executionID = UUID.randomUUID().toString();
        try {
            JsonNode request = MAPPER.readTree(readAll(exchange.getRequestBody()));
            ObjectNode response = executeWorkflow(executionID, request, elapsedMs(started));
            EXECUTIONS.put(executionID, ExecutionRecord.success(response));
            sendJson(exchange, 200, response);
        } catch (IllegalArgumentException ex) {
            ObjectNode response = failed("WORKFLOW_INVALID", ex.getMessage());
            response.put("execution_id", executionID);
            response.put("execution_time_ms", elapsedMs(started));
            EXECUTIONS.put(executionID, ExecutionRecord.failed(response));
            sendJson(exchange, 400, response);
        } catch (Exception ex) {
            ObjectNode response = failed("EXECUTION_FAILED", "SuperMap workflow execution failed");
            response.put("details", ex.toString());
            response.put("execution_id", executionID);
            response.put("execution_time_ms", elapsedMs(started));
            EXECUTIONS.put(executionID, ExecutionRecord.failed(response));
            ex.printStackTrace(System.err);
            sendJson(exchange, 500, response);
        }
    }

    private static void handleDirectOperator(HttpExchange exchange) throws IOException {
        if (!method(exchange, "POST")) {
            sendJson(exchange, 405, failed("METHOD_NOT_ALLOWED", "Only POST is supported"));
            return;
        }
        String path = exchange.getRequestURI().getPath();
        String prefix = "/api/operators/";
        String suffix = "/invoke";
        if (!path.startsWith(prefix) || !path.endsWith(suffix)) {
            sendJson(exchange, 404, failed("OPERATOR_NOT_FOUND", "Operator endpoint not found"));
            return;
        }
        String name = path.substring(prefix.length(), path.length() - suffix.length());
        if (!operators().containsKey(name)) {
            sendJson(exchange, 404, failed("OPERATOR_NOT_FOUND", "Operator not found: " + name));
            return;
        }
        if (!operatorSupportsMode(operators().get(name), "direct")) {
            sendJson(exchange, 403, failed("DIRECT_NOT_SUPPORTED", "operator does not support direct execution: " + name));
            return;
        }

        long started = System.nanoTime();
        try {
            JsonNode request = MAPPER.readTree(readAll(exchange.getRequestBody()));
            JsonNode params = request.path("params");
            if (!params.isObject()) {
                throw new IllegalArgumentException("params must be an object");
            }
            ObjectNode response = invokeDirectOperator(name, params);
            response.put("status", "success");
            response.put("execution_time_ms", elapsedMs(started));
            sendJson(exchange, 200, response);
        } catch (IllegalArgumentException ex) {
            ObjectNode response = failed("INVALID_PARAMS", ex.getMessage());
            response.put("execution_time_ms", elapsedMs(started));
            sendJson(exchange, 400, response);
        } catch (Exception ex) {
            ObjectNode response = failed("EXECUTION_FAILED", "SuperMap direct operator execution failed");
            response.put("details", ex.toString());
            response.put("execution_time_ms", elapsedMs(started));
            ex.printStackTrace(System.err);
            sendJson(exchange, 500, response);
        }
    }

    private static ObjectNode invokeDirectOperator(String name, JsonNode params) {
        return switch (name) {
            case "datasource.enable_postgis" -> enablePostgis(params);
            case "datasource.upgrade_udbx" -> upgradeUdbx(params);
            default -> throw new IllegalArgumentException("operator does not support direct execution: " + name);
        };
    }

    private static ObjectNode enablePostgis(JsonNode params) {
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

    private static ObjectNode upgradeUdbx(JsonNode params) {
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
                            + after.missingTables() + ", missing SmRegister columns=" + after.missingRegisterColumns()
            );
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

    private static boolean operatorSupportsMode(ObjectNode operator, String mode) {
        JsonNode modes = operator.path("execution_modes");
        if (!modes.isArray()) {
            return false;
        }
        for (JsonNode item : modes) {
            if (mode.equals(item.asText())) {
                return true;
            }
        }
        return false;
    }

    private static void handleExecutionStatus(HttpExchange exchange) throws IOException {
        if (!method(exchange, "GET")) {
            sendJson(exchange, 405, failed("METHOD_NOT_ALLOWED", "Only GET is supported"));
            return;
        }
        String executionID = exchange.getRequestURI().getPath().substring("/api/executions/".length());
        ExecutionRecord record = EXECUTIONS.get(executionID);
        if (record == null) {
            sendJson(exchange, 404, failed("EXECUTION_NOT_FOUND", "Execution not found"));
            return;
        }
        ObjectNode response = MAPPER.createObjectNode();
        response.put("status", record.status);
        response.put("execution_id", executionID);
        response.set("result", record.result);
        response.set("all_results", record.allResults);
        if (record.error != null) {
            response.put("error", record.error);
            response.put("error_code", record.errorCode);
            if (record.details != null) {
                response.put("details", record.details);
            }
        }
        response.put("progress", 100);
        response.put("started_at", record.startedAt);
        response.put("execution_time_ms", record.executionTimeMs);
        response.put("message", record.message);
        sendJson(exchange, 200, response);
    }

    private static ObjectNode executeWorkflow(String executionID, JsonNode request, double elapsedBeforeRunMs) {
        JsonNode tasks = request.path("workflow_def").path("tasks");
        if (!tasks.isArray() || tasks.isEmpty()) {
            throw new IllegalArgumentException("workflow_def.tasks must be a non-empty array");
        }

        WorkflowExecutionContext context = new WorkflowExecutionContext();
        IWorkflow workflow = new WorkflowFactory().createDefaultWorkflow("addp_supermap_" + executionID);
        Map<String, IProcess> processes = new LinkedHashMap<>();
        Map<String, IProcessItem> items = new LinkedHashMap<>();
        Map<String, String> taskOperators = new LinkedHashMap<>();

        try {
            for (JsonNode task : tasks) {
                String id = requireText(task, "id");
                String operator = requireText(task, "operator");
                IProcess process = createProcess(operator, task.path("params"), context);
                if (processes.put(id, process) != null) {
                    throw new IllegalArgumentException("duplicate task id: " + id);
                }
                taskOperators.put(id, operator);
                workflow.addProcess(process);
                IProcessItem item = workflow.getItem(process);
                item.setId(id);
                items.put(id, item);
            }

            for (JsonNode task : tasks) {
                String id = requireText(task, "id");
                JsonNode params = task.path("params");
                if (!params.isObject()) {
                    continue;
                }
                params.fields().forEachRemaining(entry -> {
                    JsonNode value = entry.getValue();
                    if (!isRef(value)) {
                        return;
                    }
                    String dependencyID = value.path("$ref").asText();
                    IProcess from = processes.get(dependencyID);
                    IProcess to = processes.get(id);
                    if (from == null) {
                        throw new IllegalArgumentException("unknown dependency: " + dependencyID);
                    }
                    String fromPort = value.path("port").asText(defaultOutputPort(taskOperators.get(dependencyID)));
                    String toPort = entry.getKey();
                    workflow.connect(from, fromPort, to, toPort);
                });
            }

            long started = System.nanoTime();
            IWorkflowExecutor executor = WorkflowExecutors.getOrCreateExecutor(workflow);
            try {
                boolean ok = executor.execute();
                ObjectNode allResults = MAPPER.createObjectNode();
                JsonNode finalResult = MAPPER.nullNode();
                for (Map.Entry<String, IProcessItem> entry : items.entrySet()) {
                    String operator = taskOperators.get(entry.getKey());
                    ObjectNode summary = summarizeOutput(entry.getValue(), defaultOutputPort(operator));
                    allResults.set(entry.getKey(), summary);
                    finalResult = summary;
                }

                ObjectNode response = MAPPER.createObjectNode();
                response.put("status", ok ? "success" : "failed");
                response.put("execution_id", executionID);
                response.set("final_result", finalResult);
                response.set("all_results", allResults);
                response.put("execution_time_ms", elapsedBeforeRunMs + elapsedMs(started));
                response.set("lineage_events", lineageEvents(tasks, taskOperators));
                return response;
            } finally {
                executor.close();
            }
        } finally {
            context.close();
        }
    }

    private static IProcess createProcess(String operator, JsonNode params, WorkflowExecutionContext context) {
        return switch (operator) {
            case "datasource.open" -> new OpenDatasourceProcess(context, params);
            case "datasource.open_postgis" -> new OpenPostgisDatasourceProcess(context, params);
            case "datasource.create" -> new CreateDatasourceProcess(context, params);
            case "dataset.select" -> new SelectDatasetProcess(params);
            case "dataset.info" -> new DatasetInfoProcess(params);
            case "dataset.project" -> new DatasetProjectProcess(params);
            case "vector.filter" -> new VectorFilterProcess(params);
            case "vector.spatial_filter" -> new VectorSpatialFilterProcess(params);
            case "vector.buffer" -> new VectorBufferProcess(params);
            case "vector.dissolve" -> new VectorDissolveProcess(params);
            case "vector.merge" -> new VectorMergeProcess(params);
            case "vector.feature_envelope" -> new VectorFeatureEnvelopeProcess(params);
            case "vector.inner_point" -> new VectorInnerPointProcess(params);
            case "overlay.intersect", "overlay.clip", "overlay.erase", "overlay.union" -> new OverlayBinaryProcess(operator, params);
            case "vector.query" -> new VectorQueryProcess(params);
            case "dataset.save" -> new SaveDatasetProcess(params);
            default -> throw new IllegalArgumentException("unsupported operator: " + operator);
        };
    }

    private static ObjectNode summarizeOutput(IProcessItem item, String outputPort) {
        IDataItem<?> output = item.getOrCreateOutputDataItem(outputPort);
        Object value = output.getValue();
        if (value instanceof SuperMapDatasourceRef datasourceRef) {
            return datasourceRef.toJson();
        }
        if (value instanceof SuperMapDatasetRef datasetRef) {
            return datasetRef.toJson();
        }
        if (value instanceof DatasetInfoSummary datasetInfo) {
            return datasetInfo.toJson();
        }
        if (value instanceof QueryResult queryResult) {
            return queryResult.toJson();
        }
        ObjectNode summary = MAPPER.createObjectNode();
        summary.put("kind", "object");
        summary.put("object_type", value == null ? "null" : value.getClass().getName());
        if (value == null) {
            summary.set("preview", MAPPER.nullNode());
        } else {
            summary.put("preview", value.toString());
        }
        return summary;
    }

    private static ArrayNode lineageEvents(JsonNode tasks, Map<String, String> taskOperators) {
        ArrayNode events = MAPPER.createArrayNode();
        for (JsonNode task : tasks) {
            String taskID = task.path("id").asText();
            ObjectNode event = MAPPER.createObjectNode();
            event.put("event_type", "workflow.operator.executed");
            event.put("task_id", taskID);
            event.put("operator", taskOperators.getOrDefault(taskID, task.path("operator").asText()));
            event.put("storage", storageFor(taskOperators.get(taskID)));
            event.put("asset_ref", assetRefFor(task));
            events.add(event);
        }
        return events;
    }

    private static String storageFor(String operator) {
        if ("datasource.open".equals(operator) || "datasource.open_postgis".equals(operator) || "datasource.create".equals(operator)
                || "datasource.enable_postgis".equals(operator) || "overlay.intersect".equals(operator) || "overlay.clip".equals(operator)
                || "overlay.erase".equals(operator) || "overlay.union".equals(operator) || "vector.filter".equals(operator)
                || "vector.spatial_filter".equals(operator) || "vector.buffer".equals(operator) || "vector.dissolve".equals(operator)
                || "vector.merge".equals(operator) || "vector.feature_envelope".equals(operator) || "vector.inner_point".equals(operator)
                || "dataset.project".equals(operator) || "dataset.save".equals(operator)) {
            return "datasource";
        }
        return "memory";
    }

    private static String assetRefFor(JsonNode task) {
        JsonNode params = task.path("params");
        String path = params.path("path").asText("");
        if (!path.isBlank()) {
            return path;
        }
        String outputPath = params.path("output_path").asText("");
        return outputPath == null ? "" : outputPath;
    }

    private static Map<String, ObjectNode> operators() {
        Map<String, ObjectNode> result = new LinkedHashMap<>();
        result.put("datasource.open", operator(
                "datasource.open",
                "打开数据源",
                "打开已有 UDBX 数据源，输出运行时 Datasource 引用。",
                "数据源",
                List.of(
                        param("path", "string", false, true, "UDBX 文件路径。"),
                        param("alias", "string", false, false, "数据源别名。"),
                        param("read_only", "boolean", false, false, "是否只读打开，默认 true。")
                ),
                List.of(output("datasource", "supermap.datasource", "运行时 Datasource 引用。"))
        ));
        result.put("datasource.open_postgis", operator(
                "datasource.open_postgis",
                "打开 PostGIS 数据源",
                "打开已由 Develop 派生连接信息的已有 PostGIS 空间表所在数据源，不创建 SuperMap 系统表。",
                "数据源",
                List.of(
                        param("connection_info", "object", false, false, "运行时派生连接信息。"),
                        param("schema", "string", false, false, "运行时派生 schema。"),
                        param("table", "string", false, false, "运行时派生表名。"),
                        param("alias", "string", false, false, "数据源别名。"),
                        param("read_only", "boolean", false, false, "是否只读打开，默认 true。")
                ),
                List.of(output("datasource", "supermap.datasource", "运行时 Datasource 引用。"))
        ));
        result.put("datasource.enable_postgis", operator(
                "datasource.enable_postgis",
                "启用 PostGIS 空间工作区",
                "对已有 PostgreSQL/PostGIS 数据库执行 SuperMap SDX+ 初始化，可能创建 SuperMap 系统表。",
                "数据源",
                List.of(
                        param("connection_info", "object", false, true, "运行时派生连接信息。"),
                        param("alias", "string", false, false, "数据源别名。")
                ),
                List.of(output("workspace", "supermap.spatial_workspace", "SuperMap SDX+ 空间工作区摘要。")),
                List.of("direct")
        ));
        result.put("datasource.upgrade_udbx", operator(
                "datasource.upgrade_udbx",
                "升级 UDBX 数据源",
                "显式检查并原位升级已有 UDBX 的 SuperMap schema；只在旧 schema 时以可写方式打开。",
                "数据源",
                List.of(
                        param("connection_info", "object", false, true, "运行时派生连接信息。"),
                        param("path", "string", false, true, "目标 UDBX 文件路径；NFS 调用使用 export 内相对路径。"),
                        param("alias", "string", false, false, "数据源别名。")
                ),
                List.of(output("upgrade", "supermap.udbx_upgrade", "UDBX schema 升级结果摘要。")),
                List.of("direct")
        ));
        result.put("datasource.create", operator(
                "datasource.create",
                "创建数据源",
                "创建 UDBX 输出数据源，供后续空间分析或保存算子写入。",
                "数据源",
                List.of(
                        param("connection_info", "object", false, true, "运行时派生连接信息。"),
                        param("path", "string", false, true, "目标 UDBX 文件路径。"),
                        param("alias", "string", false, false, "数据源别名。"),
                        param("overwrite", "boolean", false, false, "目标文件存在时是否覆盖，默认 false。")
                ),
                List.of(output("datasource", "supermap.datasource", "运行时 Datasource 引用。"))
        ));
        result.put("dataset.select", operator(
                "dataset.select",
                "选择矢量数据集",
                "从 Datasource 中选择 DatasetVector。",
                "数据集",
                List.of(
                        param("datasource", "supermap.datasource", true, true, "上游 Datasource 引用。"),
                        param("dataset_name", "string", false, true, "数据集名称。")
                ),
                List.of(output("dataset", "supermap.dataset", "运行时 DatasetVector 引用。"))
        ));
        result.put("dataset.info", operator(
                "dataset.info",
                "数据集信息",
                "读取 DatasetVector 的字段、记录数、范围和坐标系摘要。",
                "数据集",
                List.of(
                        param("dataset", "supermap.dataset", true, true, "输入 DatasetVector。")
                ),
                List.of(output("info", "supermap.dataset_info", "数据集轻量信息摘要。"))
        ));
        result.put("dataset.project", operator(
                "dataset.project",
                "数据集投影转换",
                "把 DatasetVector 转换到目标 EPSG 坐标系，并写入目标 Datasource。",
                "数据集",
                List.of(
                        param("dataset", "supermap.dataset", true, true, "输入 DatasetVector。"),
                        param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                        param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                        param("target_epsg", "integer", false, true, "目标 EPSG 编码。"),
                        param("method", "string", false, false, "坐标转换方法，默认 geocentric_translation。"),
                        param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。")
                ),
                List.of(output("result_dataset", "supermap.dataset", "投影转换结果 DatasetVector 引用。"))
        ));
        result.put("vector.filter", operator(
                "vector.filter",
                "矢量属性过滤",
                "按 SuperMap 属性过滤表达式生成新的 DatasetVector，供下游分析继续使用。",
                "空间分析",
                List.of(
                        param("dataset", "supermap.dataset", true, true, "输入 DatasetVector。"),
                        param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                        param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                        param("attribute_filter", "string", false, true, "SuperMap 属性过滤表达式。"),
                        param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。")
                ),
                List.of(output("result_dataset", "supermap.dataset", "过滤结果 DatasetVector 引用。"))
        ));
        result.put("vector.spatial_filter", operator(
                "vector.spatial_filter",
                "矢量空间筛选",
                "按两个 DatasetVector 的空间关系筛选要素，并写入目标 Datasource。",
                "空间分析",
                List.of(
                        param("input_dataset", "supermap.dataset", true, true, "被筛选 DatasetVector。"),
                        param("filter_dataset", "supermap.dataset", true, true, "空间关系筛选 DatasetVector。"),
                        param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                        param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                        param("relation", "string", false, true, "空间关系，支持 intersect/contain/within/closest。"),
                        param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。")
                ),
                List.of(output("result_dataset", "supermap.dataset", "空间筛选结果 DatasetVector 引用。"))
        ));
        result.put("vector.buffer", operator(
                "vector.buffer",
                "缓冲区分析",
                "对 DatasetVector 执行缓冲区分析，并写入目标 Datasource。",
                "空间分析",
                List.of(
                        param("input_dataset", "supermap.dataset", true, true, "输入 DatasetVector。"),
                        param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                        param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                        param("distance", "float", false, true, "缓冲距离。"),
                        param("radius_unit", "string", false, false, "距离单位，支持 meter/kilometer/foot/mile，默认 meter。"),
                        param("end_type", "string", false, false, "线缓冲端点类型 round/flat，默认 round。"),
                        param("semicircle_segments", "integer", false, false, "半圆弧线段数，默认 10。"),
                        param("dissolve", "boolean", false, false, "是否融合缓冲结果，默认 false。"),
                        param("keep_attributes", "boolean", false, false, "是否保留属性，默认 true。"),
                        param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。")
                ),
                List.of(output("result_dataset", "supermap.dataset", "缓冲区结果 DatasetVector 引用。"))
        ));
        result.put("vector.dissolve", operator(
                "vector.dissolve",
                "矢量融合",
                "按字段融合 DatasetVector 要素，并写入目标 Datasource。",
                "空间分析",
                List.of(
                        param("input_dataset", "supermap.dataset", true, true, "输入 DatasetVector。"),
                        param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                        param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                        param("field_names", "array", false, false, "融合字段数组，或以逗号分隔的字段名。"),
                        param("dissolve_type", "string", false, false, "融合类型 single/multipart/only_multipart，默认 multipart。"),
                        param("tolerance", "float", false, false, "融合容差，默认 0。"),
                        param("save_all_fields", "boolean", false, false, "是否保留全部字段，默认 true。"),
                        param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。")
                ),
                List.of(output("result_dataset", "supermap.dataset", "融合结果 DatasetVector 引用。"))
        ));
        result.put("vector.merge", operator(
                "vector.merge",
                "矢量合并",
                "复制主 DatasetVector 后追加另一个 DatasetVector 的记录，生成合并结果。",
                "空间分析",
                List.of(
                        param("primary_dataset", "supermap.dataset", true, true, "主 DatasetVector。"),
                        param("append_dataset", "supermap.dataset", true, true, "追加 DatasetVector。"),
                        param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                        param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                        param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。")
                ),
                List.of(output("result_dataset", "supermap.dataset", "合并结果 DatasetVector 引用。"))
        ));
        result.put("vector.feature_envelope", operator(
                "vector.feature_envelope",
                "要素外接矩形",
                "为 DatasetVector 中每个要素生成外接矩形数据集。",
                "空间分析",
                List.of(
                        param("input_dataset", "supermap.dataset", true, true, "输入 DatasetVector。"),
                        param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                        param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                        param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。")
                ),
                List.of(output("result_dataset", "supermap.dataset", "外接矩形结果 DatasetVector 引用。"))
        ));
        result.put("vector.inner_point", operator(
                "vector.inner_point",
                "面内点提取",
                "从面 DatasetVector 生成内部点 DatasetVector。",
                "空间分析",
                List.of(
                        param("input_dataset", "supermap.dataset", true, true, "输入面 DatasetVector。"),
                        param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                        param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                        param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。")
                ),
                List.of(output("result_dataset", "supermap.dataset", "面内点结果 DatasetVector 引用。"))
        ));
        result.put("overlay.intersect", operator(
                "overlay.intersect",
                "叠加求交",
                "对两个 DatasetVector 执行 OverlayAnalyst.intersect，并写入目标 Datasource。",
                "空间分析",
                List.of(
                        param("input_dataset", "supermap.dataset", true, true, "源 DatasetVector。"),
                        param("overlay_dataset", "supermap.dataset", true, true, "叠加 DatasetVector。"),
                        param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                        param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                        param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。"),
                        param("tolerance", "float", false, false, "叠加容差，默认 0。")
                ),
                List.of(output("result_dataset", "supermap.dataset", "叠加分析结果 DatasetVector 引用。"))
        ));
        result.put("overlay.clip", operator(
                "overlay.clip",
                "叠加裁剪",
                "对两个 DatasetVector 执行 OverlayAnalyst.clip，并写入目标 Datasource。",
                "空间分析",
                overlayParameters(),
                List.of(output("result_dataset", "supermap.dataset", "裁剪结果 DatasetVector 引用。"))
        ));
        result.put("overlay.erase", operator(
                "overlay.erase",
                "叠加擦除",
                "对两个 DatasetVector 执行 OverlayAnalyst.erase，并写入目标 Datasource。",
                "空间分析",
                overlayParameters(),
                List.of(output("result_dataset", "supermap.dataset", "擦除结果 DatasetVector 引用。"))
        ));
        result.put("overlay.union", operator(
                "overlay.union",
                "叠加合并",
                "对两个 DatasetVector 执行 OverlayAnalyst.union，并写入目标 Datasource。",
                "空间分析",
                overlayParameters(),
                List.of(output("result_dataset", "supermap.dataset", "合并结果 DatasetVector 引用。"))
        ));
        result.put("vector.query", operator(
                "vector.query",
                "矢量属性查询",
                "对 DatasetVector 执行属性过滤并返回轻量查询摘要。",
                "空间分析",
                List.of(
                        param("dataset", "supermap.dataset", true, true, "输入 DatasetVector。"),
                        param("attribute_filter", "string", false, false, "SuperMap 属性过滤表达式。"),
                        param("max_records", "integer", false, false, "预留参数，当前仅返回总数。")
                ),
                List.of(output("query_result", "supermap.query_result", "查询结果摘要。"))
        ));
        result.put("dataset.save", operator(
                "dataset.save",
                "保存数据集",
                "把上游 DatasetVector 复制保存到目标 Datasource。",
                "数据集",
                List.of(
                        param("dataset", "supermap.dataset", true, true, "输入 DatasetVector。"),
                        param("target_datasource", "supermap.datasource", true, true, "目标 Datasource。"),
                        param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                        param("overwrite", "boolean", false, false, "目标数据集存在时是否覆盖，默认 false。")
                ),
                List.of(output("saved_dataset", "supermap.dataset", "保存后的 DatasetVector 引用。"))
        ));
        return result;
    }

    private static ObjectNode operator(
            String id,
            String displayName,
            String description,
            String category,
            List<ObjectNode> parameters,
            List<ObjectNode> outputPorts
    ) {
        return operator(id, displayName, description, category, parameters, outputPorts, List.of("workflow"));
    }

    private static ObjectNode operator(
            String id,
            String displayName,
            String description,
            String category,
            List<ObjectNode> parameters,
            List<ObjectNode> outputPorts,
            List<String> executionModes
    ) {
        ObjectNode op = MAPPER.createObjectNode();
        op.put("id", id);
        op.put("name", id);
        op.put("display_name", displayName);
        op.put("engine_type", ENGINE_TYPE);
        op.put("category", category);
        ArrayNode categoryPath = op.putArray("category_path");
        categoryPath.add(category);
        op.put("description", description);
        op.put("brief_description", displayName);
        ArrayNode modes = op.putArray("execution_modes");
        executionModes.forEach(modes::add);
        ArrayNode params = op.putArray("parameters");
        parameters.forEach(params::add);
        ArrayNode outputs = op.putArray("output_ports");
        outputPorts.forEach(outputs::add);
        return op;
    }

    private static ObjectNode param(String name, String type, boolean workflowInput, boolean required, String description) {
        ObjectNode parameter = MAPPER.createObjectNode();
        parameter.put("name", name);
        parameter.put("type", type);
        parameter.put("param_type", workflowInput ? "input" : "param");
        parameter.put("required", required);
        parameter.put("description", description);
        return parameter;
    }

    private static ObjectNode output(String name, String type, String description) {
        ObjectNode output = MAPPER.createObjectNode();
        output.put("name", name);
        output.put("type", type);
        output.put("description", description);
        output.put("is_default", true);
        return output;
    }

    private static List<ObjectNode> overlayParameters() {
        return List.of(
                param("input_dataset", "supermap.dataset", true, true, "源 DatasetVector。"),
                param("overlay_dataset", "supermap.dataset", true, true, "叠加 DatasetVector。"),
                param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。"),
                param("tolerance", "float", false, false, "叠加容差，默认 0。")
        );
    }

    private static String defaultOutputPort(String operator) {
        return switch (operator) {
            case "datasource.open", "datasource.open_postgis", "datasource.create" -> "datasource";
            case "dataset.select" -> "dataset";
            case "dataset.info" -> "info";
            case "dataset.project", "overlay.intersect", "overlay.clip", "overlay.erase", "overlay.union", "vector.filter",
                    "vector.spatial_filter", "vector.buffer", "vector.dissolve", "vector.merge", "vector.feature_envelope",
                    "vector.inner_point" -> "result_dataset";
            case "vector.query" -> "query_result";
            case "dataset.save" -> "saved_dataset";
            default -> "out";
        };
    }

    private static boolean isRef(JsonNode value) {
        return value != null && value.isObject() && value.has("$ref") && value.path("$ref").isTextual();
    }

    private static String requireText(JsonNode node, String field) {
        String value = node.path(field).asText(null);
        if (value == null || value.isBlank()) {
            throw new IllegalArgumentException("task." + field + " is required");
        }
        return value;
    }

    private static String paramText(JsonNode params, String field) {
        String value = params.path(field).asText(null);
        if (value == null || value.isBlank()) {
            throw new IllegalArgumentException("params." + field + " is required");
        }
        return value;
    }

    private static String optionalText(JsonNode params, String field, String defaultValue) {
        String value = params.path(field).asText(null);
        return value == null || value.isBlank() ? defaultValue : value;
    }

    private static JsonNode requireObject(JsonNode params, String field) {
        JsonNode value = params.path(field);
        if (!value.isObject()) {
            throw new IllegalArgumentException("params." + field + " must be an object");
        }
        return value;
    }

    private static String requireConnText(JsonNode connInfo, String field) {
        String value = optionalConnText(connInfo, field, "");
        if (value.isBlank()) {
            throw new IllegalArgumentException("connection_info." + field + " is required");
        }
        return value;
    }

    private static String optionalConnText(JsonNode connInfo, String field, String defaultValue) {
        JsonNode value = connInfo.path(field);
        if (value.isMissingNode() || value.isNull()) {
            return defaultValue;
        }
        String text = value.asText("");
        return text.isBlank() ? defaultValue : text.trim();
    }

    private static String connectionEngineType(JsonNode connInfo) {
        return optionalConnText(connInfo, "engine_type", "");
    }

    private static boolean isNfsConnection(JsonNode connInfo) {
        return "nfs".equalsIgnoreCase(connectionEngineType(connInfo));
    }

    private static UdbxSchemaState inspectUdbxSchema(Path path) {
        if (!Files.isRegularFile(path)) {
            throw new IllegalArgumentException("UDBX file does not exist: " + path);
        }
        String sql = "SELECT 'table|' || name FROM sqlite_master "
                + "WHERE type='table' AND name IN ('SmAdditionalInfo','SmAttributeRule','SmGroupItems','SmPyramidColumns');"
                + "SELECT 'column|' || name FROM pragma_table_info('SmRegister') "
                + "WHERE name IN ('SmGroupID','SmRelationship','SmSubTypes');";
        try {
            Process process = new ProcessBuilder("sqlite3", "-readonly", path.toString(), sql)
                    .redirectErrorStream(true)
                    .start();
            if (!process.waitFor(10, TimeUnit.SECONDS)) {
                process.destroyForcibly();
                throw new IllegalStateException("timed out while inspecting UDBX schema: " + path);
            }
            String output;
            try (InputStream input = process.getInputStream()) {
                output = readAll(input);
            }
            if (process.exitValue() != 0) {
                throw new IllegalArgumentException("failed to inspect UDBX schema: " + output.trim());
            }

            List<String> tables = new ArrayList<>();
            List<String> registerColumns = new ArrayList<>();
            output.lines().forEach(line -> {
                if (line.startsWith("table|")) {
                    tables.add(line.substring("table|".length()));
                } else if (line.startsWith("column|")) {
                    registerColumns.add(line.substring("column|".length()));
                }
            });
            return new UdbxSchemaState(
                    missingValues(CURRENT_UDBX_TABLES, tables),
                    missingValues(CURRENT_UDBX_REGISTER_COLUMNS, registerColumns)
            );
        } catch (IOException ex) {
            throw new IllegalStateException("failed to run sqlite3 for UDBX schema inspection", ex);
        } catch (InterruptedException ex) {
            Thread.currentThread().interrupt();
            throw new IllegalStateException("interrupted while inspecting UDBX schema", ex);
        }
    }

    private static List<String> missingValues(List<String> required, List<String> actual) {
        List<String> missing = new ArrayList<>();
        for (String value : required) {
            if (!actual.contains(value)) {
                missing.add(value);
            }
        }
        return List.copyOf(missing);
    }

    private static Path resolveUdbxPath(JsonNode connInfo, String outputPath) {
        if (connInfo == null || !connInfo.isObject() || !isNfsConnection(connInfo)) {
            return Path.of(outputPath);
        }
        String server = normalizeResourceHost(requireConnText(connInfo, "server"));
        String exportPath = requireConnText(connInfo, "export_path");
        String nfsVersion = optionalConnText(connInfo, "nfs_version", optionalConnText(connInfo, "version", ""));
        Path relativePath = normalizeNfsRelativePath(outputPath);
        Path mountRoot = dynamicNfsMountRoot(server, exportPath);
        ensureNfsMounted(server, exportPath, nfsVersion, mountRoot);
        return mountRoot.resolve(relativePath).normalize();
    }

    private static Path normalizeNfsRelativePath(String outputPath) {
        if (outputPath == null || outputPath.isBlank()) {
            throw new IllegalArgumentException("params.path is required for NFS UDBX output");
        }
        String normalizedText = outputPath.trim().replace('\\', '/');
        if (normalizedText.startsWith("/") || normalizedText.contains("://")) {
            throw new IllegalArgumentException("NFS UDBX output path must be relative to the selected ADDP NFS root: " + outputPath);
        }
        try {
            Path normalized = Path.of(normalizedText).normalize();
            if (normalized.isAbsolute() || normalized.toString().isBlank() || normalized.startsWith("..")) {
                throw new IllegalArgumentException("NFS UDBX output path escapes the selected ADDP NFS root: " + outputPath);
            }
            return normalized;
        } catch (InvalidPathException ex) {
            throw new IllegalArgumentException("invalid NFS UDBX output path: " + outputPath, ex);
        }
    }

    private static Path dynamicNfsMountRoot(String server, String exportPath) {
        String baseDir = System.getenv().getOrDefault("SUPERMAP_DYNAMIC_NFS_MOUNT_BASE", "/mnt/addp-dynamic-nfs");
        return Path.of(baseDir).resolve(sha256Hex(server + "|" + exportPath).substring(0, 16));
    }

    private static void ensureNfsMounted(String server, String exportPath, String nfsVersion, Path mountRoot) {
        try {
            Files.createDirectories(mountRoot);
            if (isMountPoint(mountRoot)) {
                return;
            }
            List<String> outputs = new ArrayList<>();
            for (String options : nfsMountOptionCandidates(nfsVersion)) {
                List<String> command = new ArrayList<>(Arrays.asList(
                        "mount",
                        "-t",
                        "nfs",
                        "-o",
                        options,
                        server + ":" + exportPath,
                        mountRoot.toString()
                ));
                Process process = new ProcessBuilder(command).redirectErrorStream(true).start();
                String output;
                try (InputStream input = process.getInputStream()) {
                    output = readAll(input);
                }
                int exit = process.waitFor();
                outputs.add("options=" + options + ", exit=" + exit + ", output=" + output.trim());
                if (exit == 0 || isMountPoint(mountRoot)) {
                    return;
                }
            }
            throw new IllegalStateException(
                    "failed to dynamically mount NFS export " + server + ":" + exportPath
                            + " to " + mountRoot + ". The SuperMap workflow container must include nfs-common "
                            + "and run with mount permission. mount attempts: " + String.join(" | ", outputs)
            );
        } catch (IOException ex) {
            throw new IllegalStateException(
                    "failed to dynamically mount NFS export " + server + ":" + exportPath
                            + ". The SuperMap workflow container must include nfs-common and run with mount permission.",
                    ex
            );
        } catch (InterruptedException ex) {
            Thread.currentThread().interrupt();
            throw new IllegalStateException("interrupted while dynamically mounting NFS export " + server + ":" + exportPath, ex);
        }
    }

    private static List<String> nfsMountOptionCandidates(String nfsVersion) {
        String version = nfsVersion == null ? "" : nfsVersion.trim();
        if (!version.isBlank()) {
            return List.of(nfsMountOptions(version));
        }
        return List.of(nfsMountOptions("4"), nfsMountOptions("3"));
    }

    private static String nfsMountOptions(String nfsVersion) {
        return "vers=" + nfsVersion + ",tcp,nolock,proto=tcp";
    }

    private static boolean isMountPoint(Path path) {
        try {
            Process process = new ProcessBuilder("mountpoint", "-q", path.toString()).start();
            return process.waitFor() == 0;
        } catch (IOException | InterruptedException ex) {
            if (ex instanceof InterruptedException) {
                Thread.currentThread().interrupt();
            }
            return false;
        }
    }

    private static String sha256Hex(String value) {
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            return HexFormat.of().formatHex(digest.digest(value.getBytes(StandardCharsets.UTF_8)));
        } catch (NoSuchAlgorithmException ex) {
            throw new IllegalStateException("SHA-256 digest is unavailable", ex);
        }
    }

    private static String postgisServer(JsonNode connInfo) {
        String host = normalizeResourceHost(requireConnText(connInfo, "host"));
        String port = optionalConnText(connInfo, "port", "");
        if (port.isBlank()) {
            return host;
        }
        return host + ":" + port;
    }

    private static String normalizeResourceHost(String host) {
        if (!isLocalhost(host)) {
            return host;
        }
        String alias = System.getenv().getOrDefault("SUPERMAP_RESOURCE_LOCALHOST_ALIAS", "").trim();
        return alias.isBlank() ? host : alias;
    }

    private static boolean isLocalhost(String host) {
        String normalized = host == null ? "" : host.trim().toLowerCase();
        return "localhost".equals(normalized) || "127.0.0.1".equals(normalized) || "::1".equals(normalized);
    }

    private static String defaultPostgisAlias(JsonNode params) {
        String schema = optionalText(params, "schema", "");
        String table = optionalText(params, "table", "");
        if (!schema.isBlank() && !table.isBlank()) {
            return schema + "_" + table;
        }
        if (!table.isBlank()) {
            return table;
        }
        return "postgis";
    }

    private static boolean optionalBoolean(JsonNode params, String field, boolean defaultValue) {
        return params.has(field) && !params.path(field).isNull() ? params.path(field).asBoolean(defaultValue) : defaultValue;
    }

    private static double optionalDouble(JsonNode params, String field, double defaultValue) {
        return params.has(field) && !params.path(field).isNull() ? params.path(field).asDouble(defaultValue) : defaultValue;
    }

    private static double requiredDouble(JsonNode params, String field) {
        if (!params.has(field) || params.path(field).isNull()) {
            throw new IllegalArgumentException("params." + field + " is required");
        }
        return params.path(field).asDouble();
    }

    private static int requiredInt(JsonNode params, String field) {
        if (!params.has(field) || params.path(field).isNull()) {
            throw new IllegalArgumentException("params." + field + " is required");
        }
        return params.path(field).asInt();
    }

    private static int optionalInt(JsonNode params, String field, int defaultValue) {
        return params.has(field) && !params.path(field).isNull() ? params.path(field).asInt(defaultValue) : defaultValue;
    }

    private static String[] stringArrayParam(JsonNode params, String field) {
        JsonNode value = params.path(field);
        if (value.isMissingNode() || value.isNull()) {
            return new String[0];
        }
        List<String> values = new ArrayList<>();
        if (value.isArray()) {
            for (JsonNode item : value) {
                String text = item.asText("").trim();
                if (!text.isBlank()) {
                    values.add(text);
                }
            }
        } else {
            String raw = value.asText("").trim();
            if (!raw.isBlank()) {
                for (String part : raw.split(",")) {
                    String text = part.trim();
                    if (!text.isBlank()) {
                        values.add(text);
                    }
                }
            }
        }
        return values.toArray(new String[0]);
    }

    private static BufferRadiusUnit bufferRadiusUnit(String value) {
        return switch ((value == null ? "" : value.trim().toLowerCase()).replace("_", "")) {
            case "", "meter", "metre", "m" -> BufferRadiusUnit.Meter;
            case "kilometer", "kilometre", "km" -> BufferRadiusUnit.KiloMeter;
            case "millimeter", "millimetre", "mm" -> BufferRadiusUnit.MiliMeter;
            case "centimeter", "centimetre", "cm" -> BufferRadiusUnit.CentiMeter;
            case "decimeter", "decimetre", "dm" -> BufferRadiusUnit.DeciMeter;
            case "yard", "yd" -> BufferRadiusUnit.Yard;
            case "inch", "in" -> BufferRadiusUnit.Inch;
            case "foot", "feet", "ft" -> BufferRadiusUnit.Foot;
            case "mile", "mi" -> BufferRadiusUnit.Mile;
            default -> throw new IllegalArgumentException("unsupported buffer radius_unit: " + value);
        };
    }

    private static BufferEndType bufferEndType(String value) {
        return switch ((value == null ? "" : value.trim().toLowerCase())) {
            case "", "round" -> BufferEndType.ROUND;
            case "flat" -> BufferEndType.FLAT;
            default -> throw new IllegalArgumentException("unsupported buffer end_type: " + value);
        };
    }

    private static CoordSysTransMethod coordSysTransMethod(String value) {
        return switch ((value == null ? "" : value.trim().toLowerCase()).replace("-", "_")) {
            case "", "geocentric_translation", "mth_geocentric_translation" -> CoordSysTransMethod.MTH_GEOCENTRIC_TRANSLATION;
            case "molodensky", "mth_molodensky" -> CoordSysTransMethod.MTH_MOLODENSKY;
            case "molodensky_abridged", "mth_molodensky_abridged" -> CoordSysTransMethod.MTH_MOLODENSKY_ABRIDGED;
            case "position_vector", "mth_position_vector" -> CoordSysTransMethod.MTH_POSITION_VECTOR;
            case "coordinate_frame", "mth_coordinate_frame" -> CoordSysTransMethod.MTH_COORDINATE_FRAME;
            case "bursa_wolf", "mth_bursa_wolf" -> CoordSysTransMethod.MTH_BURSA_WOLF;
            case "prj4", "mth_prj4" -> CoordSysTransMethod.MTH_Prj4;
            case "bd09_to_gcj02" -> CoordSysTransMethod.BD09toGCJ02;
            case "gcj02_to_bd09" -> CoordSysTransMethod.GCJ02TOBD09;
            case "gcj02_to_wgs84" -> CoordSysTransMethod.GCJ02TOWGS84;
            case "wgs84_to_gcj02" -> CoordSysTransMethod.WGS84TOGCJ02;
            default -> throw new IllegalArgumentException("unsupported coordinate transform method: " + value);
        };
    }

    private static SpatialRelationType spatialRelationType(String value) {
        return switch ((value == null ? "" : value.trim().toLowerCase()).replace("-", "_")) {
            case "intersect", "intersects" -> SpatialRelationType.INTERSECT;
            case "contain", "contains" -> SpatialRelationType.CONTAIN;
            case "within" -> SpatialRelationType.WITHIN;
            case "closest" -> SpatialRelationType.CLOSEST;
            default -> throw new IllegalArgumentException("unsupported spatial relation: " + value);
        };
    }

    private static DissolveType dissolveType(String value) {
        return switch ((value == null ? "" : value.trim().toLowerCase()).replace("-", "_")) {
            case "", "multipart", "multi_part" -> DissolveType.MULTIPART;
            case "single" -> DissolveType.SINGLE;
            case "only_multipart", "only_multi_part" -> DissolveType.ONLYMULTIPART;
            default -> throw new IllegalArgumentException("unsupported dissolve_type: " + value);
        };
    }

    private static boolean executeOverlay(String operator, DatasetVector input, DatasetVector overlay, DatasetVector output, OverlayAnalystParameter parameter) {
        return switch (operator) {
            case "overlay.intersect" -> OverlayAnalyst.intersect(input, overlay, output, parameter);
            case "overlay.clip" -> OverlayAnalyst.clip(input, overlay, output, parameter);
            case "overlay.erase" -> OverlayAnalyst.erase(input, overlay, output, parameter);
            case "overlay.union" -> OverlayAnalyst.union(input, overlay, output, parameter);
            default -> throw new IllegalArgumentException("unsupported overlay operator: " + operator);
        };
    }

    private static String overlayMethodName(String operator) {
        return switch (operator) {
            case "overlay.intersect" -> "intersect";
            case "overlay.clip" -> "clip";
            case "overlay.erase" -> "erase";
            case "overlay.union" -> "union";
            default -> operator;
        };
    }

    private static boolean method(HttpExchange exchange, String method) {
        return method.equals(exchange.getRequestMethod());
    }

    private static String queryParam(HttpExchange exchange, String name) {
        String query = exchange.getRequestURI().getRawQuery();
        if (query == null || query.isBlank()) {
            return null;
        }
        for (String part : query.split("&")) {
            String[] kv = part.split("=", 2);
            if (kv.length == 2 && name.equals(kv[0])) {
                return URLDecoder.decode(kv[1], StandardCharsets.UTF_8);
            }
        }
        return null;
    }

    private static ObjectNode failed(String code, String message) {
        ObjectNode result = MAPPER.createObjectNode();
        result.put("status", "failed");
        result.put("error_code", code);
        result.put("error", message);
        return result;
    }

    private static String readAll(InputStream input) throws IOException {
        return new String(input.readAllBytes(), StandardCharsets.UTF_8);
    }

    private static void sendJson(HttpExchange exchange, int statusCode, JsonNode body) throws IOException {
        byte[] bytes = MAPPER.writeValueAsBytes(body);
        exchange.getResponseHeaders().set("Content-Type", "application/json; charset=utf-8");
        exchange.sendResponseHeaders(statusCode, bytes.length);
        try (OutputStream output = exchange.getResponseBody()) {
            output.write(bytes);
        }
    }

    private static double elapsedMs(long startedNanos) {
        return (System.nanoTime() - startedNanos) / 1_000_000.0;
    }

    public abstract static class BaseProcess extends AbstractProcess {
        BaseProcess(String name) {
            super("addp.supermap.workflow", name);
            this.parameters = new ParametersImpl(this);
        }

        @Override
        public String getFactory() {
            return SPS_FACTORY;
        }

        protected void addStringInput(String name, String value) {
            ISingleDataDefinition<String> def = new DefaultSingleDataDefinition<>(String.class);
            SingleInputImpl<String> input = new SingleInputImpl<>(name, def);
            input.setValueProvider(new ConstantValueProvider<>(def, value));
            this.parameters.addInput(input);
        }

        protected void addBooleanInput(String name, boolean value) {
            ISingleDataDefinition<Boolean> def = new DefaultSingleDataDefinition<>(Boolean.class);
            SingleInputImpl<Boolean> input = new SingleInputImpl<>(name, def);
            input.setValueProvider(new ConstantValueProvider<>(def, value));
            this.parameters.addInput(input);
        }

        protected void addDoubleInput(String name, double value) {
            ISingleDataDefinition<Double> def = new DefaultSingleDataDefinition<>(Double.class);
            SingleInputImpl<Double> input = new SingleInputImpl<>(name, def);
            input.setValueProvider(new ConstantValueProvider<>(def, value));
            this.parameters.addInput(input);
        }

        protected void addIntegerInput(String name, int value) {
            ISingleDataDefinition<Integer> def = new DefaultSingleDataDefinition<>(Integer.class);
            SingleInputImpl<Integer> input = new SingleInputImpl<>(name, def);
            input.setValueProvider(new ConstantValueProvider<>(def, value));
            this.parameters.addInput(input);
        }

        protected void addStringArrayInput(String name, String[] value) {
            ISingleDataDefinition<String[]> def = new DefaultSingleDataDefinition<>(String[].class);
            SingleInputImpl<String[]> input = new SingleInputImpl<>(name, def);
            input.setValueProvider(new ConstantValueProvider<>(def, value));
            this.parameters.addInput(input);
        }

        protected <T> void addObjectInput(String name, Class<T> type) {
            ISingleDataDefinition<T> def = new DefaultSingleDataDefinition<>(type);
            SingleInputImpl<T> input = new SingleInputImpl<>(name, def);
            input.setRequired(true);
            this.parameters.addInput(input);
        }

        protected <T> SingleOutputImpl<T> addObjectOutput(String name, Class<T> type) {
            ISingleDataDefinition<T> def = new DefaultSingleDataDefinition<>(type);
            SingleOutputImpl<T> output = new SingleOutputImpl<>(name, def);
            this.parameters.addOutput(output);
            return output;
        }
    }

    public static final class OpenDatasourceProcess extends BaseProcess {
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

    public static final class OpenPostgisDatasourceProcess extends BaseProcess {
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
            output.setValue(context.openPostgis(server, database, user, password, schema, table, alias, readOnly));
            return true;
        }
    }

    public static final class CreateDatasourceProcess extends BaseProcess {
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

    public static final class SelectDatasetProcess extends BaseProcess {
        private final SingleOutputImpl<SuperMapDatasetRef> output;

        SelectDatasetProcess(JsonNode params) {
            super("dataset.select");
            addObjectInput("datasource", SuperMapDatasourceRef.class);
            addStringInput("dataset_name", paramText(params, "dataset_name"));
            this.output = addObjectOutput("dataset", SuperMapDatasetRef.class);
        }

        @Override
        public boolean execute() {
            SuperMapDatasourceRef datasource = (SuperMapDatasourceRef) parameters.getInput("datasource").getValue();
            String datasetName = String.valueOf(parameters.getInput("dataset_name").getValue());
            Dataset dataset = datasource.datasource.getDatasets().get(datasetName);
            if (!(dataset instanceof DatasetVector datasetVector)) {
                throw new IllegalArgumentException("dataset is not a DatasetVector: " + datasetName);
            }
            output.setValue(new SuperMapDatasetRef(datasource, datasetVector));
            return true;
        }
    }

    public static final class DatasetInfoProcess extends BaseProcess {
        private final SingleOutputImpl<DatasetInfoSummary> output;

        DatasetInfoProcess(JsonNode params) {
            super("dataset.info");
            addObjectInput("dataset", SuperMapDatasetRef.class);
            this.output = addObjectOutput("info", DatasetInfoSummary.class);
        }

        @Override
        public boolean execute() {
            SuperMapDatasetRef dataset = (SuperMapDatasetRef) parameters.getInput("dataset").getValue();
            output.setValue(DatasetInfoSummary.from(dataset.dataset));
            return true;
        }
    }

    public static final class VectorFilterProcess extends BaseProcess {
        private final SingleOutputImpl<SuperMapDatasetRef> output;

        VectorFilterProcess(JsonNode params) {
            super("vector.filter");
            addObjectInput("dataset", SuperMapDatasetRef.class);
            addObjectInput("output_datasource", SuperMapDatasourceRef.class);
            addStringInput("output_dataset_name", paramText(params, "output_dataset_name"));
            addStringInput("attribute_filter", paramText(params, "attribute_filter"));
            addBooleanInput("overwrite", optionalBoolean(params, "overwrite", false));
            this.output = addObjectOutput("result_dataset", SuperMapDatasetRef.class);
        }

        @Override
        public boolean execute() {
            SuperMapDatasetRef dataset = (SuperMapDatasetRef) parameters.getInput("dataset").getValue();
            SuperMapDatasourceRef outputDatasource = (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
            String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
            String filter = String.valueOf(parameters.getInput("attribute_filter").getValue());
            boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());

            ensureDatasetNameAvailable(outputDatasource.datasource, outputDatasetName, overwrite);
            QueryParameter queryParameter = new QueryParameter();
            queryParameter.setAttributeFilter(filter);
            queryParameter.setCursorType(CursorType.STATIC);
            Recordset recordset = dataset.dataset.query(queryParameter);
            try {
                if (recordset == null) {
                    throw new IllegalStateException("dataset query returned null recordset");
                }
                DatasetVector result = outputDatasource.datasource.recordsetToDataset(recordset, outputDatasetName);
                if (result == null) {
                    throw new IllegalStateException("recordsetToDataset returned null: " + outputDatasetName);
                }
                inheritProjection(result, dataset.dataset);
                output.setValue(new SuperMapDatasetRef(outputDatasource, result));
            } finally {
                if (recordset != null) {
                    recordset.dispose();
                }
                queryParameter.dispose();
            }
            return true;
        }
    }

    public static final class DatasetProjectProcess extends BaseProcess {
        private final SingleOutputImpl<SuperMapDatasetRef> output;

        DatasetProjectProcess(JsonNode params) {
            super("dataset.project");
            addObjectInput("dataset", SuperMapDatasetRef.class);
            addObjectInput("output_datasource", SuperMapDatasourceRef.class);
            addStringInput("output_dataset_name", paramText(params, "output_dataset_name"));
            addIntegerInput("target_epsg", requiredInt(params, "target_epsg"));
            addStringInput("method", optionalText(params, "method", "geocentric_translation"));
            addBooleanInput("overwrite", optionalBoolean(params, "overwrite", false));
            this.output = addObjectOutput("result_dataset", SuperMapDatasetRef.class);
        }

        @Override
        public boolean execute() {
            SuperMapDatasetRef dataset = (SuperMapDatasetRef) parameters.getInput("dataset").getValue();
            SuperMapDatasourceRef outputDatasource = (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
            String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
            int targetEPSG = (Integer) parameters.getInput("target_epsg").getValue();
            String method = String.valueOf(parameters.getInput("method").getValue());
            boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());

            ensureDatasetNameAvailable(outputDatasource.datasource, outputDatasetName, overwrite);
            PrjCoordSys target = PrjCoordSys.fromEPSG(targetEPSG);
            if (target == null) {
                target = new PrjCoordSys();
                if (!target.fromEPSGCode(targetEPSG)) {
                    target.dispose();
                    throw new IllegalArgumentException("unsupported target_epsg: " + targetEPSG);
                }
            }
            CoordSysTransParameter parameter = new CoordSysTransParameter();
            try {
                Dataset projected = CoordSysTranslator.convert(
                        dataset.dataset,
                        target,
                        outputDatasource.datasource,
                        outputDatasetName,
                        parameter,
                        coordSysTransMethod(method)
                );
                if (!(projected instanceof DatasetVector projectedVector)) {
                    throw new IllegalStateException("CoordSysTranslator.convert did not return DatasetVector: " + outputDatasetName);
                }
                output.setValue(new SuperMapDatasetRef(outputDatasource, projectedVector));
            } finally {
                parameter.dispose();
                target.dispose();
            }
            return true;
        }
    }

    public static final class VectorSpatialFilterProcess extends BaseProcess {
        private final SingleOutputImpl<SuperMapDatasetRef> output;

        VectorSpatialFilterProcess(JsonNode params) {
            super("vector.spatial_filter");
            addObjectInput("input_dataset", SuperMapDatasetRef.class);
            addObjectInput("filter_dataset", SuperMapDatasetRef.class);
            addObjectInput("output_datasource", SuperMapDatasourceRef.class);
            addStringInput("output_dataset_name", paramText(params, "output_dataset_name"));
            addStringInput("relation", paramText(params, "relation"));
            addBooleanInput("overwrite", optionalBoolean(params, "overwrite", false));
            this.output = addObjectOutput("result_dataset", SuperMapDatasetRef.class);
        }

        @Override
        public boolean execute() {
            SuperMapDatasetRef input = (SuperMapDatasetRef) parameters.getInput("input_dataset").getValue();
            SuperMapDatasetRef filter = (SuperMapDatasetRef) parameters.getInput("filter_dataset").getValue();
            SuperMapDatasourceRef outputDatasource = (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
            String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
            String relation = String.valueOf(parameters.getInput("relation").getValue());
            boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());

            ensureDatasetNameAvailable(outputDatasource.datasource, outputDatasetName, overwrite);
            int[] ids = input.dataset.getIDsByGeoRelation(filter.dataset, spatialRelationType(relation), true, false);
            Recordset recordset = input.dataset.query(ids == null ? new int[0] : ids, CursorType.STATIC);
            try {
                if (recordset == null) {
                    throw new IllegalStateException("spatial relation query returned null recordset");
                }
                DatasetVector result = outputDatasource.datasource.recordsetToDataset(recordset, outputDatasetName);
                if (result == null) {
                    throw new IllegalStateException("recordsetToDataset returned null: " + outputDatasetName);
                }
                inheritProjection(result, input.dataset);
                output.setValue(new SuperMapDatasetRef(outputDatasource, result));
            } finally {
                if (recordset != null) {
                    recordset.dispose();
                }
            }
            return true;
        }
    }

    public static final class VectorBufferProcess extends BaseProcess {
        private final SingleOutputImpl<SuperMapDatasetRef> output;

        VectorBufferProcess(JsonNode params) {
            super("vector.buffer");
            addObjectInput("input_dataset", SuperMapDatasetRef.class);
            addObjectInput("output_datasource", SuperMapDatasourceRef.class);
            addStringInput("output_dataset_name", paramText(params, "output_dataset_name"));
            addDoubleInput("distance", requiredDouble(params, "distance"));
            addStringInput("radius_unit", optionalText(params, "radius_unit", "meter"));
            addStringInput("end_type", optionalText(params, "end_type", "round"));
            addIntegerInput("semicircle_segments", optionalInt(params, "semicircle_segments", 10));
            addBooleanInput("dissolve", optionalBoolean(params, "dissolve", false));
            addBooleanInput("keep_attributes", optionalBoolean(params, "keep_attributes", true));
            addBooleanInput("overwrite", optionalBoolean(params, "overwrite", false));
            this.output = addObjectOutput("result_dataset", SuperMapDatasetRef.class);
        }

        @Override
        public boolean execute() {
            SuperMapDatasetRef input = (SuperMapDatasetRef) parameters.getInput("input_dataset").getValue();
            SuperMapDatasourceRef outputDatasource = (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
            String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
            double distance = (Double) parameters.getInput("distance").getValue();
            String radiusUnit = String.valueOf(parameters.getInput("radius_unit").getValue());
            String endType = String.valueOf(parameters.getInput("end_type").getValue());
            int semicircleSegments = (Integer) parameters.getInput("semicircle_segments").getValue();
            boolean dissolve = Boolean.TRUE.equals(parameters.getInput("dissolve").getValue());
            boolean keepAttributes = Boolean.TRUE.equals(parameters.getInput("keep_attributes").getValue());
            boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());

            DatasetVector outputDataset = createOutputDataset(outputDatasource.datasource, outputDatasetName, DatasetType.REGION, overwrite, input.dataset);
            BufferAnalystParameter parameter = new BufferAnalystParameter();
            parameter.setLeftDistance(distance);
            parameter.setRightDistance(distance);
            parameter.setRadiusUnit(bufferRadiusUnit(radiusUnit));
            parameter.setEndType(bufferEndType(endType));
            parameter.setSemicircleLineSegment(semicircleSegments);
            boolean ok = BufferAnalyst.createBuffer(input.dataset, outputDataset, parameter, dissolve, keepAttributes);
            if (!ok) {
                throw new IllegalStateException("BufferAnalyst.createBuffer returned false");
            }
            output.setValue(new SuperMapDatasetRef(outputDatasource, outputDataset));
            return true;
        }
    }

    public static final class VectorDissolveProcess extends BaseProcess {
        private final SingleOutputImpl<SuperMapDatasetRef> output;

        VectorDissolveProcess(JsonNode params) {
            super("vector.dissolve");
            addObjectInput("input_dataset", SuperMapDatasetRef.class);
            addObjectInput("output_datasource", SuperMapDatasourceRef.class);
            addStringInput("output_dataset_name", paramText(params, "output_dataset_name"));
            addStringArrayInput("field_names", stringArrayParam(params, "field_names"));
            addStringInput("dissolve_type", optionalText(params, "dissolve_type", "multipart"));
            addDoubleInput("tolerance", optionalDouble(params, "tolerance", 0));
            addBooleanInput("save_all_fields", optionalBoolean(params, "save_all_fields", true));
            addBooleanInput("overwrite", optionalBoolean(params, "overwrite", false));
            this.output = addObjectOutput("result_dataset", SuperMapDatasetRef.class);
        }

        @Override
        public boolean execute() {
            SuperMapDatasetRef input = (SuperMapDatasetRef) parameters.getInput("input_dataset").getValue();
            SuperMapDatasourceRef outputDatasource = (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
            String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
            String[] fieldNames = (String[]) parameters.getInput("field_names").getValue();
            String type = String.valueOf(parameters.getInput("dissolve_type").getValue());
            double tolerance = (Double) parameters.getInput("tolerance").getValue();
            boolean saveAllFields = Boolean.TRUE.equals(parameters.getInput("save_all_fields").getValue());
            boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());

            ensureDatasetNameAvailable(outputDatasource.datasource, outputDatasetName, overwrite);
            DissolveParameter parameter = new DissolveParameter();
            try {
                parameter.setPreProcess(true);
                if (fieldNames.length > 0) {
                    parameter.setFieldNames(fieldNames);
                }
                parameter.setDissolveType(dissolveType(type));
                parameter.setTolerance(tolerance);
                parameter.setSaveAllField(saveAllFields);
                DatasetVector result = Generalization.dissolve(input.dataset, outputDatasource.datasource, outputDatasetName, parameter);
                if (result == null) {
                    throw new IllegalStateException("Generalization.dissolve returned null: " + outputDatasetName);
                }
                output.setValue(new SuperMapDatasetRef(outputDatasource, result));
            } finally {
                parameter.dispose();
            }
            return true;
        }
    }

    public static final class VectorMergeProcess extends BaseProcess {
        private final SingleOutputImpl<SuperMapDatasetRef> output;

        VectorMergeProcess(JsonNode params) {
            super("vector.merge");
            addObjectInput("primary_dataset", SuperMapDatasetRef.class);
            addObjectInput("append_dataset", SuperMapDatasetRef.class);
            addObjectInput("output_datasource", SuperMapDatasourceRef.class);
            addStringInput("output_dataset_name", paramText(params, "output_dataset_name"));
            addBooleanInput("overwrite", optionalBoolean(params, "overwrite", false));
            this.output = addObjectOutput("result_dataset", SuperMapDatasetRef.class);
        }

        @Override
        public boolean execute() {
            SuperMapDatasetRef primary = (SuperMapDatasetRef) parameters.getInput("primary_dataset").getValue();
            SuperMapDatasetRef append = (SuperMapDatasetRef) parameters.getInput("append_dataset").getValue();
            SuperMapDatasourceRef outputDatasource = (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
            String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
            boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());

            ensureDatasetNameAvailable(outputDatasource.datasource, outputDatasetName, overwrite);
            Dataset copied = outputDatasource.datasource.copyDataset(primary.dataset, outputDatasetName, EncodeType.NONE);
            if (!(copied instanceof DatasetVector copiedVector)) {
                throw new IllegalStateException("copyDataset did not return DatasetVector: " + outputDatasetName);
            }
            Recordset recordset = append.dataset.getRecordset(false, CursorType.STATIC);
            try {
                if (recordset == null) {
                    throw new IllegalStateException("append dataset returned null recordset");
                }
                if (!copiedVector.append(recordset)) {
                    throw new IllegalStateException("DatasetVector.append returned false: " + outputDatasetName);
                }
                output.setValue(new SuperMapDatasetRef(outputDatasource, copiedVector));
            } finally {
                if (recordset != null) {
                    recordset.dispose();
                }
            }
            return true;
        }
    }

    public static final class VectorFeatureEnvelopeProcess extends BaseProcess {
        private final SingleOutputImpl<SuperMapDatasetRef> output;

        VectorFeatureEnvelopeProcess(JsonNode params) {
            super("vector.feature_envelope");
            addObjectInput("input_dataset", SuperMapDatasetRef.class);
            addObjectInput("output_datasource", SuperMapDatasourceRef.class);
            addStringInput("output_dataset_name", paramText(params, "output_dataset_name"));
            addBooleanInput("overwrite", optionalBoolean(params, "overwrite", false));
            this.output = addObjectOutput("result_dataset", SuperMapDatasetRef.class);
        }

        @Override
        public boolean execute() {
            SuperMapDatasetRef input = (SuperMapDatasetRef) parameters.getInput("input_dataset").getValue();
            SuperMapDatasourceRef outputDatasource = (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
            String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
            boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());

            ensureDatasetNameAvailable(outputDatasource.datasource, outputDatasetName, overwrite);
            DatasetVector result = Generalization.featureEnvelope(input.dataset, outputDatasetName, outputDatasource.datasource);
            if (result == null) {
                throw new IllegalStateException("Generalization.featureEnvelope returned null: " + outputDatasetName);
            }
            output.setValue(new SuperMapDatasetRef(outputDatasource, result));
            return true;
        }
    }

    public static final class VectorInnerPointProcess extends BaseProcess {
        private final SingleOutputImpl<SuperMapDatasetRef> output;

        VectorInnerPointProcess(JsonNode params) {
            super("vector.inner_point");
            addObjectInput("input_dataset", SuperMapDatasetRef.class);
            addObjectInput("output_datasource", SuperMapDatasourceRef.class);
            addStringInput("output_dataset_name", paramText(params, "output_dataset_name"));
            addBooleanInput("overwrite", optionalBoolean(params, "overwrite", false));
            this.output = addObjectOutput("result_dataset", SuperMapDatasetRef.class);
        }

        @Override
        public boolean execute() {
            SuperMapDatasetRef input = (SuperMapDatasetRef) parameters.getInput("input_dataset").getValue();
            SuperMapDatasourceRef outputDatasource = (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
            String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
            boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());

            ensureDatasetNameAvailable(outputDatasource.datasource, outputDatasetName, overwrite);
            DatasetVector result = outputDatasource.datasource.innerPointToDataset(input.dataset, outputDatasetName);
            if (result == null) {
                throw new IllegalStateException("Datasource.innerPointToDataset returned null: " + outputDatasetName);
            }
            output.setValue(new SuperMapDatasetRef(outputDatasource, result));
            return true;
        }
    }

    public static final class OverlayBinaryProcess extends BaseProcess {
        private final String operator;
        private final SingleOutputImpl<SuperMapDatasetRef> output;

        OverlayBinaryProcess(String operator, JsonNode params) {
            super(operator);
            this.operator = operator;
            addObjectInput("input_dataset", SuperMapDatasetRef.class);
            addObjectInput("overlay_dataset", SuperMapDatasetRef.class);
            addObjectInput("output_datasource", SuperMapDatasourceRef.class);
            addStringInput("output_dataset_name", paramText(params, "output_dataset_name"));
            addBooleanInput("overwrite", optionalBoolean(params, "overwrite", false));
            addDoubleInput("tolerance", optionalDouble(params, "tolerance", 0));
            this.output = addObjectOutput("result_dataset", SuperMapDatasetRef.class);
        }

        @Override
        public boolean execute() {
            SuperMapDatasetRef input = (SuperMapDatasetRef) parameters.getInput("input_dataset").getValue();
            SuperMapDatasetRef overlay = (SuperMapDatasetRef) parameters.getInput("overlay_dataset").getValue();
            SuperMapDatasourceRef outputDatasource = (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
            String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
            boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());
            double tolerance = (Double) parameters.getInput("tolerance").getValue();

            DatasetVector outputDataset = createOutputDataset(outputDatasource.datasource, outputDatasetName, input.dataset.getType(), overwrite, input.dataset);
            OverlayAnalystParameter parameter = new OverlayAnalystParameter();
            parameter.setTolerance(tolerance);
            parameter.setPreprocess(true);
            boolean ok = executeOverlay(operator, input.dataset, overlay.dataset, outputDataset, parameter);
            if (!ok) {
                throw new IllegalStateException("OverlayAnalyst." + overlayMethodName(operator) + " returned false");
            }
            output.setValue(new SuperMapDatasetRef(outputDatasource, outputDataset));
            return true;
        }
    }

    public static final class VectorQueryProcess extends BaseProcess {
        private final SingleOutputImpl<QueryResult> output;

        VectorQueryProcess(JsonNode params) {
            super("vector.query");
            addObjectInput("dataset", SuperMapDatasetRef.class);
            addStringInput("attribute_filter", optionalText(params, "attribute_filter", ""));
            this.output = addObjectOutput("query_result", QueryResult.class);
        }

        @Override
        public boolean execute() {
            SuperMapDatasetRef dataset = (SuperMapDatasetRef) parameters.getInput("dataset").getValue();
            String filter = String.valueOf(parameters.getInput("attribute_filter").getValue());
            if (filter == null || filter.isBlank()) {
                output.setValue(new QueryResult(dataset.dataset.getName(), "", dataset.dataset.getRecordCount()));
                return true;
            }
            QueryParameter queryParameter = new QueryParameter();
            queryParameter.setAttributeFilter(filter);
            queryParameter.setCursorType(CursorType.STATIC);
            Recordset recordset = dataset.dataset.query(queryParameter);
            try {
                output.setValue(new QueryResult(dataset.dataset.getName(), filter, recordset == null ? 0 : recordset.getRecordCount()));
            } finally {
                if (recordset != null) {
                    recordset.dispose();
                }
                queryParameter.dispose();
            }
            return true;
        }
    }

    public static final class SaveDatasetProcess extends BaseProcess {
        private final SingleOutputImpl<SuperMapDatasetRef> output;

        SaveDatasetProcess(JsonNode params) {
            super("dataset.save");
            addObjectInput("dataset", SuperMapDatasetRef.class);
            addObjectInput("target_datasource", SuperMapDatasourceRef.class);
            addStringInput("output_dataset_name", paramText(params, "output_dataset_name"));
            addBooleanInput("overwrite", optionalBoolean(params, "overwrite", false));
            this.output = addObjectOutput("saved_dataset", SuperMapDatasetRef.class);
        }

        @Override
        public boolean execute() {
            SuperMapDatasetRef dataset = (SuperMapDatasetRef) parameters.getInput("dataset").getValue();
            SuperMapDatasourceRef targetDatasource = (SuperMapDatasourceRef) parameters.getInput("target_datasource").getValue();
            String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
            boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());
            ensureDatasetNameAvailable(targetDatasource.datasource, outputDatasetName, overwrite);
            Dataset copied = targetDatasource.datasource.copyDataset(dataset.dataset, outputDatasetName, EncodeType.NONE);
            if (!(copied instanceof DatasetVector copiedVector)) {
                throw new IllegalStateException("copyDataset did not return DatasetVector: " + outputDatasetName);
            }
            output.setValue(new SuperMapDatasetRef(targetDatasource, copiedVector));
            return true;
        }
    }

    private static DatasetVector createOutputDataset(Datasource datasource, String name, DatasetType type, boolean overwrite) {
        return createOutputDataset(datasource, name, type, overwrite, null);
    }

    private static DatasetVector createOutputDataset(Datasource datasource, String name, DatasetType type, boolean overwrite, DatasetVector projectionSource) {
        ensureDatasetNameAvailable(datasource, name, overwrite);
        DatasetVectorInfo info = new DatasetVectorInfo(name, type);
        DatasetVector dataset = datasource.getDatasets().create(info);
        info.dispose();
        if (dataset == null) {
            throw new IllegalStateException("failed to create output dataset: " + name);
        }
        inheritProjection(dataset, projectionSource);
        return dataset;
    }

    private static void inheritProjection(DatasetVector target, DatasetVector source) {
        if (target == null || source == null) {
            return;
        }
        PrjCoordSys prjCoordSys = source.getPrjCoordSys();
        if (prjCoordSys != null) {
            target.setPrjCoordSys(prjCoordSys);
        }
    }

    private static void ensureDatasetNameAvailable(Datasource datasource, String name, boolean overwrite) {
        if (!datasource.getDatasets().contains(name)) {
            return;
        }
        if (!overwrite) {
            throw new IllegalArgumentException("dataset already exists: " + name);
        }
        boolean deleted = datasource.getDatasets().delete(name);
        if (!deleted) {
            throw new IllegalStateException("failed to delete existing dataset: " + name);
        }
    }

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
                boolean readOnly
        ) {
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
                throw new IllegalStateException("failed to open PostGIS datasource: " + server + "/" + database);
            }
            if (readOnly && table != null && !table.isBlank() && !datasource.getDatasets().contains(table)) {
                throw new IllegalArgumentException("PostGIS dataset not found: " + schema + "." + table);
            }
            return new SuperMapDatasourceRef("postgis://" + server + "/" + database, datasource);
        }

        SuperMapSpatialWorkspaceRef enablePostgisWorkspace(
                String server,
                String database,
                String user,
                String password,
                String alias
        ) {
            Workspace workspace = new Workspace();
            workspaces.add(workspace);
            DatasourceConnectionInfo info = postgisConnectionInfo(server, database, user, password, alias, false);
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
                throw new IllegalStateException("failed to enable PostGIS datasource: " + server + "/" + database);
            }
            return new SuperMapSpatialWorkspaceRef("postgis://" + server + "/" + database, datasource, !created);
        }

        private DatasourceConnectionInfo connectionInfo(String path, String alias, boolean readOnly) {
            DatasourceConnectionInfo info = new DatasourceConnectionInfo();
            info.setEngineType(EngineType.UDBX);
            info.setServer(path);
            info.setAlias(alias == null || alias.isBlank() ? Path.of(path).getFileName().toString() : alias);
            info.setReadOnly(readOnly);
            return info;
        }

        private DatasourceConnectionInfo postgisConnectionInfo(
                String server,
                String database,
                String user,
                String password,
                String alias,
                boolean readOnly
        ) {
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

        DatasetInfoSummary(String name, String datasetType, int recordCount, int fieldCount, Rectangle2D bounds, PrjCoordSys prjCoordSys, FieldInfos fieldInfos) {
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
                    dataset.getFieldInfos()
            );
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

    private record UdbxSchemaState(List<String> missingTables, List<String> missingRegisterColumns) {
        boolean current() {
            return missingTables.isEmpty() && missingRegisterColumns.isEmpty();
        }
    }

    private static final class ExecutionRecord {
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

    private static final class DependencyCheck {
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
