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

import static com.addp.supermap.workflow.SuperMapRuntimeSupport.*;
import static com.addp.supermap.workflow.SuperMapWorkflowRuntime.*;

final class SuperMapAccessService {
        static void publishDirectory(Path renderRoot, JsonNode access) {
            String method = access.path("method").asText();
            if ("mounted_path".equals(method)) {
                Path targetRoot = resolveWorkflowAccessPath(access);
                if (Files.exists(targetRoot)) {
                    deleteRecursively(targetRoot);
                }
                try (Stream<Path> paths = Files.walk(renderRoot)) {
                    for (Path source : paths.toList()) {
                        Path target = targetRoot.resolve(renderRoot.relativize(source).toString());
                        if (Files.isDirectory(source)) {
                            Files.createDirectories(target);
                        } else {
                            Files.createDirectories(target.getParent());
                            Files.copy(source, target, StandardCopyOption.REPLACE_EXISTING);
                        }
                    }
                } catch (IOException ex) {
                    throw new IllegalStateException("failed to publish directory to mounted path", ex);
                }
                return;
            }
            if (!"object_store".equals(method)) {
                throw new IllegalArgumentException("directory target access method must be mounted_path or object_store");
            }
            String prefix = access.path("prefix").asText("").replace('\\', '/').replaceAll("^/+|/+$", "");
            try {
                MinioClient client = buildObjectStoreClient(access);
                try (Stream<Path> paths = Files.walk(renderRoot)) {
                    for (Path file : paths.filter(Files::isRegularFile).toList()) {
                        String relative = renderRoot.relativize(file).toString().replace('\\', '/');
                        String object = prefix.isBlank() ? relative : prefix + "/" + relative;
                        client.uploadObject(UploadObjectArgs.builder()
                                .bucket(paramText(access, "bucket"))
                                .object(object)
                                .filename(file.toString())
                                .build());
                    }
                }
            } catch (Exception ex) {
                throw new IllegalStateException("failed to publish directory to object store", ex);
            }
        }

        static WorkflowAccessFile resolveWorkflowAccessFile(JsonNode access) {
            String method = access.path("method").asText();
            if ("mounted_path".equals(method)) {
                return new WorkflowAccessFile(resolveWorkflowAccessPath(access), null);
            }
            if (!"object_store".equals(method)) {
                throw new IllegalArgumentException("CAD source access method must be mounted_path or object_store");
            }
            String bucket = paramText(access, "bucket");
            String object = paramText(access, "object");
            String fileName = Path.of(object.replace('\\', '/')).getFileName().toString();
            if (fileName.isBlank()) {
                throw new IllegalArgumentException("object_store CAD source requires a file object");
            }
            Path tempRoot;
            try {
                tempRoot = Files.createTempDirectory("addp-supermap-cad-");
            } catch (IOException ex) {
                throw new IllegalStateException("failed to create CAD materialization directory", ex);
            }
            Path localFile = tempRoot.resolve(fileName);
            try {
                MinioClient client = buildObjectStoreClient(access);
                client.downloadObject(DownloadObjectArgs.builder()
                        .bucket(bucket)
                        .object(object)
                        .filename(localFile.toString())
                        .build());
                return new WorkflowAccessFile(localFile, tempRoot);
            } catch (Exception ex) {
                deleteRecursively(tempRoot);
                throw new IllegalStateException("failed to materialize CAD object " + bucket + "/" + object, ex);
            }
        }

        record WorkflowAccessFile(Path path, Path tempRoot) implements AutoCloseable {
            @Override
            public void close() {
                if (tempRoot != null) {
                    deleteRecursively(tempRoot);
                }
            }
        }

        private static MinioClient buildObjectStoreClient(JsonNode access) {
            String endpoint = paramText(access, "endpoint");
            boolean useSSL = access.path("use_ssl").asBoolean(false);
            String candidate = endpoint.contains("://") ? endpoint : (useSSL ? "https://" : "http://") + endpoint;
            URI uri;
            try {
                uri = URI.create(candidate);
            } catch (IllegalArgumentException ex) {
                throw new IllegalArgumentException("invalid object_store endpoint", ex);
            }
            String host = normalizeResourceHost(uri.getHost());
            if (host == null || host.isBlank()) {
                throw new IllegalArgumentException("invalid object_store endpoint host");
            }
            String scheme = uri.getScheme();
            if (!"http".equalsIgnoreCase(scheme) && !"https".equalsIgnoreCase(scheme)) {
                throw new IllegalArgumentException("object_store endpoint scheme must be http or https");
            }
            String hostForURL = host.contains(":") && !host.startsWith("[") ? "[" + host + "]" : host;
            String normalizedEndpoint = scheme.toLowerCase() + "://" + hostForURL + (uri.getPort() > 0 ? ":" + uri.getPort() : "");
            return MinioClient.builder()
                    .endpoint(normalizedEndpoint)
                    .credentials(paramText(access, "access_key"), paramText(access, "secret_key"))
                    .build();
        }

        static Path resolveWorkflowAccessPath(JsonNode access) {
            if (!"mounted_path".equals(access.path("method").asText())) {
                throw new IllegalArgumentException("SuperMap S3M conversion currently requires mounted_path access");
            }
            Path configuredPath = Path.of(paramText(access, "path")).normalize();
            String server = access.path("server").asText("").trim();
            String exportPath = access.path("export_path").asText("").trim();
            if (server.isBlank() || exportPath.isBlank()) {
                return configuredPath;
            }
            Path exportRoot = Path.of(exportPath).normalize();
            if (!configuredPath.startsWith(exportRoot)) {
                throw new IllegalArgumentException("mounted_path is outside the declared NFS export: " + configuredPath);
            }
            Path mountRoot = dynamicNfsMountRoot(normalizeResourceHost(server), exportPath);
            ensureNfsMounted(
                    normalizeResourceHost(server),
                    exportPath,
                    access.path("nfs_version").asText(""),
                    mountRoot
            );
            return mountRoot.resolve(exportRoot.relativize(configuredPath)).normalize();
        }

        static void deleteRecursively(Path root) {
            if (root == null || !Files.exists(root)) {
                return;
            }
            try (Stream<Path> paths = Files.walk(root)) {
                for (Path path : paths.sorted(Comparator.reverseOrder()).toList()) {
                    Files.deleteIfExists(path);
                }
            } catch (IOException ex) {
                throw new IllegalStateException("failed to remove path: " + root, ex);
            }
        }

}
