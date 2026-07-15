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
import static com.addp.supermap.workflow.SuperMapRuntimeSupport.*;
import static com.addp.supermap.workflow.SuperMapS3MConversionService.*;
import static com.addp.supermap.workflow.SuperMapWorkflowRuntime.*;

final class SuperMapProcesses {
        public abstract static class BaseProcess extends AbstractProcess {
            BaseProcess(String name) {
                super("addp.supermap.workflow", name);
                this.parameters = new ParametersImpl(this);
            }

            @Override
            public String getFactory() {
                return SPS_FACTORY;
            }

            protected void addStringInput(String name, String value) {
                ISingleDataDefinition<String> def = new DefaultSingleDataDefinition<>(String.class);
                SingleInputImpl<String> input = new SingleInputImpl<>(name, def);
                input.setValueProvider(new ConstantValueProvider<>(def, value));
                this.parameters.addInput(input);
            }

            protected void addBooleanInput(String name, boolean value) {
                ISingleDataDefinition<Boolean> def = new DefaultSingleDataDefinition<>(Boolean.class);
                SingleInputImpl<Boolean> input = new SingleInputImpl<>(name, def);
                input.setValueProvider(new ConstantValueProvider<>(def, value));
                this.parameters.addInput(input);
            }

            protected void addDoubleInput(String name, double value) {
                ISingleDataDefinition<Double> def = new DefaultSingleDataDefinition<>(Double.class);
                SingleInputImpl<Double> input = new SingleInputImpl<>(name, def);
                input.setValueProvider(new ConstantValueProvider<>(def, value));
                this.parameters.addInput(input);
            }

            protected void addIntegerInput(String name, int value) {
                ISingleDataDefinition<Integer> def = new DefaultSingleDataDefinition<>(Integer.class);
                SingleInputImpl<Integer> input = new SingleInputImpl<>(name, def);
                input.setValueProvider(new ConstantValueProvider<>(def, value));
                this.parameters.addInput(input);
            }

            protected void addStringArrayInput(String name, String[] value) {
                ISingleDataDefinition<String[]> def = new DefaultSingleDataDefinition<>(String[].class);
                SingleInputImpl<String[]> input = new SingleInputImpl<>(name, def);
                input.setValueProvider(new ConstantValueProvider<>(def, value));
                this.parameters.addInput(input);
            }

            protected <T> void addObjectInput(String name, Class<T> type) {
                ISingleDataDefinition<T> def = new DefaultSingleDataDefinition<>(type);
                SingleInputImpl<T> input = new SingleInputImpl<>(name, def);
                input.setRequired(true);
                this.parameters.addInput(input);
            }

            protected <T> SingleOutputImpl<T> addObjectOutput(String name, Class<T> type) {
                ISingleDataDefinition<T> def = new DefaultSingleDataDefinition<>(type);
                SingleOutputImpl<T> output = new SingleOutputImpl<>(name, def);
                this.parameters.addOutput(output);
                return output;
            }
        }

        public static final class OpenDatasourceProcess extends BaseProcess {
            private final WorkflowExecutionContext context;
            private final SingleOutputImpl<SuperMapDatasourceRef> output;

            OpenDatasourceProcess(WorkflowExecutionContext context, JsonNode params) {
                super("datasource.open");
                this.context = context;
                addStringInput("path", paramText(params, "path"));
                addStringInput("alias", optionalText(params, "alias", ""));
                addBooleanInput("read_only", optionalBoolean(params, "read_only", true));
                this.output = addObjectOutput("datasource", SuperMapDatasourceRef.class);
            }

            @Override
            public boolean execute() {
                String path = String.valueOf(parameters.getInput("path").getValue());
                String alias = String.valueOf(parameters.getInput("alias").getValue());
                boolean readOnly = Boolean.TRUE.equals(parameters.getInput("read_only").getValue());
                output.setValue(context.openUdbx(path, alias, readOnly));
                return true;
            }
        }

        public static final class OpenPostgisDatasourceProcess extends BaseProcess {
            private final WorkflowExecutionContext context;
            private final SingleOutputImpl<SuperMapDatasourceRef> output;

            OpenPostgisDatasourceProcess(WorkflowExecutionContext context, JsonNode params) {
                super("datasource.open_postgis");
                this.context = context;
                JsonNode connInfo = requireObject(params, "connection_info");
                addStringInput("server", postgisServer(connInfo));
                addStringInput("database", requireConnText(connInfo, "database"));
                addStringInput("user", requireConnText(connInfo, "user"));
                addStringInput("password", optionalConnText(connInfo, "password", ""));
                addStringInput("schema", optionalText(params, "schema", ""));
                addStringInput("table", optionalText(params, "table", ""));
                addStringInput("alias", optionalText(params, "alias", defaultPostgisAlias(params)));
                addBooleanInput("read_only", optionalBoolean(params, "read_only", true));
                this.output = addObjectOutput("datasource", SuperMapDatasourceRef.class);
            }

            @Override
            public boolean execute() {
                String server = String.valueOf(parameters.getInput("server").getValue());
                String database = String.valueOf(parameters.getInput("database").getValue());
                String user = String.valueOf(parameters.getInput("user").getValue());
                String password = String.valueOf(parameters.getInput("password").getValue());
                String schema = String.valueOf(parameters.getInput("schema").getValue());
                String table = String.valueOf(parameters.getInput("table").getValue());
                String alias = String.valueOf(parameters.getInput("alias").getValue());
                boolean readOnly = Boolean.TRUE.equals(parameters.getInput("read_only").getValue());
                output.setValue(context.openPostgis(server, database, user, password, schema, table, alias, readOnly));
                return true;
            }
        }

        public static final class CreateDatasourceProcess extends BaseProcess {
            private final WorkflowExecutionContext context;
            private final SingleOutputImpl<SuperMapDatasourceRef> output;

            CreateDatasourceProcess(WorkflowExecutionContext context, JsonNode params) {
                super("datasource.create");
                this.context = context;
                JsonNode connInfo = requireObject(params, "connection_info");
                Path resolvedPath = resolveUdbxPath(connInfo, paramText(params, "path"));
                addStringInput("path", resolvedPath.toString());
                addStringInput("alias", optionalText(params, "alias", ""));
                addBooleanInput("overwrite", optionalBoolean(params, "overwrite", false));
                this.output = addObjectOutput("datasource", SuperMapDatasourceRef.class);
            }

            @Override
            public boolean execute() {
                String path = String.valueOf(parameters.getInput("path").getValue());
                String alias = String.valueOf(parameters.getInput("alias").getValue());
                boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());
                output.setValue(context.createUdbx(path, alias, overwrite));
                return true;
            }
        }

        public static final class SelectDatasetProcess extends BaseProcess {
            private final SingleOutputImpl<SuperMapDatasetRef> output;

            SelectDatasetProcess(JsonNode params) {
                super("dataset.select");
                addObjectInput("datasource", SuperMapDatasourceRef.class);
                addStringInput("dataset_name", paramText(params, "dataset_name"));
                this.output = addObjectOutput("dataset", SuperMapDatasetRef.class);
            }

            @Override
            public boolean execute() {
                SuperMapDatasourceRef datasource = (SuperMapDatasourceRef) parameters.getInput("datasource").getValue();
                String datasetName = String.valueOf(parameters.getInput("dataset_name").getValue());
                Dataset dataset = datasource.datasource.getDatasets().get(datasetName);
                if (!(dataset instanceof DatasetVector datasetVector)) {
                    throw new IllegalArgumentException("dataset is not a DatasetVector: " + datasetName);
                }
                output.setValue(new SuperMapDatasetRef(datasource, datasetVector));
                return true;
            }
        }

        public static final class DatasetInfoProcess extends BaseProcess {
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

        public static final class VectorFilterProcess extends BaseProcess {
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
                SuperMapDatasourceRef outputDatasource = (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
                String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
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
                    DatasetVector result = outputDatasource.datasource.recordsetToDataset(recordset, outputDatasetName);
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

        public static final class DatasetProjectProcess extends BaseProcess {
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
                SuperMapDatasourceRef outputDatasource = (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
                String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
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
                    Dataset projected = CoordSysTranslator.convert(
                            dataset.dataset,
                            target,
                            outputDatasource.datasource,
                            outputDatasetName,
                            parameter,
                            coordSysTransMethod(method)
                    );
                    if (!(projected instanceof DatasetVector projectedVector)) {
                        throw new IllegalStateException("CoordSysTranslator.convert did not return DatasetVector: " + outputDatasetName);
                    }
                    output.setValue(new SuperMapDatasetRef(outputDatasource, projectedVector));
                } finally {
                    parameter.dispose();
                    target.dispose();
                }
                return true;
            }
        }

        public static final class VectorSpatialFilterProcess extends BaseProcess {
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
                SuperMapDatasetRef input = (SuperMapDatasetRef) parameters.getInput("input_dataset").getValue();
                SuperMapDatasetRef filter = (SuperMapDatasetRef) parameters.getInput("filter_dataset").getValue();
                SuperMapDatasourceRef outputDatasource = (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
                String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
                String relation = String.valueOf(parameters.getInput("relation").getValue());
                boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());

                ensureDatasetNameAvailable(outputDatasource.datasource, outputDatasetName, overwrite);
                int[] ids = input.dataset.getIDsByGeoRelation(filter.dataset, spatialRelationType(relation), true, false);
                Recordset recordset = input.dataset.query(ids == null ? new int[0] : ids, CursorType.STATIC);
                try {
                    if (recordset == null) {
                        throw new IllegalStateException("spatial relation query returned null recordset");
                    }
                    DatasetVector result = outputDatasource.datasource.recordsetToDataset(recordset, outputDatasetName);
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

        public static final class VectorBufferProcess extends BaseProcess {
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
                SuperMapDatasetRef input = (SuperMapDatasetRef) parameters.getInput("input_dataset").getValue();
                SuperMapDatasourceRef outputDatasource = (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
                String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
                double distance = (Double) parameters.getInput("distance").getValue();
                String radiusUnit = String.valueOf(parameters.getInput("radius_unit").getValue());
                String endType = String.valueOf(parameters.getInput("end_type").getValue());
                int semicircleSegments = (Integer) parameters.getInput("semicircle_segments").getValue();
                boolean dissolve = Boolean.TRUE.equals(parameters.getInput("dissolve").getValue());
                boolean keepAttributes = Boolean.TRUE.equals(parameters.getInput("keep_attributes").getValue());
                boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());

                DatasetVector outputDataset = createOutputDataset(outputDatasource.datasource, outputDatasetName, DatasetType.REGION, overwrite, input.dataset);
                BufferAnalystParameter parameter = new BufferAnalystParameter();
                parameter.setLeftDistance(distance);
                parameter.setRightDistance(distance);
                parameter.setRadiusUnit(bufferRadiusUnit(radiusUnit));
                parameter.setEndType(bufferEndType(endType));
                parameter.setSemicircleLineSegment(semicircleSegments);
                boolean ok = BufferAnalyst.createBuffer(input.dataset, outputDataset, parameter, dissolve, keepAttributes);
                if (!ok) {
                    throw new IllegalStateException("BufferAnalyst.createBuffer returned false");
                }
                output.setValue(new SuperMapDatasetRef(outputDatasource, outputDataset));
                return true;
            }
        }

        public static final class VectorDissolveProcess extends BaseProcess {
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
                SuperMapDatasetRef input = (SuperMapDatasetRef) parameters.getInput("input_dataset").getValue();
                SuperMapDatasourceRef outputDatasource = (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
                String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
                String[] fieldNames = (String[]) parameters.getInput("field_names").getValue();
                String type = String.valueOf(parameters.getInput("dissolve_type").getValue());
                double tolerance = (Double) parameters.getInput("tolerance").getValue();
                boolean saveAllFields = Boolean.TRUE.equals(parameters.getInput("save_all_fields").getValue());
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
                    DatasetVector result = Generalization.dissolve(input.dataset, outputDatasource.datasource, outputDatasetName, parameter);
                    if (result == null) {
                        throw new IllegalStateException("Generalization.dissolve returned null: " + outputDatasetName);
                    }
                    output.setValue(new SuperMapDatasetRef(outputDatasource, result));
                } finally {
                    parameter.dispose();
                }
                return true;
            }
        }

        public static final class VectorMergeProcess extends BaseProcess {
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
                SuperMapDatasetRef primary = (SuperMapDatasetRef) parameters.getInput("primary_dataset").getValue();
                SuperMapDatasetRef append = (SuperMapDatasetRef) parameters.getInput("append_dataset").getValue();
                SuperMapDatasourceRef outputDatasource = (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
                String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
                boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());

                ensureDatasetNameAvailable(outputDatasource.datasource, outputDatasetName, overwrite);
                Dataset copied = outputDatasource.datasource.copyDataset(primary.dataset, outputDatasetName, EncodeType.NONE);
                if (!(copied instanceof DatasetVector copiedVector)) {
                    throw new IllegalStateException("copyDataset did not return DatasetVector: " + outputDatasetName);
                }
                Recordset recordset = append.dataset.getRecordset(false, CursorType.STATIC);
                try {
                    if (recordset == null) {
                        throw new IllegalStateException("append dataset returned null recordset");
                    }
                    if (!copiedVector.append(recordset)) {
                        throw new IllegalStateException("DatasetVector.append returned false: " + outputDatasetName);
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

        public static final class VectorFeatureEnvelopeProcess extends BaseProcess {
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
                SuperMapDatasetRef input = (SuperMapDatasetRef) parameters.getInput("input_dataset").getValue();
                SuperMapDatasourceRef outputDatasource = (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
                String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
                boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());

                ensureDatasetNameAvailable(outputDatasource.datasource, outputDatasetName, overwrite);
                DatasetVector result = Generalization.featureEnvelope(input.dataset, outputDatasetName, outputDatasource.datasource);
                if (result == null) {
                    throw new IllegalStateException("Generalization.featureEnvelope returned null: " + outputDatasetName);
                }
                output.setValue(new SuperMapDatasetRef(outputDatasource, result));
                return true;
            }
        }

        public static final class VectorInnerPointProcess extends BaseProcess {
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
                SuperMapDatasetRef input = (SuperMapDatasetRef) parameters.getInput("input_dataset").getValue();
                SuperMapDatasourceRef outputDatasource = (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
                String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
                boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());

                ensureDatasetNameAvailable(outputDatasource.datasource, outputDatasetName, overwrite);
                DatasetVector result = outputDatasource.datasource.innerPointToDataset(input.dataset, outputDatasetName);
                if (result == null) {
                    throw new IllegalStateException("Datasource.innerPointToDataset returned null: " + outputDatasetName);
                }
                output.setValue(new SuperMapDatasetRef(outputDatasource, result));
                return true;
            }
        }

        public static final class OverlayBinaryProcess extends BaseProcess {
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
                SuperMapDatasetRef input = (SuperMapDatasetRef) parameters.getInput("input_dataset").getValue();
                SuperMapDatasetRef overlay = (SuperMapDatasetRef) parameters.getInput("overlay_dataset").getValue();
                SuperMapDatasourceRef outputDatasource = (SuperMapDatasourceRef) parameters.getInput("output_datasource").getValue();
                String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
                boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());
                double tolerance = (Double) parameters.getInput("tolerance").getValue();

                DatasetVector outputDataset = createOutputDataset(outputDatasource.datasource, outputDatasetName, input.dataset.getType(), overwrite, input.dataset);
                OverlayAnalystParameter parameter = new OverlayAnalystParameter();
                parameter.setTolerance(tolerance);
                parameter.setPreprocess(true);
                boolean ok = executeOverlay(operator, input.dataset, overlay.dataset, outputDataset, parameter);
                if (!ok) {
                    throw new IllegalStateException("OverlayAnalyst." + overlayMethodName(operator) + " returned false");
                }
                output.setValue(new SuperMapDatasetRef(outputDatasource, outputDataset));
                return true;
            }
        }

        public static final class VectorQueryProcess extends BaseProcess {
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
                    output.setValue(new QueryResult(dataset.dataset.getName(), "", dataset.dataset.getRecordCount()));
                    return true;
                }
                QueryParameter queryParameter = new QueryParameter();
                queryParameter.setAttributeFilter(filter);
                queryParameter.setCursorType(CursorType.STATIC);
                Recordset recordset = dataset.dataset.query(queryParameter);
                try {
                    output.setValue(new QueryResult(dataset.dataset.getName(), filter, recordset == null ? 0 : recordset.getRecordCount()));
                } finally {
                    if (recordset != null) {
                        recordset.dispose();
                    }
                    queryParameter.dispose();
                }
                return true;
            }
        }

        public static final class SaveDatasetProcess extends BaseProcess {
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
                SuperMapDatasourceRef targetDatasource = (SuperMapDatasourceRef) parameters.getInput("target_datasource").getValue();
                String outputDatasetName = String.valueOf(parameters.getInput("output_dataset_name").getValue());
                boolean overwrite = Boolean.TRUE.equals(parameters.getInput("overwrite").getValue());
                ensureDatasetNameAvailable(targetDatasource.datasource, outputDatasetName, overwrite);
                Dataset copied = targetDatasource.datasource.copyDataset(dataset.dataset, outputDatasetName, EncodeType.NONE);
                if (!(copied instanceof DatasetVector copiedVector)) {
                    throw new IllegalStateException("copyDataset did not return DatasetVector: " + outputDatasetName);
                }
                output.setValue(new SuperMapDatasetRef(targetDatasource, copiedVector));
                return true;
            }
        }

        public static final class OSGBSceneToS3MProcess extends BaseProcess {
            private final SingleOutputImpl<ObjectNode> output;

            OSGBSceneToS3MProcess(JsonNode params) {
                super("osgb_scene_to_s3m");
                addStringInput("params_json", params.toString());
                this.output = addObjectOutput("s3m", ObjectNode.class);
            }

            @Override
            public boolean execute() {
                try {
                    JsonNode params = MAPPER.readTree(String.valueOf(parameters.getInput("params_json").getValue()));
                    output.setValue(convertOSGBSceneToS3M(params));
                    return true;
                } catch (IOException ex) {
                    throw new IllegalArgumentException("invalid osgb_scene_to_s3m params", ex);
                }
            }
        }

}
