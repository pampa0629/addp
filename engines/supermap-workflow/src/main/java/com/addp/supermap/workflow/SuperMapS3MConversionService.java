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

import static com.addp.supermap.workflow.SuperMapAccessService.*;
import static com.addp.supermap.workflow.SuperMapRuntimeSupport.*;
import static com.addp.supermap.workflow.SuperMapWorkflowRuntime.*;

final class SuperMapS3MConversionService {
        static ObjectNode convertOSGBSceneToS3M(JsonNode params) {
            JsonNode plan = requireObject(params, "access_plan");
            if (!"addp.workflow.access-plan/v1".equals(plan.path("schema_version").asText())) {
                throw new IllegalArgumentException("unsupported access_plan.schema_version");
            }
            JsonNode source = requireObject(plan, "source");
            JsonNode target = requireObject(plan, "target");
            if (!"directory".equals(source.path("kind").asText()) || !"osgb_scene".equals(source.path("format").asText())) {
                throw new IllegalArgumentException("access_plan.source must be directory/osgb_scene");
            }
            if (!"directory".equals(target.path("kind").asText()) || !"s3m".equals(target.path("format").asText())) {
                throw new IllegalArgumentException("access_plan.target must be directory/s3m");
            }

            Path sourceRoot = resolveWorkflowAccessPath(requireObject(source, "access"));
            JsonNode targetAccess = requireObject(target, "access");
            boolean objectStoreTarget = "object_store".equals(targetAccess.path("method").asText());
            Path targetRoot;
            try {
                targetRoot = objectStoreTarget
                        ? Files.createTempDirectory("addp-supermap-s3m-output-")
                        : resolveWorkflowAccessPath(targetAccess);
            } catch (IOException ex) {
                throw new IllegalStateException("failed to prepare S3M target directory", ex);
            }
            try {
                if (!Files.isDirectory(sourceRoot)) {
                    throw new IllegalArgumentException("OSGB scene directory does not exist: " + sourceRoot);
                }
                String writeMode = target.path("write_mode").asText("create");
                if (Files.exists(targetRoot)) {
                    if (!objectStoreTarget && "create".equals(writeMode)) {
                        throw new IllegalArgumentException("S3M target already exists: " + targetRoot);
                    }
                    if (!"replace".equals(writeMode)) {
                        throw new IllegalArgumentException("target write_mode must be create or replace");
                    }
                    deleteRecursively(targetRoot);
                }

                OSGBSceneMetadata metadata = readOSGBSceneMetadata(sourceRoot.resolve("metadata.xml"));
                List<String> rootTiles = findOSGBRootTiles(sourceRoot.resolve("Data"));
                if (rootTiles.isEmpty()) {
                    throw new IllegalArgumentException("OSGB scene does not contain Data/Tile_*/Tile_*.osgb roots");
                }

                Path workRoot;
                Path stagedSceneRoot;
                Path stagedDataRoot;
                try {
                    workRoot = Files.createTempDirectory("addp-supermap-s3m-");
                    stagedSceneRoot = workRoot.resolve("scene");
                    stagedDataRoot = stagedSceneRoot.resolve("Data");
                    stageOSGBSceneData(sourceRoot.resolve("Data"), stagedDataRoot);
                    Files.createDirectories(targetRoot.resolve("config"));
                } catch (IOException ex) {
                    throw new IllegalStateException("failed to prepare S3M conversion directories", ex);
                }
                List<String> stagedRootTiles = findOSGBRootTiles(stagedDataRoot);
                if (stagedRootTiles.size() != rootTiles.size()) {
                    deleteRecursively(workRoot);
                    throw new IllegalStateException("failed to stage all OSGB root tiles");
                }
                Path sourceSCP = workRoot.resolve("scene.scp");
                PrjCoordSys prjCoordSys = PrjCoordSys.fromEPSG(metadata.epsg());
                PrjCoordSys targetPrjCoordSys = PrjCoordSys.fromEPSG(4326);
                if (prjCoordSys == null || targetPrjCoordSys == null) {
                    if (prjCoordSys != null) {
                        prjCoordSys.dispose();
                    }
                    if (targetPrjCoordSys != null) {
                        targetPrjCoordSys.dispose();
                    }
                    deleteRecursively(workRoot);
                    throw new IllegalArgumentException("unsupported OSGB scene source or target CRS");
                }
                CoordSysTransParameter targetCoordSysTransParameter = new CoordSysTransParameter();
                ObliquePhotogrammetryBuilder builder = null;
                int generatedRootTileCount = 0;
                boolean conversionSucceeded = false;
                try {
                    boolean configGenerated = OSGBCacheBuilder.generateConfigFile(
                            sourceSCP.toString(),
                            new Point3D(metadata.originX(), metadata.originY(), metadata.originZ()),
                            prjCoordSys,
                            stagedRootTiles.toArray(new String[0])
                    );
                    if (!configGenerated) {
                        throw new IllegalStateException("SuperMap failed to generate OSGB scene SCP");
                    }
                    builder = new ObliquePhotogrammetryBuilder(new ObliqueProcessType[]{ObliqueProcessType.COMPRESS_TEXTURE});
                    builder.setTexCompressType(TextureCompressType.TEXTURECOMPRESS_DXT);
                    builder.setVertexOptimazationType(VertexOptimizationType.VO_DRACO);
                    builder.setS3MVersion(S3MVersion.VERSION_301);
                    builder.setFileType(CacheFileType.S3MB);
                    boolean converted = builder.build(sourceSCP.toString(), targetRoot.resolve("config").toString(), 1);
                    if (!converted) {
                        throw new IllegalStateException("SuperMap OSGB to S3M conversion returned false");
                    }
                    Path generatedManifest = findSingleSCP(targetRoot);
                    Path manifest = generatedManifest.resolveSibling("scene.scp");
                    if (!generatedManifest.equals(manifest)) {
                        Files.move(generatedManifest, manifest, StandardCopyOption.REPLACE_EXISTING);
                    }
                    normalizeS3MManifestGeoreference(
                            manifest,
                            prjCoordSys,
                            targetPrjCoordSys,
                            targetCoordSysTransParameter
                    );
                    generatedRootTileCount = validateS3MOutput(targetRoot, manifest, rootTiles.size());
                    conversionSucceeded = true;
                } catch (IOException ex) {
                    throw new IllegalStateException("failed to finalize S3M manifest", ex);
                } finally {
                    if (builder != null) {
                        builder.dispose();
                    }
                    targetCoordSysTransParameter.dispose();
                    prjCoordSys.dispose();
                    targetPrjCoordSys.dispose();
                    deleteRecursively(workRoot);
                    if (!conversionSucceeded) {
                        deleteRecursively(targetRoot);
                    }
                }

                Path manifest = findSingleSCP(targetRoot);
                long fileCount;
                long sizeBytes;
                try (Stream<Path> files = Files.walk(targetRoot)) {
                    List<Path> outputs = files.filter(Files::isRegularFile).toList();
                    fileCount = outputs.size();
                    sizeBytes = outputs.stream().mapToLong(item -> {
                        try {
                            return Files.size(item);
                        } catch (IOException ex) {
                            throw new IllegalStateException(ex);
                        }
                    }).sum();
                } catch (IOException ex) {
                    throw new IllegalStateException("failed to inspect S3M output", ex);
                }
                String manifestRef = targetRoot.relativize(manifest).toString().replace('\\', '/');
                if (objectStoreTarget) {
                    publishDirectory(targetRoot, targetAccess);
                }
                ObjectNode result = MAPPER.createObjectNode();
                result.put("kind", "supermap_s3m_dataset");
                result.put("target_format", "s3m");
                result.put("target_path", objectStoreTarget ? targetAccess.path("prefix").asText("") : targetRoot.toString());
                result.put("manifest_ref", manifestRef);
                result.put("texture_compression", "dxt");
                result.put("geometry_compression", "draco");
                result.put("s3m_version", "3.01");
                result.put("crs", "EPSG:4326");
                result.put("manifest_encoding", "json");
                result.put("tile_extension", ".s3mb");
                result.put("root_tile_count", generatedRootTileCount);
                result.put("source_root_candidate_count", rootTiles.size());
                result.put("file_count", fileCount);
                result.put("size_bytes", sizeBytes);
                return result;
            } finally {
                if (objectStoreTarget) {
                    deleteRecursively(targetRoot);
                }
            }
        }

        private static OSGBSceneMetadata readOSGBSceneMetadata(Path metadataPath) {
            if (!Files.isRegularFile(metadataPath)) {
                throw new IllegalArgumentException("OSGB scene metadata.xml does not exist: " + metadataPath);
            }
            try {
                DocumentBuilderFactory factory = DocumentBuilderFactory.newInstance();
                factory.setFeature("http://apache.org/xml/features/disallow-doctype-decl", true);
                factory.setExpandEntityReferences(false);
                Document document = factory.newDocumentBuilder().parse(metadataPath.toFile());
                String srs = document.getElementsByTagName("SRS").item(0).getTextContent().trim();
                String originText = document.getElementsByTagName("SRSOrigin").item(0).getTextContent().trim();
                if (!srs.toUpperCase().startsWith("EPSG:")) {
                    throw new IllegalArgumentException("OSGB scene SRS must use EPSG:<code>: " + srs);
                }
                int epsg = Integer.parseInt(srs.substring(srs.indexOf(':') + 1).trim());
                String[] origin = originText.split(",");
                if (origin.length != 3) {
                    throw new IllegalArgumentException("OSGB scene SRSOrigin must contain x,y,z");
                }
                return new OSGBSceneMetadata(
                        epsg,
                        Double.parseDouble(origin[0].trim()),
                        Double.parseDouble(origin[1].trim()),
                        Double.parseDouble(origin[2].trim())
                );
            } catch (IllegalArgumentException ex) {
                throw ex;
            } catch (Exception ex) {
                throw new IllegalArgumentException("failed to parse OSGB scene metadata.xml", ex);
            }
        }

        private static List<String> findOSGBRootTiles(Path dataRoot) {
            if (!Files.isDirectory(dataRoot)) {
                return List.of();
            }
            try (Stream<Path> entries = Files.list(dataRoot)) {
                return entries
                        .filter(Files::isDirectory)
                        .map(dir -> dir.resolve(dir.getFileName().toString() + ".osgb"))
                        .filter(Files::isRegularFile)
                        .sorted(Comparator.comparing(Path::toString))
                        .map(Path::toString)
                        .toList();
            } catch (IOException ex) {
                throw new IllegalStateException("failed to list OSGB root tiles", ex);
            }
        }

        private static Path findSingleSCP(Path root) {
            try (Stream<Path> files = Files.walk(root)) {
                List<Path> manifests = files
                        .filter(Files::isRegularFile)
                        .filter(item -> item.getFileName().toString().toLowerCase().endsWith(".scp"))
                        .toList();
                if (manifests.size() != 1) {
                    throw new IllegalStateException("S3M output must contain exactly one SCP manifest, got " + manifests.size());
                }
                return manifests.get(0);
            } catch (IOException ex) {
                throw new IllegalStateException("failed to inspect S3M manifest", ex);
            }
        }

        private static void stageOSGBSceneData(Path sourceDataRoot, Path stagedDataRoot) throws IOException {
            if (!Files.isDirectory(sourceDataRoot)) {
                throw new IllegalArgumentException("OSGB scene Data directory does not exist: " + sourceDataRoot);
            }
            try (Stream<Path> paths = Files.walk(sourceDataRoot)) {
                for (Path source : paths.toList()) {
                    Path staged = stagedDataRoot.resolve(sourceDataRoot.relativize(source).toString());
                    if (Files.isDirectory(source)) {
                        Files.createDirectories(staged);
                    } else if (Files.isRegularFile(source)) {
                        Files.createDirectories(staged.getParent());
                        Files.copy(source, staged, StandardCopyOption.REPLACE_EXISTING, StandardCopyOption.COPY_ATTRIBUTES);
                    }
                }
            }
        }

        private static int validateS3MOutput(Path targetRoot, Path manifest, int sourceRootCandidates) {
            try {
                JsonNode config = MAPPER.readTree(manifest.toFile());
                if (!"3.01".equals(config.path("version").asText())) {
                    throw new IllegalStateException("S3M manifest version must be 3.01");
                }
                if (!"epsg:4326".equalsIgnoreCase(config.path("crs").asText())) {
                    throw new IllegalStateException("S3M manifest CRS must be EPSG:4326");
                }
                JsonNode position = config.path("position");
                if (!"degree".equalsIgnoreCase(position.path("unit").asText())) {
                    throw new IllegalStateException("S3M manifest position unit must be Degree");
                }
                JsonNode point = position.path("point3D");
                double longitude = point.path("x").asDouble(Double.NaN);
                double latitude = point.path("y").asDouble(Double.NaN);
                if (!Double.isFinite(longitude) || longitude < -180 || longitude > 180
                        || !Double.isFinite(latitude) || latitude < -90 || latitude > 90) {
                    throw new IllegalStateException("S3M manifest position must contain WGS84 longitude and latitude");
                }
                JsonNode extensions = config.path("extensions");
                if (!"DXT".equalsIgnoreCase(extensions.path("s3m:TextureCompressionType").asText())) {
                    throw new IllegalStateException("S3M manifest texture compression must be DXT");
                }
                if (!"DRACO".equalsIgnoreCase(extensions.path("s3m:VertexCompressionType").asText())) {
                    throw new IllegalStateException("S3M manifest geometry compression must be DRACO");
                }
                JsonNode rootTiles = config.path("rootTiles");
                if (!rootTiles.isArray()) {
                    throw new IllegalStateException("S3M manifest rootTiles must be an array");
                }
                int referencedTiles = 0;
                for (JsonNode rootTile : rootTiles) {
                    String ref = rootTile.path("url").asText().trim();
                    if (!ref.toLowerCase().endsWith(".s3mb")) {
                        throw new IllegalStateException("S3M 3.01 root tile must use .s3mb: " + ref);
                    }
                    Path tile = manifest.getParent().resolve(ref).normalize();
                    if (!tile.startsWith(targetRoot.normalize()) || !Files.isRegularFile(tile)) {
                        throw new IllegalStateException("S3M manifest references a missing tile: " + ref);
                    }
                    referencedTiles++;
                }
                if (referencedTiles == 0 || referencedTiles > sourceRootCandidates) {
                    throw new IllegalStateException("S3M output has invalid root tile count: referenced=" + referencedTiles + ", source candidates=" + sourceRootCandidates);
                }
                return referencedTiles;
            } catch (IllegalStateException ex) {
                throw ex;
            } catch (Exception ex) {
                throw new IllegalStateException("failed to validate S3M output", ex);
            }
        }

        private static void normalizeS3MManifestGeoreference(
                Path manifest,
                PrjCoordSys sourcePrjCoordSys,
                PrjCoordSys targetPrjCoordSys,
                CoordSysTransParameter transParameter
        ) throws IOException {
            JsonNode parsed = MAPPER.readTree(manifest.toFile());
            if (!(parsed instanceof ObjectNode config)
                    || !(config.path("position") instanceof ObjectNode position)
                    || !(position.path("point3D") instanceof ObjectNode point)) {
                throw new IllegalStateException("S3M manifest position is missing");
            }

            List<Point2D> sourcePoints = new ArrayList<>();
            sourcePoints.add(new Point2D(point.path("x").asDouble(Double.NaN), point.path("y").asDouble(Double.NaN)));
            ObjectNode geoBounds = config.path("geoBounds") instanceof ObjectNode bounds ? bounds : null;
            if (geoBounds != null) {
                double left = geoBounds.path("left").asDouble(Double.NaN);
                double bottom = geoBounds.path("bottom").asDouble(Double.NaN);
                double right = geoBounds.path("right").asDouble(Double.NaN);
                double top = geoBounds.path("top").asDouble(Double.NaN);
                sourcePoints.add(new Point2D(left, bottom));
                sourcePoints.add(new Point2D(left, top));
                sourcePoints.add(new Point2D(right, bottom));
                sourcePoints.add(new Point2D(right, top));
            }
            if (sourcePoints.stream().anyMatch(value -> !Double.isFinite(value.getX()) || !Double.isFinite(value.getY()))) {
                throw new IllegalStateException("S3M manifest projected position or bounds are invalid");
            }

            Point2Ds transformed = new Point2Ds(sourcePoints.toArray(new Point2D[0]));
            if (!CoordSysTranslator.convert(
                    transformed,
                    sourcePrjCoordSys,
                    targetPrjCoordSys,
                    transParameter,
                    CoordSysTransMethod.MTH_Prj4
            )) {
                throw new IllegalStateException("failed to transform S3M manifest georeference to EPSG:4326");
            }

            Point2D origin = transformed.getItem(0);
            point.put("x", origin.getX());
            point.put("y", origin.getY());
            position.put("unit", "Degree");
            position.remove("units");
            config.put("crs", "epsg:4326");

            if (geoBounds != null) {
                double left = Double.POSITIVE_INFINITY;
                double bottom = Double.POSITIVE_INFINITY;
                double right = Double.NEGATIVE_INFINITY;
                double top = Double.NEGATIVE_INFINITY;
                for (int index = 1; index < transformed.getCount(); index++) {
                    Point2D value = transformed.getItem(index);
                    left = Math.min(left, value.getX());
                    bottom = Math.min(bottom, value.getY());
                    right = Math.max(right, value.getX());
                    top = Math.max(top, value.getY());
                }
                geoBounds.put("left", left);
                geoBounds.put("bottom", bottom);
                geoBounds.put("right", right);
                geoBounds.put("top", top);
            }
            MAPPER.writerWithDefaultPrettyPrinter().writeValue(manifest.toFile(), config);
        }

        private record OSGBSceneMetadata(int epsg, double originX, double originY, double originZ) {}
}
