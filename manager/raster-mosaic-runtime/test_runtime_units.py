import unittest

import numpy as np

import runtime.overview_renderer as overview_renderer
from runtime.gdal_env import join_gdal_path
from runtime.overview_renderer import RasterMosaicRuntimeError, _array_to_image, _output_format
from runtime.overview_renderer import _display_range_from_samples, _effective_dataset_projection, _extent_intersects, _leaf_extent_3857, _resolution_is_sufficient, _scale_to_uint8, _tile_resolution_3857
from runtime.tile_math import (
    WEB_MERCATOR_HALF_WORLD,
    clamp_tile_size,
    int_param,
    web_mercator_tile_bounds,
)


class _FakeBand:
    def __init__(self, nodata):
        self._nodata = nodata

    def GetNoDataValue(self):
        return self._nodata


class _FakeDataset:
    def __init__(self, nodata=None, resolution=0, projection="", transform=None, width=10, height=10):
        self._nodata = nodata
        self._resolution = resolution
        self._projection = projection
        self._transform = transform
        self.RasterXSize = width
        self.RasterYSize = height

    def GetRasterBand(self, _index):
        return _FakeBand(self._nodata)

    def GetProjection(self):
        return self._projection

    def GetProjectionRef(self):
        return self._projection

    def GetGeoTransform(self):
        return self._transform


class _IdentityTransform:
    def TransformPoint(self, x, y):
        return (x, y, 0)


class RuntimeUnitTests(unittest.TestCase):
    def test_web_mercator_tile_bounds(self):
        self.assertEqual(
            web_mercator_tile_bounds(0, 0, 0),
            (
                -WEB_MERCATOR_HALF_WORLD,
                -WEB_MERCATOR_HALF_WORLD,
                WEB_MERCATOR_HALF_WORLD,
                WEB_MERCATOR_HALF_WORLD,
            ),
        )
        minx, miny, maxx, maxy = web_mercator_tile_bounds(1, 1, 1)
        self.assertAlmostEqual(minx, 0)
        self.assertAlmostEqual(maxy, 0)
        self.assertAlmostEqual(maxx, WEB_MERCATOR_HALF_WORLD)
        self.assertAlmostEqual(miny, -WEB_MERCATOR_HALF_WORLD)

    def test_rejects_invalid_tile_inputs(self):
        with self.assertRaises(ValueError):
            web_mercator_tile_bounds(-1, 0, 0)
        with self.assertRaises(ValueError):
            web_mercator_tile_bounds(1, 2, 0)
        with self.assertRaises(ValueError):
            int_param("abc", "tile.z")
        with self.assertRaises(ValueError):
            int_param(0, "tile.z", minimum=1)
        with self.assertRaises(ValueError):
            clamp_tile_size(1024)

    def test_join_gdal_path_normalizes_slashes(self):
        self.assertEqual(join_gdal_path("/vsis3/bucket/root/", "/overviews/overview.cog.tif"), "/vsis3/bucket/root/overviews/overview.cog.tif")
        self.assertEqual(join_gdal_path("", "mosaic.addp.json"), "mosaic.addp.json")
        self.assertEqual(join_gdal_path("/vsis3/bucket/root", ""), "/vsis3/bucket/root")

    def test_output_format_contract(self):
        self.assertEqual(_output_format(None), "webp")
        self.assertEqual(_output_format("jpg"), "jpeg")
        self.assertEqual(_output_format("PNG"), "png")
        with self.assertRaises(RasterMosaicRuntimeError):
            _output_format("gif")

    def test_array_to_image_applies_single_band_nodata_alpha(self):
        values = np.array([[0, 5], [10, 15]], dtype=np.float32)
        image = _array_to_image(values, _FakeDataset(nodata=0))
        rgba = np.asarray(image)
        self.assertEqual(image.mode, "RGBA")
        self.assertEqual(tuple(rgba[0, 0]), (0, 0, 0, 0))
        self.assertEqual(rgba[1, 1, 3], 255)

    def test_scale_to_uint8_uses_shared_display_range(self):
        left = _scale_to_uint8(np.array([0, 50], dtype=np.float32), (0, 100))
        right = _scale_to_uint8(np.array([50, 100], dtype=np.float32), (0, 100))

        self.assertEqual(int(left[1]), int(right[0]))
        self.assertEqual(int(left[0]), 0)
        self.assertEqual(int(right[1]), 255)

    def test_scale_to_uint8_gamma_brightens_low_values_without_changing_endpoints(self):
        linear = _scale_to_uint8(np.array([0, 25, 100], dtype=np.float32), (0, 100), 1.0)
        curved = _scale_to_uint8(np.array([0, 25, 100], dtype=np.float32), (0, 100), 0.6)

        self.assertEqual(int(curved[0]), 0)
        self.assertEqual(int(curved[2]), 255)
        self.assertGreater(int(curved[1]), int(linear[1]))

    def test_display_range_from_samples_uses_percentiles_and_nodata(self):
        values = np.array([-32768, -10, 0, 10, 20, 30, 40, 1000], dtype=np.float32)
        display_range = _display_range_from_samples(values, -32768)

        self.assertGreater(display_range[0], -10)
        self.assertLess(display_range[1], 1000)

    def test_overview_resolution_decides_leaf_switch_without_zoom_threshold(self):
        bounds = (0.0, 0.0, 256.0, 256.0)
        self.assertEqual(_tile_resolution_3857(bounds, 256), 1.0)
        self.assertTrue(_resolution_is_sufficient(1.4, 1.0, 1.5))
        self.assertFalse(_resolution_is_sufficient(1.6, 1.0, 1.5))

    def test_extent_intersection(self):
        self.assertTrue(_extent_intersects((0, 0, 10, 10), (5, 5, 15, 15)))
        self.assertFalse(_extent_intersects((0, 0, 10, 10), (10, 10, 15, 15)))

    def test_leaf_extent_uses_index_extent_and_crs(self):
        original = overview_renderer._coordinate_transform
        try:
            overview_renderer._coordinate_transform = lambda source, target: _IdentityTransform() if source == "EPSG:3857" and target == "EPSG:3857" else None
            self.assertEqual(_leaf_extent_3857({"extent": [0, 1, 2, 3], "source_crs": "EPSG:3857"}), (0, 1, 2, 3))
            self.assertIsNone(_leaf_extent_3857({"extent": [0, 1, 2, 3], "source_crs": "EPSG:4326"}))
        finally:
            overview_renderer._coordinate_transform = original

    def test_leaf_extent_requires_valid_extent_and_crs(self):
        self.assertIsNone(_leaf_extent_3857({"extent": [1000, 1000, 2000, 2000]}))
        self.assertIsNone(_leaf_extent_3857({"extent": [1, 1, 0, 0], "source_crs": "EPSG:4326"}))

    def test_effective_dataset_projection_infers_wgs84_from_geographic_extent(self):
        dataset = _FakeDataset(projection="", transform=(82.0, 0.1, 0.0, 28.0, 0.0, -0.1), width=10, height=10)
        self.assertEqual(_effective_dataset_projection(dataset), "EPSG:4326")

    def test_effective_dataset_projection_keeps_explicit_projection(self):
        dataset = _FakeDataset(projection="EPSG:3857", transform=(82.0, 0.1, 0.0, 28.0, 0.0, -0.1), width=10, height=10)
        self.assertEqual(_effective_dataset_projection(dataset), "EPSG:3857")


if __name__ == "__main__":
    unittest.main()
