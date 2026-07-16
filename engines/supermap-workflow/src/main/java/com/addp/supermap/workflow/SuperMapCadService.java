package com.addp.supermap.workflow;

import static com.addp.supermap.workflow.SuperMapAccessService.*;
import static com.addp.supermap.workflow.SuperMapRuntimeSupport.*;
import static com.addp.supermap.workflow.SuperMapWorkflowRuntime.*;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.supermap.data.Dataset;
import com.supermap.data.DatasetVector;
import com.supermap.data.Datasource;
import com.supermap.data.DatasourceConnectionInfo;
import com.supermap.data.EngineType;
import com.supermap.data.GeoStyle;
import com.supermap.data.Rectangle2D;
import com.supermap.data.Workspace;
import java.awt.Color;
import java.awt.Dimension;
import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;
import java.util.UUID;

final class SuperMapCadService {
  static ObjectNode inspectCAD(JsonNode params) {
    JsonNode plan = requireObject(params, "access_plan");
    if (!"addp.workflow.access-plan/v1".equals(plan.path("schema_version").asText())) {
      throw new IllegalArgumentException("unsupported access_plan.schema_version");
    }
    JsonNode source = requireObject(plan, "source");
    if (!"file".equals(source.path("kind").asText())) {
      throw new IllegalArgumentException("access_plan.source must be a CAD file");
    }
    String sourceFormat = requireCADSourceFormat(source);
    WorkflowAccessFile sourceFile = resolveWorkflowAccessFile(requireObject(source, "access"));
    Path sourcePath = sourceFile.path();
    if (!Files.isRegularFile(sourcePath)) {
      sourceFile.close();
      throw new IllegalArgumentException("CAD file does not exist: " + sourcePath);
    }
    String formatVersion;
    try {
      formatVersion = readCADFormatVersion(sourcePath, sourceFormat);
    } catch (RuntimeException ex) {
      sourceFile.close();
      throw ex;
    }

    Workspace workspace = new Workspace();
    DatasourceConnectionInfo connectionInfo = new DatasourceConnectionInfo();
    try {
      connectionInfo.setEngineType(EngineType.VECTORFILE);
      connectionInfo.setServer(sourcePath.toString());
      connectionInfo.setAlias("cad_inspect_" + UUID.randomUUID().toString().replace("-", ""));
      connectionInfo.setReadOnly(true);
      Datasource datasource = workspace.getDatasources().open(connectionInfo);
      if (datasource == null || !datasource.isOpened()) {
        throw new IllegalStateException("SuperMap failed to open CAD datasource: " + sourcePath);
      }

      int datasetCount = datasource.getDatasets().getCount();
      long interpretedRecordCount = 0;
      double minX = Double.POSITIVE_INFINITY;
      double minY = Double.POSITIVE_INFINITY;
      double maxX = Double.NEGATIVE_INFINITY;
      double maxY = Double.NEGATIVE_INFINITY;
      boolean hasBounds = false;
      ArrayNode datasets = MAPPER.createArrayNode();
      for (int index = 0; index < datasetCount; index++) {
        Dataset dataset = datasource.getDatasets().get(index);
        if (dataset == null) {
          continue;
        }
        ObjectNode datasetSummary = datasets.addObject();
        datasetSummary.put("name", dataset.getName());
        datasetSummary.put("dataset_type", String.valueOf(dataset.getType()));
        if (dataset instanceof DatasetVector vector) {
          int recordCount = vector.getRecordCount();
          datasetSummary.put("record_count", recordCount);
          interpretedRecordCount += recordCount;
        }
        Rectangle2D bounds = dataset.getBounds();
        if (bounds != null && !bounds.isEmpty()) {
          hasBounds = true;
          minX = Math.min(minX, bounds.getLeft());
          minY = Math.min(minY, bounds.getBottom());
          maxX = Math.max(maxX, bounds.getRight());
          maxY = Math.max(maxY, bounds.getTop());
        }
      }

      ObjectNode result = MAPPER.createObjectNode();
      result.put("schema_version", "addp.cad.inspect/v1");
      result.put("format", sourceFormat);
      result.put("format_version", formatVersion);
      ObjectNode drawing = result.putObject("drawing");
      drawing.put("drawing_kind", "2d");
      drawing.put("has_model_space", datasetCount > 0);
      drawing.put("layer_count", datasetCount);
      if (hasBounds) {
        ObjectNode bounds = drawing.putObject("bounds_2d");
        bounds.put("min_x", minX);
        bounds.put("min_y", minY);
        bounds.put("max_x", maxX);
        bounds.put("max_y", maxY);
      }
      ObjectNode interpretation = result.putObject("interpretation");
      interpretation.put("dataset_count", datasetCount);
      interpretation.put("interpreted_record_count", interpretedRecordCount);
      interpretation.put("provider", "supermap_iobjects_java");
      interpretation.put("provider_version", superMapProviderVersion());
      interpretation.put("normalized_geometry", true);
      interpretation.put("geometry_traversed", false);
      interpretation.put("scan_complete", true);
      interpretation.set("datasets", datasets);
      interpretation.putArray("warnings");
      return result;
    } finally {
      connectionInfo.dispose();
      try {
        workspace.close();
      } finally {
        workspace.dispose();
        sourceFile.close();
      }
    }
  }

  static ObjectNode renderCADPreview(JsonNode params) {
    JsonNode plan = requireObject(params, "access_plan");
    if (!"addp.workflow.access-plan/v1".equals(plan.path("schema_version").asText())) {
      throw new IllegalArgumentException("unsupported access_plan.schema_version");
    }
    JsonNode source = requireObject(plan, "source");
    JsonNode target = requireObject(plan, "target");
    if (!"file".equals(source.path("kind").asText())) {
      throw new IllegalArgumentException("access_plan.source must be a CAD file");
    }
    String sourceFormat = requireCADSourceFormat(source);
    if (!"directory".equals(target.path("kind").asText())
        || !"cad_preview".equals(target.path("format").asText())) {
      throw new IllegalArgumentException("access_plan.target must be directory/cad_preview");
    }
    int tileSize = Math.max(128, Math.min(1024, optionalInt(params, "tile_size", 512)));
    int maxZoom = Math.max(0, Math.min(8, optionalInt(params, "max_zoom", 4)));
    WorkflowAccessFile sourceFile = resolveWorkflowAccessFile(requireObject(source, "access"));
    Path renderRoot;
    try {
      renderRoot = Files.createTempDirectory("addp-supermap-cad-preview-");
    } catch (IOException ex) {
      sourceFile.close();
      throw new IllegalStateException("failed to create CAD preview directory", ex);
    }

    Workspace workspace = new Workspace();
    DatasourceConnectionInfo connectionInfo = new DatasourceConnectionInfo();
    com.supermap.mapping.Map map = new com.supermap.mapping.Map(workspace);
    GeoStyle backgroundStyle = new GeoStyle();
    try {
      Path sourcePath = sourceFile.path();
      readCADFormatVersion(sourcePath, sourceFormat);
      connectionInfo.setEngineType(EngineType.VECTORFILE);
      connectionInfo.setServer(sourcePath.toString());
      connectionInfo.setAlias("cad_preview_" + UUID.randomUUID().toString().replace("-", ""));
      connectionInfo.setReadOnly(true);
      Datasource datasource = workspace.getDatasources().open(connectionInfo);
      if (datasource == null || !datasource.isOpened()) {
        throw new IllegalStateException("SuperMap failed to open CAD datasource: " + sourcePath);
      }

      Rectangle2D drawingBounds = null;
      int datasetCount = datasource.getDatasets().getCount();
      for (int index = 0; index < datasetCount; index++) {
        Dataset dataset = datasource.getDatasets().get(index);
        if (dataset == null) {
          continue;
        }
        map.getLayers().add(dataset, true);
        Rectangle2D bounds = dataset.getBounds();
        if (bounds == null || bounds.isEmpty()) {
          continue;
        }
        if (drawingBounds == null) {
          drawingBounds =
              new Rectangle2D(
                  bounds.getLeft(), bounds.getBottom(), bounds.getRight(), bounds.getTop());
        } else {
          drawingBounds.setLeft(Math.min(drawingBounds.getLeft(), bounds.getLeft()));
          drawingBounds.setBottom(Math.min(drawingBounds.getBottom(), bounds.getBottom()));
          drawingBounds.setRight(Math.max(drawingBounds.getRight(), bounds.getRight()));
          drawingBounds.setTop(Math.max(drawingBounds.getTop(), bounds.getTop()));
        }
      }
      if (drawingBounds == null
          || drawingBounds.isEmpty()
          || drawingBounds.getWidth() <= 0
          || drawingBounds.getHeight() <= 0) {
        throw new IllegalStateException("CAD datasource has no renderable 2D bounds");
      }
      double renderSpan = Math.max(drawingBounds.getWidth(), drawingBounds.getHeight());
      double centerX = (drawingBounds.getLeft() + drawingBounds.getRight()) / 2.0d;
      double centerY = (drawingBounds.getBottom() + drawingBounds.getTop()) / 2.0d;
      Rectangle2D renderBounds =
          new Rectangle2D(
              centerX - renderSpan / 2.0d,
              centerY - renderSpan / 2.0d,
              centerX + renderSpan / 2.0d,
              centerY + renderSpan / 2.0d);
      long expectedTileCount = 0;
      for (int z = 0; z <= maxZoom; z++) {
        long side = 1L << z;
        expectedTileCount += side * side;
      }
      if (expectedTileCount > 25000L) {
        throw new IllegalArgumentException("CAD preview max_zoom produces more than 25000 tiles");
      }
      map.setImageSize(new Dimension(tileSize, tileSize));
      map.setInflateBounds(false);
      backgroundStyle.setFillForeColor(new Color(30, 30, 30));
      backgroundStyle.setFillBackColor(new Color(30, 30, 30));
      backgroundStyle.setFillBackOpaque(true);
      backgroundStyle.setFillOpaqueRate(100);
      map.setPaintBackground(true);
      map.setBackgroundStyle(backgroundStyle);
      Files.createDirectories(renderRoot.resolve("model-space"));
      Path thumbnail = renderRoot.resolve("thumbnail.webp");
      if (!outputCADMapToWebP(map, thumbnail, renderBounds)) {
        throw new IllegalStateException("SuperMap failed to render CAD thumbnail");
      }

      long tileCount = 0;
      for (int z = 0; z <= maxZoom; z++) {
        int side = 1 << z;
        double tileWidth = renderBounds.getWidth() / side;
        double tileHeight = renderBounds.getHeight() / side;
        for (int x = 0; x < side; x++) {
          for (int y = 0; y < side; y++) {
            double left = renderBounds.getLeft() + x * tileWidth;
            double right = left + tileWidth;
            double top = renderBounds.getTop() - y * tileHeight;
            double bottom = top - tileHeight;
            Path tile =
                renderRoot
                    .resolve("model-space")
                    .resolve(String.valueOf(z))
                    .resolve(String.valueOf(x))
                    .resolve(y + ".webp");
            Files.createDirectories(tile.getParent());
            Rectangle2D tileBounds = new Rectangle2D(left, bottom, right, top);
            if (!outputCADMapToWebP(map, tile, tileBounds)) {
              throw new IllegalStateException(
                  "SuperMap failed to render CAD tile " + z + "/" + x + "/" + y);
            }
            tileCount++;
          }
        }
      }

      ObjectNode manifest = MAPPER.createObjectNode();
      manifest.put("schema_version", "addp.cad.preview-manifest/v1");
      manifest.put("tile_size", tileSize);
      manifest.put("min_zoom", 0);
      manifest.put("max_zoom", maxZoom);
      manifest.put("tile_format", "webp");
      manifest.put("tile_template", "model-space/{z}/{x}/{y}.webp");
      ObjectNode bounds = manifest.putObject("bounds_2d");
      bounds.put("min_x", renderBounds.getLeft());
      bounds.put("min_y", renderBounds.getBottom());
      bounds.put("max_x", renderBounds.getRight());
      bounds.put("max_y", renderBounds.getTop());
      ObjectNode drawing = manifest.putObject("drawing_bounds_2d");
      drawing.put("min_x", drawingBounds.getLeft());
      drawing.put("min_y", drawingBounds.getBottom());
      drawing.put("max_x", drawingBounds.getRight());
      drawing.put("max_y", drawingBounds.getTop());
      manifest
          .putArray("spaces")
          .addObject()
          .put("id", "model-space")
          .put("kind", "model_space")
          .put("title", "Model Space");
      Files.writeString(
          renderRoot.resolve("manifest.json"),
          MAPPER.writerWithDefaultPrettyPrinter().writeValueAsString(manifest),
          StandardCharsets.UTF_8);

      JsonNode targetAccess = requireObject(target, "access");
      publishDirectory(renderRoot, targetAccess);
      ObjectNode result = MAPPER.createObjectNode();
      result.put("schema_version", "addp.cad.render-preview/v1");
      result.put("format", sourceFormat);
      result.put("manifest_ref", "manifest.json");
      result.put("thumbnail_ref", "thumbnail.webp");
      result.put("tile_count", tileCount);
      result.put("dataset_count", datasetCount);
      result.set("bounds_2d", bounds.deepCopy());
      return result;
    } catch (IOException ex) {
      throw new IllegalStateException("failed to write CAD preview artifact", ex);
    } finally {
      try {
        map.close();
        map.dispose();
      } finally {
        backgroundStyle.dispose();
        connectionInfo.dispose();
        try {
          workspace.close();
          workspace.dispose();
        } finally {
          sourceFile.close();
          deleteRecursively(renderRoot);
        }
      }
    }
  }

  private static boolean outputCADMapToWebP(
      com.supermap.mapping.Map map, Path output, Rectangle2D bounds) {
    map.setViewBounds(bounds);
    return map.outputMapToWEBP(output.toString(), false);
  }

  private static String requireCADSourceFormat(JsonNode source) {
    String sourceFormat = source.path("format").asText("").trim().toLowerCase();
    if (!"dwg".equals(sourceFormat) && !"dxf".equals(sourceFormat)) {
      throw new IllegalArgumentException("access_plan.source.format must be dwg or dxf");
    }
    return sourceFormat;
  }

  private static String readCADFormatVersion(Path sourcePath, String sourceFormat) {
    return switch (sourceFormat) {
      case "dwg" -> readDWGVersion(sourcePath);
      case "dxf" -> readDXFVersion(sourcePath);
      default -> throw new IllegalArgumentException("unsupported CAD format: " + sourceFormat);
    };
  }

  private static String readDWGVersion(Path sourcePath) {
    try (InputStream input = Files.newInputStream(sourcePath)) {
      byte[] header = input.readNBytes(6);
      String version = new String(header, StandardCharsets.US_ASCII);
      if (header.length != 6 || !version.matches("AC10[0-9]{2}")) {
        throw new IllegalArgumentException("invalid DWG AC10xx header: " + sourcePath);
      }
      return version;
    } catch (IOException ex) {
      throw new IllegalStateException("failed to read DWG header: " + sourcePath, ex);
    }
  }

  private static String readDXFVersion(Path sourcePath) {
    byte[] binarySignature =
        new byte[] {
          'A', 'u', 't', 'o', 'C', 'A', 'D', ' ', 'B', 'i', 'n', 'a', 'r', 'y', ' ', 'D', 'X', 'F',
          '\r', '\n', 0x1a, 0x00
        };
    try (InputStream input = Files.newInputStream(sourcePath)) {
      byte[] header = input.readNBytes(binarySignature.length);
      if (Arrays.equals(header, binarySignature)) {
        return "";
      }
    } catch (IOException ex) {
      throw new IllegalStateException("failed to read DXF header: " + sourcePath, ex);
    }
    try (BufferedReader reader = Files.newBufferedReader(sourcePath, StandardCharsets.US_ASCII)) {
      String first = reader.readLine();
      String second = reader.readLine();
      if (first == null
          || second == null
          || !first.replace("\uFEFF", "").trim().equals("0")
          || !second.trim().equalsIgnoreCase("SECTION")) {
        throw new IllegalArgumentException("invalid ASCII DXF SECTION header: " + sourcePath);
      }
      for (int lineNumber = 2; lineNumber < 4096; lineNumber++) {
        String line = reader.readLine();
        if (line == null) {
          break;
        }
        if (!"$ACADVER".equalsIgnoreCase(line.trim())) {
          continue;
        }
        String groupCode = reader.readLine();
        String value = reader.readLine();
        if (groupCode != null && value != null && "1".equals(groupCode.trim())) {
          return value.trim();
        }
        break;
      }
      return "";
    } catch (IOException ex) {
      throw new IllegalStateException("failed to read DXF header: " + sourcePath, ex);
    }
  }

  private static String superMapProviderVersion() {
    Package providerPackage = Workspace.class.getPackage();
    String version = providerPackage == null ? null : providerPackage.getImplementationVersion();
    return version == null || version.isBlank() ? "12.1" : version;
  }
}
