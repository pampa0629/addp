package com.addp.supermap.workflow;

import static com.addp.supermap.workflow.SuperMapWorkflowRuntime.*;

import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

final class SuperMapOperatorMetadata {
  static Map<String, ObjectNode> buildOperatorDescriptors() {
    Map<String, ObjectNode> result = new LinkedHashMap<>();
    result.put(
        "datasource.open",
        operator(
            "datasource.open",
            "打开数据源",
            "打开已有 UDBX 数据源，输出运行时 Datasource 引用。",
            "数据源",
            List.of("read"),
            List.of(
                param("path", "string", false, true, "UDBX 文件路径。"),
                param("alias", "string", false, false, "数据源别名。"),
                param("read_only", "boolean", false, false, "是否只读打开，默认 true。")),
            List.of(output("datasource", "supermap.datasource", "运行时 Datasource 引用。"))));
    result.put(
        "datasource.open_postgis",
        operator(
            "datasource.open_postgis",
            "打开 PostGIS 数据源",
            "打开已由 Develop 派生连接信息的已有 PostGIS 空间表所在数据源，不创建 SuperMap 系统表。",
            "数据源",
            List.of("read"),
            List.of(
                param("connection_info", "object", false, false, "运行时派生连接信息。"),
                param("schema", "string", false, false, "运行时派生 schema。"),
                param("table", "string", false, false, "运行时派生表名。"),
                param("alias", "string", false, false, "数据源别名。"),
                param("read_only", "boolean", false, false, "是否只读打开，默认 true。")),
            List.of(output("datasource", "supermap.datasource", "运行时 Datasource 引用。"))));
    result.put(
        "datasource.enable_postgis",
        operator(
            "datasource.enable_postgis",
            "启用 PostGIS 空间工作区",
            "对已有 PostgreSQL/PostGIS 数据库执行 SuperMap SDX+ 初始化，可能创建 SuperMap 系统表。",
            "数据源",
            List.of("ddl"),
            List.of(
                param("connection_info", "object", false, true, "运行时派生连接信息。"),
                param("alias", "string", false, false, "数据源别名。")),
            List.of(output("workspace", "supermap.spatial_workspace", "SuperMap SDX+ 空间工作区摘要。")),
            List.of("direct")));
    result.put(
        "datasource.upgrade_udbx",
        operator(
            "datasource.upgrade_udbx",
            "升级 UDBX 数据源",
            "显式检查并原位升级已有 UDBX 的 SuperMap schema；只在旧 schema 时以可写方式打开。",
            "数据源",
            List.of("ddl"),
            List.of(
                param("connection_info", "object", false, true, "运行时派生连接信息。"),
                param("path", "string", false, true, "目标 UDBX 文件路径；NFS 调用使用 export 内相对路径。"),
                param("alias", "string", false, false, "数据源别名。")),
            List.of(output("upgrade", "supermap.udbx_upgrade", "UDBX schema 升级结果摘要。")),
            List.of("direct")));
    result.put(
        "datasource.create",
        operator(
            "datasource.create",
            "创建数据源",
            "创建 UDBX 输出数据源，供后续空间分析或保存算子写入。",
            "数据源",
            List.of("ddl"),
            List.of(
                param("connection_info", "object", false, true, "运行时派生连接信息。"),
                param("path", "string", false, true, "目标 UDBX 文件路径。"),
                param("alias", "string", false, false, "数据源别名。"),
                param("overwrite", "boolean", false, false, "目标文件存在时是否覆盖，默认 false。")),
            List.of(output("datasource", "supermap.datasource", "运行时 Datasource 引用。"))));
    result.put(
        "osgb_scene_to_s3m",
        operator(
            "osgb_scene_to_s3m",
            "OSGB Scene 转 S3M",
            "使用 SuperMap iObjects Java 把整套 OSGB 倾斜摄影场景转换为 S3M 数据集。",
            "三维模型",
            List.of("read", "write"),
            List.of(param("access_plan", "object", false, true, "ADDP 工作流资源访问计划。")),
            List.of(output("s3m", "supermap.s3m_dataset", "S3M 数据集发布摘要。")),
            List.of("workflow", "direct")));
    result.put(
        "cad.inspect",
        operator(
            "cad.inspect",
            "检查 CAD 图纸",
            "只读打开 DWG 或 DXF 并返回 Dataset 元数据、记录数和范围；不遍历 Geometry。",
            "CAD",
            List.of("read"),
            List.of(param("access_plan", "object", false, true, "ADDP 工作流资源访问计划。")),
            List.of(output("inspection", "addp.cad.inspect/v1", "CAD 图纸轻量检查结果。")),
            List.of("direct")));
    result.put(
        "cad.render_preview",
        operator(
            "cad.render_preview",
            "渲染 CAD 预览",
            "使用 SuperMap Map/Layer 直接渲染 DWG 或 DXF Dataset，生成受管 WebP 瓦片。",
            "CAD",
            List.of("read", "write"),
            List.of(
                param("access_plan", "object", false, true, "ADDP 工作流源文件与目标 artifact 访问计划。"),
                param("tile_size", "integer", false, false, "瓦片边长，默认 512。"),
                param("max_zoom", "integer", false, false, "最大缩放级别，默认 4，最大 8。")),
            List.of(output("preview", "addp.cad.render-preview/v1", "CAD 栅格瓦片预览产物摘要。")),
            List.of("direct")));
    result.put(
        "dataset.select",
        operator(
            "dataset.select",
            "选择矢量数据集",
            "从 Datasource 中选择 DatasetVector。",
            "数据集",
            List.of("read"),
            List.of(
                param("datasource", "supermap.datasource", true, true, "上游 Datasource 引用。"),
                param("dataset_name", "string", false, true, "数据集名称。")),
            List.of(output("dataset", "supermap.dataset", "运行时 DatasetVector 引用。"))));
    result.put(
        "dataset.info",
        operator(
            "dataset.info",
            "数据集信息",
            "读取 DatasetVector 的字段、记录数、范围和坐标系摘要。",
            "数据集",
            List.of("read"),
            List.of(param("dataset", "supermap.dataset", true, true, "输入 DatasetVector。")),
            List.of(output("info", "supermap.dataset_info", "数据集轻量信息摘要。"))));
    result.put(
        "dataset.project",
        operator(
            "dataset.project",
            "数据集投影转换",
            "把 DatasetVector 转换到目标 EPSG 坐标系，并写入目标 Datasource。",
            "数据集",
            List.of("write"),
            List.of(
                param("dataset", "supermap.dataset", true, true, "输入 DatasetVector。"),
                param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                param("target_epsg", "integer", false, true, "目标 EPSG 编码。"),
                param("method", "string", false, false, "坐标转换方法，默认 geocentric_translation。"),
                param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。")),
            List.of(output("result_dataset", "supermap.dataset", "投影转换结果 DatasetVector 引用。"))));
    result.put(
        "vector.filter",
        operator(
            "vector.filter",
            "矢量属性过滤",
            "按 SuperMap 属性过滤表达式生成新的 DatasetVector，供下游分析继续使用。",
            "空间分析",
            List.of("write"),
            List.of(
                param("dataset", "supermap.dataset", true, true, "输入 DatasetVector。"),
                param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                param("attribute_filter", "string", false, true, "SuperMap 属性过滤表达式。"),
                param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。")),
            List.of(output("result_dataset", "supermap.dataset", "过滤结果 DatasetVector 引用。"))));
    result.put(
        "vector.spatial_filter",
        operator(
            "vector.spatial_filter",
            "矢量空间筛选",
            "按两个 DatasetVector 的空间关系筛选要素，并写入目标 Datasource。",
            "空间分析",
            List.of("write"),
            List.of(
                param("input_dataset", "supermap.dataset", true, true, "被筛选 DatasetVector。"),
                param("filter_dataset", "supermap.dataset", true, true, "空间关系筛选 DatasetVector。"),
                param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                param(
                    "relation", "string", false, true, "空间关系，支持 intersect/contain/within/closest。"),
                param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。")),
            List.of(output("result_dataset", "supermap.dataset", "空间筛选结果 DatasetVector 引用。"))));
    result.put(
        "vector.buffer",
        operator(
            "vector.buffer",
            "缓冲区分析",
            "对 DatasetVector 执行缓冲区分析，并写入目标 Datasource。",
            "空间分析",
            List.of("write"),
            List.of(
                param("input_dataset", "supermap.dataset", true, true, "输入 DatasetVector。"),
                param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                param("distance", "float", false, true, "缓冲距离。"),
                param(
                    "radius_unit",
                    "string",
                    false,
                    false,
                    "距离单位，支持 meter/kilometer/foot/mile，默认 meter。"),
                param("end_type", "string", false, false, "线缓冲端点类型 round/flat，默认 round。"),
                param("semicircle_segments", "integer", false, false, "半圆弧线段数，默认 10。"),
                param("dissolve", "boolean", false, false, "是否融合缓冲结果，默认 false。"),
                param("keep_attributes", "boolean", false, false, "是否保留属性，默认 true。"),
                param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。")),
            List.of(output("result_dataset", "supermap.dataset", "缓冲区结果 DatasetVector 引用。"))));
    result.put(
        "vector.dissolve",
        operator(
            "vector.dissolve",
            "矢量融合",
            "按字段融合 DatasetVector 要素，并写入目标 Datasource。",
            "空间分析",
            List.of("write"),
            List.of(
                param("input_dataset", "supermap.dataset", true, true, "输入 DatasetVector。"),
                param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                param("field_names", "array", false, false, "融合字段数组，或以逗号分隔的字段名。"),
                param(
                    "dissolve_type",
                    "string",
                    false,
                    false,
                    "融合类型 single/multipart/only_multipart，默认 multipart。"),
                param("tolerance", "float", false, false, "融合容差，默认 0。"),
                param("save_all_fields", "boolean", false, false, "是否保留全部字段，默认 true。"),
                param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。")),
            List.of(output("result_dataset", "supermap.dataset", "融合结果 DatasetVector 引用。"))));
    result.put(
        "vector.merge",
        operator(
            "vector.merge",
            "矢量合并",
            "复制主 DatasetVector 后追加另一个 DatasetVector 的记录，生成合并结果。",
            "空间分析",
            List.of("write"),
            List.of(
                param("primary_dataset", "supermap.dataset", true, true, "主 DatasetVector。"),
                param("append_dataset", "supermap.dataset", true, true, "追加 DatasetVector。"),
                param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。")),
            List.of(output("result_dataset", "supermap.dataset", "合并结果 DatasetVector 引用。"))));
    result.put(
        "vector.feature_envelope",
        operator(
            "vector.feature_envelope",
            "要素外接矩形",
            "为 DatasetVector 中每个要素生成外接矩形数据集。",
            "空间分析",
            List.of("write"),
            List.of(
                param("input_dataset", "supermap.dataset", true, true, "输入 DatasetVector。"),
                param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。")),
            List.of(output("result_dataset", "supermap.dataset", "外接矩形结果 DatasetVector 引用。"))));
    result.put(
        "vector.inner_point",
        operator(
            "vector.inner_point",
            "面内点提取",
            "从面 DatasetVector 生成内部点 DatasetVector。",
            "空间分析",
            List.of("write"),
            List.of(
                param("input_dataset", "supermap.dataset", true, true, "输入面 DatasetVector。"),
                param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。")),
            List.of(output("result_dataset", "supermap.dataset", "面内点结果 DatasetVector 引用。"))));
    result.put(
        "overlay.intersect",
        operator(
            "overlay.intersect",
            "叠加求交",
            "对两个 DatasetVector 执行 OverlayAnalyst.intersect，并写入目标 Datasource。",
            "空间分析",
            List.of("write"),
            List.of(
                param("input_dataset", "supermap.dataset", true, true, "源 DatasetVector。"),
                param("overlay_dataset", "supermap.dataset", true, true, "叠加 DatasetVector。"),
                param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
                param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。"),
                param("tolerance", "float", false, false, "叠加容差，默认 0。")),
            List.of(output("result_dataset", "supermap.dataset", "叠加分析结果 DatasetVector 引用。"))));
    result.put(
        "overlay.clip",
        operator(
            "overlay.clip",
            "叠加裁剪",
            "对两个 DatasetVector 执行 OverlayAnalyst.clip，并写入目标 Datasource。",
            "空间分析",
            List.of("write"),
            overlayParameters(),
            List.of(output("result_dataset", "supermap.dataset", "裁剪结果 DatasetVector 引用。"))));
    result.put(
        "overlay.erase",
        operator(
            "overlay.erase",
            "叠加擦除",
            "对两个 DatasetVector 执行 OverlayAnalyst.erase，并写入目标 Datasource。",
            "空间分析",
            List.of("write"),
            overlayParameters(),
            List.of(output("result_dataset", "supermap.dataset", "擦除结果 DatasetVector 引用。"))));
    result.put(
        "overlay.union",
        operator(
            "overlay.union",
            "叠加合并",
            "对两个 DatasetVector 执行 OverlayAnalyst.union，并写入目标 Datasource。",
            "空间分析",
            List.of("write"),
            overlayParameters(),
            List.of(output("result_dataset", "supermap.dataset", "合并结果 DatasetVector 引用。"))));
    result.put(
        "vector.query",
        operator(
            "vector.query",
            "矢量属性查询",
            "对 DatasetVector 执行属性过滤并返回轻量查询摘要。",
            "空间分析",
            List.of("read"),
            List.of(
                param("dataset", "supermap.dataset", true, true, "输入 DatasetVector。"),
                param("attribute_filter", "string", false, false, "SuperMap 属性过滤表达式。"),
                param("max_records", "integer", false, false, "预留参数，当前仅返回总数。")),
            List.of(output("query_result", "supermap.query_result", "查询结果摘要。"))));
    result.put(
        "dataset.save",
        operator(
            "dataset.save",
            "保存数据集",
            "把上游 DatasetVector 复制保存到目标 Datasource。",
            "数据集",
            List.of("write"),
            List.of(
                param("dataset", "supermap.dataset", true, true, "输入 DatasetVector。"),
                param("target_datasource", "supermap.datasource", true, true, "目标 Datasource。"),
                param("output_dataset_name", "string", false, true, "输出数据集名称。"),
                param("overwrite", "boolean", false, false, "目标数据集存在时是否覆盖，默认 false。")),
            List.of(output("saved_dataset", "supermap.dataset", "保存后的 DatasetVector 引用。"))));
    return result;
  }

  private static ObjectNode operator(
      String id,
      String displayName,
      String description,
      String category,
      List<String> effects,
      List<ObjectNode> parameters,
      List<ObjectNode> outputPorts) {
    return operator(
        id,
        displayName,
        description,
        category,
        effects,
        parameters,
        outputPorts,
        List.of("workflow"));
  }

  private static ObjectNode operator(
      String id,
      String displayName,
      String description,
      String category,
      List<String> effects,
      List<ObjectNode> parameters,
      List<ObjectNode> outputPorts,
      List<String> executionModes) {
    ObjectNode op = MAPPER.createObjectNode();
    op.put("id", id);
    op.put("name", id);
    op.put("display_name", displayName);
    op.put("engine_type", ENGINE_TYPE);
    op.put("category", category);
    ArrayNode categoryPath = op.putArray("category_path");
    categoryPath.add(category);
    op.put("description", description);
    op.put("brief_description", displayName);
    ArrayNode modes = op.putArray("execution_modes");
    executionModes.forEach(modes::add);
    ArrayNode operatorEffects = op.putArray("effects");
    effects.forEach(operatorEffects::add);
    ArrayNode params = op.putArray("parameters");
    parameters.forEach(params::add);
    ArrayNode outputs = op.putArray("output_ports");
    outputPorts.forEach(outputs::add);
    return op;
  }

  private static ObjectNode param(
      String name, String type, boolean workflowInput, boolean required, String description) {
    ObjectNode parameter = MAPPER.createObjectNode();
    parameter.put("name", name);
    parameter.put("type", type);
    parameter.put("param_type", workflowInput ? "input" : "param");
    parameter.put("required", required);
    parameter.put("description", description);
    return parameter;
  }

  private static ObjectNode output(String name, String type, String description) {
    ObjectNode output = MAPPER.createObjectNode();
    output.put("name", name);
    output.put("type", type);
    output.put("description", description);
    output.put("is_default", true);
    return output;
  }

  private static List<ObjectNode> overlayParameters() {
    return List.of(
        param("input_dataset", "supermap.dataset", true, true, "源 DatasetVector。"),
        param("overlay_dataset", "supermap.dataset", true, true, "叠加 DatasetVector。"),
        param("output_datasource", "supermap.datasource", true, true, "输出 Datasource。"),
        param("output_dataset_name", "string", false, true, "输出数据集名称。"),
        param("overwrite", "boolean", false, false, "输出数据集存在时是否覆盖，默认 false。"),
        param("tolerance", "float", false, false, "叠加容差，默认 0。"));
  }
}
