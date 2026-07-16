package com.addp.supermap.workflow;

import static com.addp.supermap.workflow.SuperMapConversionProcesses.*;
import static com.addp.supermap.workflow.SuperMapDatasetProcesses.*;
import static com.addp.supermap.workflow.SuperMapDatasourceProcesses.*;
import static com.addp.supermap.workflow.SuperMapModels.*;
import static com.addp.supermap.workflow.SuperMapOperatorMetadata.*;
import static com.addp.supermap.workflow.SuperMapVectorProcesses.*;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.supermap.sps.core.workflow.IProcess;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Set;

final class SuperMapOperatorRegistry {
  private static final Map<String, WorkflowProcessFactory> WORKFLOW_FACTORIES =
      Map.ofEntries(
          Map.entry(
              "datasource.open", (params, context) -> new OpenDatasourceProcess(context, params)),
          Map.entry(
              "datasource.open_postgis",
              (params, context) -> new OpenPostgisDatasourceProcess(context, params)),
          Map.entry(
              "datasource.create",
              (params, context) -> new CreateDatasourceProcess(context, params)),
          Map.entry("dataset.select", (params, context) -> new SelectDatasetProcess(params)),
          Map.entry("dataset.info", (params, context) -> new DatasetInfoProcess(params)),
          Map.entry("dataset.project", (params, context) -> new DatasetProjectProcess(params)),
          Map.entry("vector.filter", (params, context) -> new VectorFilterProcess(params)),
          Map.entry(
              "vector.spatial_filter", (params, context) -> new VectorSpatialFilterProcess(params)),
          Map.entry("vector.buffer", (params, context) -> new VectorBufferProcess(params)),
          Map.entry("vector.dissolve", (params, context) -> new VectorDissolveProcess(params)),
          Map.entry("vector.merge", (params, context) -> new VectorMergeProcess(params)),
          Map.entry(
              "vector.feature_envelope",
              (params, context) -> new VectorFeatureEnvelopeProcess(params)),
          Map.entry("vector.inner_point", (params, context) -> new VectorInnerPointProcess(params)),
          Map.entry(
              "overlay.intersect",
              (params, context) -> new OverlayBinaryProcess("overlay.intersect", params)),
          Map.entry(
              "overlay.clip",
              (params, context) -> new OverlayBinaryProcess("overlay.clip", params)),
          Map.entry(
              "overlay.erase",
              (params, context) -> new OverlayBinaryProcess("overlay.erase", params)),
          Map.entry(
              "overlay.union",
              (params, context) -> new OverlayBinaryProcess("overlay.union", params)),
          Map.entry("vector.query", (params, context) -> new VectorQueryProcess(params)),
          Map.entry("dataset.save", (params, context) -> new SaveDatasetProcess(params)),
          Map.entry("osgb_scene_to_s3m", (params, context) -> new OSGBSceneToS3MProcess(params)));

  private static final Map<String, DirectOperatorHandler> DIRECT_HANDLERS =
      Map.of(
          "datasource.enable_postgis", SuperMapDirectOperatorService::enablePostgis,
          "datasource.upgrade_udbx", SuperMapDirectOperatorService::upgradeUdbx,
          "osgb_scene_to_s3m", SuperMapS3MConversionService::convertOSGBSceneToS3M,
          "cad.inspect", SuperMapCadService::inspectCAD,
          "cad.render_preview", SuperMapCadService::renderCADPreview);

  private static final Set<String> SUPERMAP_STORAGE_OPERATORS =
      Set.of(
          "datasource.open",
          "datasource.open_postgis",
          "datasource.create",
          "datasource.enable_postgis",
          "overlay.intersect",
          "overlay.clip",
          "overlay.erase",
          "overlay.union",
          "vector.filter",
          "vector.spatial_filter",
          "vector.buffer",
          "vector.dissolve",
          "vector.merge",
          "vector.feature_envelope",
          "vector.inner_point",
          "dataset.project",
          "dataset.save",
          "osgb_scene_to_s3m");

  private static final Map<String, OperatorDefinition> DEFINITIONS = buildDefinitions();

  private SuperMapOperatorRegistry() {}

  static Map<String, ObjectNode> operators() {
    Map<String, ObjectNode> result = new LinkedHashMap<>();
    DEFINITIONS.forEach((id, definition) -> result.put(id, definition.descriptor()));
    return result;
  }

  static boolean operatorSupportsMode(ObjectNode operator, String mode) {
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

  static ObjectNode invokeDirectOperator(String name, JsonNode params) {
    OperatorDefinition definition = requireDefinition(name);
    if (definition.directHandler() == null) {
      throw new IllegalArgumentException("operator does not support direct execution: " + name);
    }
    return definition.directHandler().invoke(params);
  }

  static IProcess createProcess(
      String operator, JsonNode params, WorkflowExecutionContext context) {
    OperatorDefinition definition = requireDefinition(operator);
    if (definition.workflowFactory() == null) {
      throw new IllegalArgumentException(
          "operator does not support workflow execution: " + operator);
    }
    return definition.workflowFactory().create(params, context);
  }

  static String defaultOutputPort(String operator) {
    return requireDefinition(operator).defaultOutputPort();
  }

  static String storageFor(String operator) {
    OperatorDefinition definition = DEFINITIONS.get(operator);
    return definition == null ? "memory" : definition.storage();
  }

  private static OperatorDefinition requireDefinition(String operator) {
    OperatorDefinition definition = DEFINITIONS.get(operator);
    if (definition == null) {
      throw new IllegalArgumentException("unsupported operator: " + operator);
    }
    return definition;
  }

  private static Map<String, OperatorDefinition> buildDefinitions() {
    Map<String, ObjectNode> descriptors = buildOperatorDescriptors();
    Map<String, OperatorDefinition> definitions = new LinkedHashMap<>();
    descriptors.forEach(
        (id, descriptor) -> {
          WorkflowProcessFactory workflowFactory = WORKFLOW_FACTORIES.get(id);
          DirectOperatorHandler directHandler = DIRECT_HANDLERS.get(id);
          validateExecutionModes(id, descriptor, workflowFactory, directHandler);
          definitions.put(
              id,
              new OperatorDefinition(
                  descriptor,
                  workflowFactory,
                  directHandler,
                  findDefaultOutputPort(id, descriptor),
                  SUPERMAP_STORAGE_OPERATORS.contains(id) ? "datasource" : "memory"));
        });
    for (String id : WORKFLOW_FACTORIES.keySet()) {
      if (!descriptors.containsKey(id)) {
        throw new IllegalStateException("workflow factory has no operator descriptor: " + id);
      }
    }
    for (String id : DIRECT_HANDLERS.keySet()) {
      if (!descriptors.containsKey(id)) {
        throw new IllegalStateException("direct handler has no operator descriptor: " + id);
      }
    }
    return Map.copyOf(definitions);
  }

  private static void validateExecutionModes(
      String id,
      ObjectNode descriptor,
      WorkflowProcessFactory workflowFactory,
      DirectOperatorHandler directHandler) {
    boolean workflowMode = operatorSupportsMode(descriptor, "workflow");
    boolean directMode = operatorSupportsMode(descriptor, "direct");
    if (workflowMode != (workflowFactory != null)) {
      throw new IllegalStateException(
          "workflow execution mode and factory disagree for operator: " + id);
    }
    if (directMode != (directHandler != null)) {
      throw new IllegalStateException(
          "direct execution mode and handler disagree for operator: " + id);
    }
  }

  private static String findDefaultOutputPort(String id, ObjectNode descriptor) {
    JsonNode outputPorts = descriptor.path("output_ports");
    if (outputPorts.isArray()) {
      for (JsonNode outputPort : outputPorts) {
        if (outputPort.path("is_default").asBoolean(false)) {
          String name = outputPort.path("name").asText("");
          if (!name.isBlank()) {
            return name;
          }
        }
      }
    }
    throw new IllegalStateException("operator has no default output port: " + id);
  }

  @FunctionalInterface
  private interface WorkflowProcessFactory {
    IProcess create(JsonNode params, WorkflowExecutionContext context);
  }

  @FunctionalInterface
  private interface DirectOperatorHandler {
    ObjectNode invoke(JsonNode params);
  }

  private record OperatorDefinition(
      ObjectNode descriptor,
      WorkflowProcessFactory workflowFactory,
      DirectOperatorHandler directHandler,
      String defaultOutputPort,
      String storage) {}
}
