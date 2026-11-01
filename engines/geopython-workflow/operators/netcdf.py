"""Bounded classic NetCDF inspection and raw numeric windows, solely via GDAL."""
from __future__ import annotations

import hashlib
import json
import math
from pathlib import Path
import sys
import tempfile
from contextlib import contextmanager

import numpy as np
from osgeo import gdal, gdal_array, osr
from addp_common.workflow_access import require_source_plan, stage_source_file
from . import _native_process
from .base import OperatorCategory, OperatorMetadata, OperatorParam, OperatorType, register_operator

SCHEMA = "gdal.netcdf.inspect/v1"
SLICE_SCHEMA = "gdal.netcdf.slice/v1"
MAX_BYTES = 128 * 1024 * 1024
MAX_VARIABLES = 256
MAX_DIMENSIONS = 8
MAX_ATTRIBUTE_ELEMENTS = 4096
MAX_ATTRIBUTE_BYTES = 65536
MAX_CELLS = 4096
MAX_SIDE = 128


class SliceSelectionError(ValueError):
    pass


class SourceChanged(ValueError):
    pass


def _integer(value, name, low, high):
    if isinstance(value, bool) or not isinstance(value, int) or not low <= value <= high:
        raise SliceSelectionError(f"{name} outside supported bounds")
    return value


@contextmanager
def _source(access_plan, source_version=None):
    plan = require_source_plan({"access_plan": access_plan})
    if plan["source"].get("kind") != "file" or plan["source"].get("format") != "netcdf":
        raise ValueError("NetCDF requires an exact single file source plan")
    with tempfile.TemporaryDirectory(prefix="addp-netcdf-") as work:
        staged = stage_source_file(plan, Path(work), max_bytes=MAX_BYTES)
        path = Path(work) / "input.nc"
        staged.rename(path)
        with path.open("rb") as stream:
            signature = stream.read(4)
        if signature not in (b"CDF\x01", b"CDF\x02", b"CDF\x05"):
            raise ValueError("only classic CDF-1/2/5 is supported; NetCDF-4/HDF5 is not admitted")
        with path.open("rb") as stream:
            version = "sha256:" + hashlib.file_digest(stream, "sha256").hexdigest()
        if source_version is not None and version != source_version:
            raise SourceChanged("NetCDF source changed; refresh metadata")
        with gdal.config_options({"GDAL_PAM_ENABLED": "NO", "GDAL_DISABLE_READDIR_ON_OPEN": "EMPTY_DIR", "GDAL_NETCDF_ASSUME_LONGLAT": "NO"}):
            dataset = gdal.OpenEx(f'NETCDF:"{path}"', gdal.OF_MULTIDIM_RASTER | gdal.OF_READONLY, allowed_drivers=["netCDF"])
            if dataset is None:
                raise ValueError("invalid classic NetCDF")
            try:
                yield dataset, version, signature[-1]
            finally:
                dataset = None


def _numeric_text(value):
    if isinstance(value, (int, np.integer)):
        return str(int(value))
    number = float(value)
    if math.isnan(number):
        return "NaN"
    if math.isinf(number):
        return "Infinity" if number > 0 else "-Infinity"
    return repr(number)


def _type_name(kind):
    if kind.GetClass() == gdal.GEDTC_NUMERIC:
        return gdal.GetDataTypeName(kind.GetNumericDataType())
    return kind.GetName() or "String"


def _attributes(owner, selected=None):
    result = {}
    for attribute in owner.GetAttributes(["SHOW_ALL=YES"]) or []:
        if selected is not None and attribute.GetName() not in selected:
            continue
        if len(result) >= 256 or attribute.GetTotalElementsCount() > MAX_ATTRIBUTE_ELEMENTS:
            raise ValueError("NetCDF attributes exceed inspection budget")
        kind = attribute.GetDataType()
        if kind.GetClass() == gdal.GEDTC_STRING:
            values = attribute.ReadAsStringArray()
        elif kind.GetClass() == gdal.GEDTC_NUMERIC:
            native = kind.GetNumericDataType()
            numeric_type = gdal_array.GDALTypeCodeToNumericTypeCode(native)
            if numeric_type is None or gdal.DataTypeIsComplex(native):
                raise ValueError("unsupported NetCDF numeric attribute type")
            dtype = np.dtype(numeric_type)
            raw = attribute.ReadAsRaw()
            if raw is None or len(raw) != attribute.GetTotalElementsCount() * dtype.itemsize:
                raise ValueError("incomplete NetCDF numeric attribute storage")
            values = [_numeric_text(v) for v in np.frombuffer(raw, dtype=dtype)]
        else:
            raise ValueError("NetCDF compound attribute types are unsupported")
        result[attribute.GetName()] = {"type": _type_name(kind), "values": list(values)}
        if len(json.dumps(result, ensure_ascii=False).encode()) > MAX_ATTRIBUTE_BYTES:
            raise ValueError("NetCDF attribute bytes exceed inspection budget")
    return result


def _description(array, light=False):
    dimensions = array.GetDimensions()
    if len(dimensions) > MAX_DIMENSIONS:
        raise ValueError("NetCDF dimension count exceeds budget")
    kind = array.GetDataType()
    numeric = kind.GetClass() == gdal.GEDTC_NUMERIC and not gdal.DataTypeIsComplex(kind.GetNumericDataType())
    attributes = _attributes(array, {"units", "calendar", "standard_name", "long_name", "axis", "grid_mapping", "scale_factor", "add_offset", "_FillValue", "missing_value", "valid_min", "valid_max", "valid_range", "_Unsigned"} if light else None)
    unsigned = attributes.get("_Unsigned", {}).get("values", [])
    numeric = numeric and not (len(unsigned) == 1 and str(unsigned[0]).strip().lower() == "true")
    return {
        "name": array.GetFullName(),
        "type": _type_name(kind),
        "dimensions": [{"name": d.GetFullName(), "size": str(d.GetSize()), "axis_type": d.GetType(), "direction": d.GetDirection()} for d in dimensions],
        "attributes": attributes,
        "attributes_coverage": "selected" if light else "complete",
        "slice_supported": bool(numeric and len(dimensions) >= 2 and all(d.GetSize() > 0 for d in dimensions)),
    }


def _catalog(dataset):
    group = dataset.GetRootGroup()
    if group.GetGroupNames():
        raise ValueError("classic NetCDF must not contain nested groups")
    names = group.GetMDArrayNames(["SHOW_ALL=YES"]) or []
    if len(names) > MAX_VARIABLES:
        raise ValueError("NetCDF variable count exceeds inspection budget")
    variables = []
    for name in names:
        array = group.OpenMDArray(name)
        if array is None:
            raise ValueError("NetCDF variable cannot be opened")
        variables.append(_description(array, light=True))
    dimensions = [{"name": d.GetFullName(), "size": str(d.GetSize()), "axis_type": d.GetType(), "direction": d.GetDirection()} for d in group.GetDimensions()]
    if len(dimensions) > 256:
        raise ValueError("NetCDF dimensions exceed inspection budget")
    if len(json.dumps(variables, ensure_ascii=False).encode()) > 256 * 1024:
        raise ValueError("NetCDF variable index exceeds byte budget")
    return group, variables, dimensions


def inspect(access_plan):
    with _source(access_plan) as (dataset, version, encoding):
        group, variables, dimensions = _catalog(dataset)
        children = [{"name": v["name"], "child_kind": "array", "data_type": "media" if v["slice_supported"] else "unknown", "format": "netcdf", "native": v} for v in variables]
        return {"schema_version": SCHEMA, "format": "netcdf", "container": {"child_count": len(children), "resource_count": 1, "children": children},
                "format_info": {"driver": "netCDF", "encoding": f"CDF-{encoding}", "scan_complete": True, "source_version": version, "dimensions": dimensions, "attributes": _attributes(group), "value_mode": "raw_storage"}}


def read_slice(access_plan, source_version, selection):
    if not isinstance(source_version, str) or len(source_version) != 71 or not source_version.startswith("sha256:"):
        raise ValueError("NetCDF preview requires the scanned source version")
    if not isinstance(selection, dict) or set(selection) != {"variable", "indices", "x", "y", "width", "height"}:
        raise SliceSelectionError("NetCDF slice requires exact variable, indices and window")
    with _source(access_plan, source_version) as (dataset, version, _):
        group, variables, _ = _catalog(dataset)
        matches = [v for v in variables if v["name"] == selection["variable"]]
        if len(matches) != 1 or not matches[0]["slice_supported"]:
            raise SliceSelectionError("NetCDF variable has no supported numeric two-dimensional slice")
        description = matches[0]
        array = group.OpenMDArrayFromFullname(description["name"])
        description = _description(array)
        dimensions = array.GetDimensions()
        sizes = [d.GetSize() for d in dimensions]
        indices = selection["indices"]
        if not isinstance(indices, list) or len(indices) != len(sizes) - 2:
            raise SliceSelectionError("every leading dimension requires one explicit index")
        indices = [_integer(i, "dimension index", 0, size - 1) for i, size in zip(indices, sizes)]
        x = _integer(selection["x"], "x", 0, sizes[-1] - 1)
        y = _integer(selection["y"], "y", 0, sizes[-2] - 1)
        width = _integer(selection["width"], "width", 1, min(MAX_SIDE, sizes[-1] - x))
        height = _integer(selection["height"], "height", 1, min(MAX_SIDE, sizes[-2] - y))
        if width * height > MAX_CELLS:
            raise SliceSelectionError("NetCDF window exceeds cell budget")
        starts, counts = indices + [y, x], [1] * len(indices) + [height, width]
        values = array.ReadAsArray(array_start_idx=starts, count=counts)
        mask = array.GetMask().ReadAsArray(array_start_idx=starts, count=counts)
        if values is None or mask is None or values.size != width * height or mask.size != width * height:
            raise ValueError("NetCDF returned an incomplete slice")
        values, mask = values.reshape(height, width), mask.reshape(height, width)
        return {"schema_version": SLICE_SCHEMA, "format": "netcdf", "source_version": version, "selection": selection,
                "variable": description, "values": [[_numeric_text(v) for v in row] for row in values], "valid": (mask != 0).tolist(),
                "value_mode": "raw_storage", "spatial": _spatial(group, array, dimensions, x, y, width, height)}


def _spatial(group, array, dimensions, x, y, width, height):
    # The declared mapping must exist in this exact file; never infer WGS84
    # from coordinate names or let PAM/sidecars supply a projection.
    mapping = _attributes(array).get("grid_mapping", {}).get("values", [])
    if len(mapping) != 1 or not isinstance(mapping[0], str):
        return None
    name = mapping[0]
    if not name or name not in (group.GetMDArrayNames(["SHOW_ALL=YES"]) or []):
        return None
    declaration = group.OpenMDArray(name)
    attrs = _attributes(declaration)
    wkts = [attrs[k]["values"][0] for k in ("crs_wkt", "spatial_ref") if k in attrs and len(attrs[k]["values"]) == 1]
    if not wkts or any(not isinstance(w, str) for w in wkts):
        return None
    source = osr.SpatialReference()
    try:
        source.ImportFromWkt(wkts[0])
        for wkt in wkts[1:]:
            other = osr.SpatialReference(); other.ImportFromWkt(wkt)
            if not source.IsSame(other):
                return None
        source.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
        coordinates = []
        for dimension in dimensions[-2:]:
            axis = dimension.GetIndexingVariable()
            if axis is None or axis.GetDimensionCount() != 1 or dimension.GetSize() > 4096 or dimension.GetSize() < 2:
                return None
            # Only a declared horizontal axis participates in georeferencing.
            if dimension.GetType() not in ("HORIZONTAL_X", "HORIZONTAL_Y"):
                return None
            axis_attrs = _attributes(axis)
            # Coordinates must already be in the declared CRS unit. Do not
            # infer unit conversion or apply packed coordinate values silently.
            unit = axis_attrs.get("units", {}).get("values", [])
            expected_units = {"m", "metre", "meter", "metres", "meters"} if source.IsProjected() and source.GetLinearUnits() == 1.0 else set()
            if source.IsGeographic() and math.isclose(source.GetAngularUnits(), math.pi / 180, rel_tol=0, abs_tol=1e-15):
                expected_units = {"degrees_east", "degree_east", "degrees_E", "degree_E"} if dimension.GetType() == "HORIZONTAL_X" else {"degrees_north", "degree_north", "degrees_N", "degree_N"}
            if len(unit) != 1 or unit[0] not in expected_units or any(k in axis_attrs for k in ("scale_factor", "add_offset", "_Unsigned")):
                return None
            mask = axis.GetMask().ReadAsArray()
            if mask is None or not np.all(mask):
                return None
            raw = axis.ReadAsArray()
            if raw is None or not np.issubdtype(raw.dtype, np.number):
                return None
            values = np.asarray(raw, dtype=np.float64).reshape(-1)
            step = values[1] - values[0]
            if not np.all(np.isfinite(values)) or not math.isfinite(step) or step == 0 or not np.array_equal(values, values[0] + step * np.arange(values.size)):
                return None
            coordinates.append((float(values[0]), float(step)))
        if dimensions[-2].GetType() != "HORIZONTAL_Y" or dimensions[-1].GetType() != "HORIZONTAL_X":
            return None
        (y0, dy), (x0, dx) = coordinates
        transform = [x0 + (x - .5)*dx, dx, 0., y0 + (y - .5)*dy, 0., dy]
        corners = [[transform[0] + ix*dx, transform[3] + iy*dy] for ix, iy in ((0,0),(width,0),(width,height),(0,height),(0,0))]
        geographic = osr.SpatialReference(); geographic.ImportFromEPSG(4326)
        geographic.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
        conversion = osr.CoordinateTransformation(source, geographic)
        lonlat = [list(conversion.TransformPoint(*point)[:2]) for point in corners]
        if not all(math.isfinite(v) for point in lonlat for v in point) or not all(-180 <= a <= 180 and -90 <= b <= 90 for a,b in lonlat):
            return None
        return {"crs_wkt": source.ExportToWkt(), "transform": transform,
                "footprint": {"type": "Feature", "properties": {}, "geometry": {"type": "Polygon", "coordinates": [lonlat]}}}
    except RuntimeError:
        # Unsupported coordinates do not invalidate the separately preserved raw grid.
        return None


def _native_main():
    request = json.load(sys.stdin)
    try:
        operation = {"inspect": inspect, "read_slice": read_slice}[request["operation"]]
        response = {"result": operation(**request["params"])}
    except SliceSelectionError:
        response = {"error_code": "NETCDF_SLICE_INVALID"}
    except SourceChanged:
        response = {"error_code": "NETCDF_SOURCE_CHANGED"}
    except Exception as exc:
        response = {"error": str(exc)}
    print(json.dumps(response, ensure_ascii=False, allow_nan=False))


def _register(name, function, params):
    metadata = OperatorMetadata(name=name, type=OperatorType.GENERAL, category=OperatorCategory.COMMON_IO,
        description=name, brief_description="NetCDF classic single-file read", execution_modes=["direct"], effects=["read"],
        params=[OperatorParam(name="access_plan", type="param", data_type="object", description="Authorized source-only plan", required=True), *params],
        overview="受控经典NetCDF单文件只读", use_cases=["变量和维度查看", "有界原始值切片"],
        notes=["CDF-1/2/5 only", "No implicit coordinate system or time conversion"], workflow_example={})
    def invoke(**kwargs):
        completed = _native_process.run([sys.executable, "-c", "from operators.netcdf import _native_main; _native_main()"],
            input=json.dumps({"operation": function.__name__, "params": kwargs}, allow_nan=False), capture_output=True, text=True, timeout=120)
        if completed.returncode != 0:
            raise RuntimeError("NetCDF native process failed")
        response = json.loads(completed.stdout)
        if response.get("error_code") in {"NETCDF_SOURCE_CHANGED", "NETCDF_SLICE_INVALID"}:
            return {"schema_version": SLICE_SCHEMA, "error_code": response["error_code"]}
        if "error" in response:
            raise ValueError(response["error"])
        return response["result"]
    return register_operator(metadata, invoke)


OPERATORS = dict([
    _register("netcdf.inspect", inspect, []),
    _register("netcdf.read_slice", read_slice, [
        OperatorParam(name="source_version", type="param", data_type="string", description="Scanned source content SHA256", required=True),
        OperatorParam(name="selection", type="param", data_type="object", description="Variable, leading indices and bounded XY window", required=True),
    ]),
])
