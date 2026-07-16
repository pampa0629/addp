package com.addp.supermap.workflow;

import static com.addp.supermap.workflow.SuperMapRuntimeSupport.*;

import com.fasterxml.jackson.databind.JsonNode;
import io.minio.DownloadObjectArgs;
import io.minio.MinioClient;
import io.minio.UploadObjectArgs;
import java.io.IOException;
import java.net.URI;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.util.Comparator;
import java.util.stream.Stream;

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
      throw new IllegalArgumentException(
          "directory target access method must be mounted_path or object_store");
    }
    String prefix = access.path("prefix").asText("").replace('\\', '/').replaceAll("^/+|/+$", "");
    try {
      MinioClient client = buildObjectStoreClient(access);
      try (Stream<Path> paths = Files.walk(renderRoot)) {
        for (Path file : paths.filter(Files::isRegularFile).toList()) {
          String relative = renderRoot.relativize(file).toString().replace('\\', '/');
          String object = prefix.isBlank() ? relative : prefix + "/" + relative;
          client.uploadObject(
              UploadObjectArgs.builder()
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
      throw new IllegalArgumentException(
          "CAD source access method must be mounted_path or object_store");
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
      client.downloadObject(
          DownloadObjectArgs.builder()
              .bucket(bucket)
              .object(object)
              .filename(localFile.toString())
              .build());
      return new WorkflowAccessFile(localFile, tempRoot);
    } catch (Exception ex) {
      deleteRecursively(tempRoot);
      throw new IllegalStateException(
          "failed to materialize CAD object " + bucket + "/" + object, ex);
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
    String candidate =
        endpoint.contains("://") ? endpoint : (useSSL ? "https://" : "http://") + endpoint;
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
    String normalizedEndpoint =
        scheme.toLowerCase() + "://" + hostForURL + (uri.getPort() > 0 ? ":" + uri.getPort() : "");
    return MinioClient.builder()
        .endpoint(normalizedEndpoint)
        .credentials(paramText(access, "access_key"), paramText(access, "secret_key"))
        .build();
  }

  static Path resolveWorkflowAccessPath(JsonNode access) {
    if (!"mounted_path".equals(access.path("method").asText())) {
      throw new IllegalArgumentException(
          "SuperMap S3M conversion currently requires mounted_path access");
    }
    Path configuredPath = Path.of(paramText(access, "path")).normalize();
    String server = access.path("server").asText("").trim();
    String exportPath = access.path("export_path").asText("").trim();
    if (server.isBlank() || exportPath.isBlank()) {
      return configuredPath;
    }
    Path exportRoot = Path.of(exportPath).normalize();
    if (!configuredPath.startsWith(exportRoot)) {
      throw new IllegalArgumentException(
          "mounted_path is outside the declared NFS export: " + configuredPath);
    }
    Path mountRoot = dynamicNfsMountRoot(normalizeResourceHost(server), exportPath);
    ensureNfsMounted(
        normalizeResourceHost(server),
        exportPath,
        access.path("nfs_version").asText(""),
        mountRoot);
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
