package com.addp.supermap.workflow;

import static com.addp.supermap.workflow.SuperMapModels.*;
import static com.addp.supermap.workflow.SuperMapRuntimeSupport.*;

import com.fasterxml.jackson.databind.JsonNode;
import com.supermap.data.CoordSysTransParameter;
import com.supermap.data.CoordSysTranslator;
import com.supermap.data.Dataset;
import com.supermap.data.DatasetVector;
import com.supermap.data.EncodeType;
import com.supermap.data.PrjCoordSys;
import com.supermap.sps.core.parameters.impls.SingleOutputImpl;

final class SuperMapDatasetProcesses {
  public static final class SelectDatasetProcess extends SuperMapBaseProcess {
    private final SingleOutputImpl<SuperMapDatasetRef> output;

    SelectDatasetProcess(JsonNode params) {
      super("dataset.select");
      addObjectInput("datasource", SuperMapDatasourceRef.class);
      addStringInput("dataset_name", paramText(params, "dataset_name"));
      this.output = addObjectOutput("dataset", SuperMapDatasetRef.class);
    }

    @Override
    public boolean execute() {
      SuperMapDatasourceRef datasource =
          (SuperMapDatasourceRef) parameters.getInput("datasource").getValue();
      String datasetName = String.valueOf(parameters.getInput("dataset_name").getValue());
      Dataset dataset = datasource.datasource.getDatasets().get(datasetName);
      if (!(dataset instanceof DatasetVector datasetVector)) {
        throw new IllegalArgumentException("dataset is not a DatasetVector: " + datasetName);
      }
      output.setValue(new SuperMapDatasetRef(datasource, datasetVector));
      return true;
    }
  }

  public static final class DatasetInfoProcess extends SuperMapBaseProcess {
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

  public static final class DatasetProjectProcess extends SuperMapBaseProcess {
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
      SuperMapDatasourceRef outputDatasource =
          (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
      String outputDatasetName =
          String.valueOf(parameters.getInput("output_dataset_name").getValue());
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
        Dataset projected =
            CoordSysTranslator.convert(
                dataset.dataset,
                target,
                outputDatasource.datasource,
                outputDatasetName,
                parameter,
                coordSysTransMethod(method));
        if (!(projected instanceof DatasetVector projectedVector)) {
          throw new IllegalStateException(
              "CoordSysTranslator.convert did not return DatasetVector: " + outputDatasetName);
        }
        output.setValue(new SuperMapDatasetRef(outputDatasource, projectedVector));
      } finally {
        parameter.dispose();
        target.dispose();
      }
      return true;
    }
  }

  public static final class SaveDatasetProcess extends SuperMapBaseProcess {
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
      SuperMapDatasourceRef targetDatasource =
          (SuperMapDatasourceRef) parameters.getInput("target_datasource").getValue();
      String outputDatasetName =
          String.valueOf(parameters.getInput("output_dataset_name").getValue());
      boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());
      ensureDatasetNameAvailable(targetDatasource.datasource, outputDatasetName, overwrite);
      Dataset copied =
          targetDatasource.datasource.copyDataset(
              dataset.dataset, outputDatasetName, EncodeType.NONE);
      if (!(copied instanceof DatasetVector copiedVector)) {
        throw new IllegalStateException(
            "copyDataset did not return DatasetVector: " + outputDatasetName);
      }
      output.setValue(new SuperMapDatasetRef(targetDatasource, copiedVector));
      return true;
    }
  }
}
