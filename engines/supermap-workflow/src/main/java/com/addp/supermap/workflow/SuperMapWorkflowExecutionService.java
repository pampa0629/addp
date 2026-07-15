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


import static com.addp.supermap.workflow.SuperMapModels.*;
import static com.addp.supermap.workflow.SuperMapHttpServer.*;
import static com.addp.supermap.workflow.SuperMapOperatorRegistry.*;
import static com.addp.supermap.workflow.SuperMapWorkflowRuntime.*;

final class SuperMapWorkflowExecutionService {
        static ObjectNode executeWorkflow(String executionID, JsonNode request, double elapsedBeforeRunMs) {
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
            if (value instanceof ObjectNode objectNode) {
                return objectNode.deepCopy();
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
        private static String assetRefFor(JsonNode task) {
            JsonNode params = task.path("params");
            String path = params.path("path").asText("");
            if (!path.isBlank()) {
                return path;
            }
            String outputPath = params.path("output_path").asText("");
    		if (outputPath != null && !outputPath.isBlank()) {
    			return outputPath;
    		}
    		return params.path("access_plan").path("target").path("access").path("path").asText("");
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
}
