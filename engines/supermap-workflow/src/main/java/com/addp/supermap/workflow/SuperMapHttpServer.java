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
import com.supermap.data.GeoStyle;
import com.supermap.data.Point2D;
import com.supermap.data.Point2Ds;
import com.supermap.data.Point3D;
import com.supermap.data.PrjCoordSys;
import com.supermap.data.QueryParameter;
import com.supermap.data.Rectangle2D;
import com.supermap.data.Recordset;
import com.supermap.data.S3MVersion;
import com.supermap.data.SpatialRelationType;
import com.supermap.data.Workspace;
import com.supermap.data.processing.ObliquePhotogrammetryBuilder;
import com.supermap.data.processing.ObliqueProcessType;
import com.supermap.data.processing.OSGBCacheBuilder;
import com.supermap.data.processing.TextureCompressType;
import com.supermap.data.processing.VertexOptimizationType;
import com.supermap.realspace.CacheFileType;
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
import io.minio.DownloadObjectArgs;
import io.minio.MinioClient;
import io.minio.UploadObjectArgs;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.awt.Dimension;
import java.awt.Color;
import java.net.InetSocketAddress;
import java.net.URI;
import java.net.URLDecoder;
import java.nio.file.InvalidPathException;
import java.nio.charset.StandardCharsets;
import java.nio.file.DirectoryStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
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
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.Comparator;
import java.util.stream.Stream;
import javax.xml.parsers.DocumentBuilderFactory;
import org.w3c.dom.Document;
import org.w3c.dom.Element;
import org.w3c.dom.NodeList;

import static com.addp.supermap.workflow.SuperMapModels.*;
import static com.addp.supermap.workflow.SuperMapProcesses.*;
import static com.addp.supermap.workflow.SuperMapOperatorRegistry.*;
import static com.addp.supermap.workflow.SuperMapWorkflowExecutionService.*;

public final class SuperMapWorkflowRuntime {

import static com.addp.supermap.workflow.SuperMapModels.*;
import static com.addp.supermap.workflow.SuperMapOperatorRegistry.*;
import static com.addp.supermap.workflow.SuperMapRuntimeSupport.*;
import static com.addp.supermap.workflow.SuperMapWorkflowExecutionService.*;
import static com.addp.supermap.workflow.SuperMapWorkflowRuntime.*;

final class SuperMapHttpServer {
    private static final String SERVICE_NAME = "supermap-workflow-engine";
    private static final String VERSION = "0.3.0";
    private static final Instant STARTED_AT = Instant.now();
    private static final Map<String, ExecutionRecord> EXECUTIONS = new ConcurrentHashMap<>();
    private static final Object SUPERMAP_EXECUTION_LOCK = new Object();

    private SuperMapHttpServer() {
    }

    static void start() throws Exception {
            int port = Integer.parseInt(System.getenv().getOrDefault("PORT", "8103"));
            HttpServer server = HttpServer.create(new InetSocketAddress("0.0.0.0", port), 0);
            server.createContext("/health", SuperMapWorkflowRuntime::handleHealth);
            server.createContext("/api/operators", SuperMapWorkflowRuntime::handleOperators);
            server.createContext("/api/workflow", SuperMapWorkflowRuntime::handleWorkflow);
            server.createContext("/api/operators/", SuperMapWorkflowRuntime::handleDirectOperator);
            server.createContext("/api/executions/", SuperMapWorkflowRuntime::handleExecutionStatus);
            int httpThreads = Math.max(2, Integer.parseInt(System.getenv().getOrDefault("SUPERMAP_HTTP_THREADS", "4")));
            server.setExecutor(Executors.newFixedThreadPool(httpThreads));
            server.start();
            System.out.printf("%s listening on http://0.0.0.0:%d%n", SERVICE_NAME, port);
        }
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
                ObjectNode response;
                synchronized (SUPERMAP_EXECUTION_LOCK) {
                    response = executeWorkflow(executionID, request, elapsedMs(started));
                }
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
                ObjectNode response;
                synchronized (SUPERMAP_EXECUTION_LOCK) {
                    response = invokeDirectOperator(name, params);
                }
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

        private static void sendJson(HttpExchange exchange, int statusCode, JsonNode body) throws IOException {
            byte[] bytes = MAPPER.writeValueAsBytes(body);
            exchange.getResponseHeaders().set("Content-Type", "application/json; charset=utf-8");
            exchange.sendResponseHeaders(statusCode, bytes.length);
            try (OutputStream output = exchange.getResponseBody()) {
                output.write(bytes);
            }
        }

        static double elapsedMs(long startedNanos) {
            return (System.nanoTime() - startedNanos) / 1_000_000.0;
        }

}

