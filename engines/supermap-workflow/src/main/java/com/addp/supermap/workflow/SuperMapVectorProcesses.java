package com.addp.supermap.workflow;

import static com.addp.supermap.workflow.SuperMapModels.*;
import static com.addp.supermap.workflow.SuperMapRuntimeSupport.*;

import com.fasterxml.jackson.databind.JsonNode;
import com.supermap.analyst.spatialanalyst.BufferAnalyst;
import com.supermap.analyst.spatialanalyst.BufferAnalystParameter;
import com.supermap.analyst.spatialanalyst.DissolveParameter;
import com.supermap.analyst.spatialanalyst.Generalization;
import com.supermap.analyst.spatialanalyst.OverlayAnalystParameter;
import com.supermap.data.CursorType;
import com.supermap.data.Dataset;
import com.supermap.data.DatasetType;
import com.supermap.data.DatasetVector;
import com.supermap.data.EncodeType;
import com.supermap.data.QueryParameter;
import com.supermap.data.Recordset;
import com.supermap.sps.core.parameters.impls.SingleOutputImpl;

final class SuperMapVectorProcesses {
  public static final class VectorFilterProcess extends SuperMapBaseProcess {
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
      SuperMapDatasourceRef outputDatasource =
          (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
      String outputDatasetName =
          String.valueOf(parameters.getInput("output_dataset_name").getValue());
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
        DatasetVector result =
            outputDatasource.datasource.recordsetToDataset(recordset, outputDatasetName);
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

  public static final class VectorSpatialFilterProcess extends SuperMapBaseProcess {
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
      SuperMapDatasetRef input =
          (SuperMapDatasetRef) parameters.getInput("input_dataset").getValue();
      SuperMapDatasetRef filter =
          (SuperMapDatasetRef) parameters.getInput("filter_dataset").getValue();
      SuperMapDatasourceRef outputDatasource =
          (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
      String outputDatasetName =
          String.valueOf(parameters.getInput("output_dataset_name").getValue());
      String relation = String.valueOf(parameters.getInput("relation").getValue());
      boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());

      ensureDatasetNameAvailable(outputDatasource.datasource, outputDatasetName, overwrite);
      int[] ids =
          input.dataset.getIDsByGeoRelation(
              filter.dataset, spatialRelationType(relation), true, false);
      Recordset recordset = input.dataset.query(ids == null ? new int[0] : ids, CursorType.STATIC);
      try {
        if (recordset == null) {
          throw new IllegalStateException("spatial relation query returned null recordset");
        }
        DatasetVector result =
            outputDatasource.datasource.recordsetToDataset(recordset, outputDatasetName);
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

  public static final class VectorBufferProcess extends SuperMapBaseProcess {
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
      SuperMapDatasetRef input =
          (SuperMapDatasetRef) parameters.getInput("input_dataset").getValue();
      SuperMapDatasourceRef outputDatasource =
          (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
      String outputDatasetName =
          String.valueOf(parameters.getInput("output_dataset_name").getValue());
      double distance = (Double) parameters.getInput("distance").getValue();
      String radiusUnit = String.valueOf(parameters.getInput("radius_unit").getValue());
      String endType = String.valueOf(parameters.getInput("end_type").getValue());
      int semicircleSegments = (Integer) parameters.getInput("semicircle_segments").getValue();
      boolean dissolve = Boolean.TRUE.equals(parameters.getInput("dissolve").getValue());
      boolean keepAttributes =
          Boolean.TRUE.equals(parameters.getInput("keep_attributes").getValue());
      boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());

      DatasetVector outputDataset =
          createOutputDataset(
              outputDatasource.datasource,
              outputDatasetName,
              DatasetType.REGION,
              overwrite,
              input.dataset);
      BufferAnalystParameter parameter = new BufferAnalystParameter();
      parameter.setLeftDistance(distance);
      parameter.setRightDistance(distance);
      parameter.setRadiusUnit(bufferRadiusUnit(radiusUnit));
      parameter.setEndType(bufferEndType(endType));
      parameter.setSemicircleLineSegment(semicircleSegments);
      boolean ok =
          BufferAnalyst.createBuffer(
              input.dataset, outputDataset, parameter, dissolve, keepAttributes);
      if (!ok) {
        throw new IllegalStateException("BufferAnalyst.createBuffer returned false");
      }
      output.setValue(new SuperMapDatasetRef(outputDatasource, outputDataset));
      return true;
    }
  }

  public static final class VectorDissolveProcess extends SuperMapBaseProcess {
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
      SuperMapDatasetRef input =
          (SuperMapDatasetRef) parameters.getInput("input_dataset").getValue();
      SuperMapDatasourceRef outputDatasource =
          (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
      String outputDatasetName =
          String.valueOf(parameters.getInput("output_dataset_name").getValue());
      String[] fieldNames = (String[]) parameters.getInput("field_names").getValue();
      String type = String.valueOf(parameters.getInput("dissolve_type").getValue());
      double tolerance = (Double) parameters.getInput("tolerance").getValue();
      boolean saveAllFields =
          Boolean.TRUE.equals(parameters.getInput("save_all_fields").getValue());
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
        DatasetVector result =
            Generalization.dissolve(
                input.dataset, outputDatasource.datasource, outputDatasetName, parameter);
        if (result == null) {
          throw new IllegalStateException(
              "Generalization.dissolve returned null: " + outputDatasetName);
        }
        output.setValue(new SuperMapDatasetRef(outputDatasource, result));
      } finally {
        parameter.dispose();
      }
      return true;
    }
  }

  public static final class VectorMergeProcess extends SuperMapBaseProcess {
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
      SuperMapDatasetRef primary =
          (SuperMapDatasetRef) parameters.getInput("primary_dataset").getValue();
      SuperMapDatasetRef append =
          (SuperMapDatasetRef) parameters.getInput("append_dataset").getValue();
      SuperMapDatasourceRef outputDatasource =
          (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
      String outputDatasetName =
          String.valueOf(parameters.getInput("output_dataset_name").getValue());
      boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());

      ensureDatasetNameAvailable(outputDatasource.datasource, outputDatasetName, overwrite);
      Dataset copied =
          outputDatasource.datasource.copyDataset(
              primary.dataset, outputDatasetName, EncodeType.NONE);
      if (!(copied instanceof DatasetVector copiedVector)) {
        throw new IllegalStateException(
            "copyDataset did not return DatasetVector: " + outputDatasetName);
      }
      Recordset recordset = append.dataset.getRecordset(false, CursorType.STATIC);
      try {
        if (recordset == null) {
          throw new IllegalStateException("append dataset returned null recordset");
        }
        if (!copiedVector.append(recordset)) {
          throw new IllegalStateException(
              "DatasetVector.append returned false: " + outputDatasetName);
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

  public static final class VectorFeatureEnvelopeProcess extends SuperMapBaseProcess {
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
      SuperMapDatasetRef input =
          (SuperMapDatasetRef) parameters.getInput("input_dataset").getValue();
      SuperMapDatasourceRef outputDatasource =
          (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
      String outputDatasetName =
          String.valueOf(parameters.getInput("output_dataset_name").getValue());
      boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());

      ensureDatasetNameAvailable(outputDatasource.datasource, outputDatasetName, overwrite);
      DatasetVector result =
          Generalization.featureEnvelope(
              input.dataset, outputDatasetName, outputDatasource.datasource);
      if (result == null) {
        throw new IllegalStateException(
            "Generalization.featureEnvelope returned null: " + outputDatasetName);
      }
      output.setValue(new SuperMapDatasetRef(outputDatasource, result));
      return true;
    }
  }

  public static final class VectorInnerPointProcess extends SuperMapBaseProcess {
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
      SuperMapDatasetRef input =
          (SuperMapDatasetRef) parameters.getInput("input_dataset").getValue();
      SuperMapDatasourceRef outputDatasource =
          (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
      String outputDatasetName =
          String.valueOf(parameters.getInput("output_dataset_name").getValue());
      boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());

      ensureDatasetNameAvailable(outputDatasource.datasource, outputDatasetName, overwrite);
      DatasetVector result =
          outputDatasource.datasource.innerPointToDataset(input.dataset, outputDatasetName);
      if (result == null) {
        throw new IllegalStateException(
            "Datasource.innerPointToDataset returned null: " + outputDatasetName);
      }
      output.setValue(new SuperMapDatasetRef(outputDatasource, result));
      return true;
    }
  }

  public static final class OverlayBinaryProcess extends SuperMapBaseProcess {
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
      SuperMapDatasetRef input =
          (SuperMapDatasetRef) parameters.getInput("input_dataset").getValue();
      SuperMapDatasetRef overlay =
          (SuperMapDatasetRef) parameters.getInput("overlay_dataset").getValue();
      SuperMapDatasourceRef outputDatasource =
          (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
      String outputDatasetName =
          String.valueOf(parameters.getInput("output_dataset_name").getValue());
      boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());
      double tolerance = (Double) parameters.getInput("tolerance").getValue();

      DatasetVector outputDataset =
          createOutputDataset(
              outputDatasource.datasource,
              outputDatasetName,
              input.dataset.getType(),
              overwrite,
              input.dataset);
      OverlayAnalystParameter parameter = new OverlayAnalystParameter();
      parameter.setTolerance(tolerance);
      parameter.setPreprocess(true);
      boolean ok =
          executeOverlay(operator, input.dataset, overlay.dataset, outputDataset, parameter);
      if (!ok) {
        throw new IllegalStateException(
            "OverlayAnalyst." + overlayMethodName(operator) + " returned false");
      }
      output.setValue(new SuperMapDatasetRef(outputDatasource, outputDataset));
      return true;
    }
  }

  public static final class VectorQueryProcess extends SuperMapBaseProcess {
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
        output.setValue(
            new QueryResult(dataset.dataset.getName(), "", dataset.dataset.getRecordCount()));
        return true;
      }
      QueryParameter queryParameter = new QueryParameter();
      queryParameter.setAttributeFilter(filter);
      queryParameter.setCursorType(CursorType.STATIC);
      Recordset recordset = dataset.dataset.query(queryParameter);
      try {
        output.setValue(
            new QueryResult(
                dataset.dataset.getName(),
                filter,
                recordset == null ? 0 : recordset.getRecordCount()));
      } finally {
        if (recordset != null) {
          recordset.dispose();
        }
        queryParameter.dispose();
      }
      return true;
    }
  }
}
