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
import static com.addp.supermap.workflow.SuperMapWorkflowRuntime.*;

final class SuperMapRuntimeSupport {
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

    private SuperMapRuntimeSupport() {
    }

        static String paramText(JsonNode params, String field) {
            String value = params.path(field).asText(null);
            if (value == null || value.isBlank()) {
                throw new IllegalArgumentException("params." + field + " is required");
            }
            return value;
        }

        static String optionalText(JsonNode params, String field, String defaultValue) {
            String value = params.path(field).asText(null);
            return value == null || value.isBlank() ? defaultValue : value;
        }

        static JsonNode requireObject(JsonNode params, String field) {
            JsonNode value = params.path(field);
            if (!value.isObject()) {
                throw new IllegalArgumentException("params." + field + " must be an object");
            }
            return value;
        }

        static String requireConnText(JsonNode connInfo, String field) {
            String value = optionalConnText(connInfo, field, "");
            if (value.isBlank()) {
                throw new IllegalArgumentException("connection_info." + field + " is required");
            }
            return value;
        }

        static String optionalConnText(JsonNode connInfo, String field, String defaultValue) {
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

        static UdbxSchemaState inspectUdbxSchema(Path path) {
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

        static Path resolveUdbxPath(JsonNode connInfo, String outputPath) {
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

        static Path dynamicNfsMountRoot(String server, String exportPath) {
            String baseDir = System.getenv().getOrDefault("SUPERMAP_DYNAMIC_NFS_MOUNT_BASE", "/mnt/addp-dynamic-nfs");
            return Path.of(baseDir).resolve(sha256Hex(server + "|" + exportPath).substring(0, 16));
        }

        static void ensureNfsMounted(String server, String exportPath, String nfsVersion, Path mountRoot) {
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

        static String postgisServer(JsonNode connInfo) {
            String host = normalizeResourceHost(requireConnText(connInfo, "host"));
            String port = optionalConnText(connInfo, "port", "");
            if (port.isBlank()) {
                return host;
            }
            return host + ":" + port;
        }

        static String normalizeResourceHost(String host) {
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

        static String defaultPostgisAlias(JsonNode params) {
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

        static boolean optionalBoolean(JsonNode params, String field, boolean defaultValue) {
            return params.has(field) && !params.path(field).isNull() ? params.path(field).asBoolean(defaultValue) : defaultValue;
        }

        static double optionalDouble(JsonNode params, String field, double defaultValue) {
            return params.has(field) && !params.path(field).isNull() ? params.path(field).asDouble(defaultValue) : defaultValue;
        }

        static double requiredDouble(JsonNode params, String field) {
            if (!params.has(field) || params.path(field).isNull()) {
                throw new IllegalArgumentException("params." + field + " is required");
            }
            return params.path(field).asDouble();
        }

        static int requiredInt(JsonNode params, String field) {
            if (!params.has(field) || params.path(field).isNull()) {
                throw new IllegalArgumentException("params." + field + " is required");
            }
            return params.path(field).asInt();
        }

        static int optionalInt(JsonNode params, String field, int defaultValue) {
            return params.has(field) && !params.path(field).isNull() ? params.path(field).asInt(defaultValue) : defaultValue;
        }

        static String[] stringArrayParam(JsonNode params, String field) {
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

        static BufferRadiusUnit bufferRadiusUnit(String value) {
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

        static BufferEndType bufferEndType(String value) {
            return switch ((value == null ? "" : value.trim().toLowerCase())) {
                case "", "round" -> BufferEndType.ROUND;
                case "flat" -> BufferEndType.FLAT;
                default -> throw new IllegalArgumentException("unsupported buffer end_type: " + value);
            };
        }

        static CoordSysTransMethod coordSysTransMethod(String value) {
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

        static SpatialRelationType spatialRelationType(String value) {
            return switch ((value == null ? "" : value.trim().toLowerCase()).replace("-", "_")) {
                case "intersect", "intersects" -> SpatialRelationType.INTERSECT;
                case "contain", "contains" -> SpatialRelationType.CONTAIN;
                case "within" -> SpatialRelationType.WITHIN;
                case "closest" -> SpatialRelationType.CLOSEST;
                default -> throw new IllegalArgumentException("unsupported spatial relation: " + value);
            };
        }

        static DissolveType dissolveType(String value) {
            return switch ((value == null ? "" : value.trim().toLowerCase()).replace("-", "_")) {
                case "", "multipart", "multi_part" -> DissolveType.MULTIPART;
                case "single" -> DissolveType.SINGLE;
                case "only_multipart", "only_multi_part" -> DissolveType.ONLYMULTIPART;
                default -> throw new IllegalArgumentException("unsupported dissolve_type: " + value);
            };
        }

        static boolean executeOverlay(String operator, DatasetVector input, DatasetVector overlay, DatasetVector output, OverlayAnalystParameter parameter) {
            return switch (operator) {
                case "overlay.intersect" -> OverlayAnalyst.intersect(input, overlay, output, parameter);
                case "overlay.clip" -> OverlayAnalyst.clip(input, overlay, output, parameter);
                case "overlay.erase" -> OverlayAnalyst.erase(input, overlay, output, parameter);
                case "overlay.union" -> OverlayAnalyst.union(input, overlay, output, parameter);
                default -> throw new IllegalArgumentException("unsupported overlay operator: " + operator);
            };
        }

        static String overlayMethodName(String operator) {
            return switch (operator) {
                case "overlay.intersect" -> "intersect";
                case "overlay.clip" -> "clip";
                case "overlay.erase" -> "erase";
                case "overlay.union" -> "union";
                default -> operator;
            };
        }

        static String readAll(InputStream input) throws IOException {
            return new String(input.readAllBytes(), StandardCharsets.UTF_8);
        }

        static DatasetVector createOutputDataset(Datasource datasource, String name, DatasetType type, boolean overwrite) {
            return createOutputDataset(datasource, name, type, overwrite, null);
        }

        static DatasetVector createOutputDataset(Datasource datasource, String name, DatasetType type, boolean overwrite, DatasetVector projectionSource) {
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

        static void inheritProjection(DatasetVector target, DatasetVector source) {
            if (target == null || source == null) {
                return;
            }
            PrjCoordSys prjCoordSys = source.getPrjCoordSys();
            if (prjCoordSys != null) {
                target.setPrjCoordSys(prjCoordSys);
            }
        }

        static void ensureDatasetNameAvailable(Datasource datasource, String name, boolean overwrite) {
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

    }


}

