import os
import sys
import tempfile
import unittest
from io import BytesIO
from pathlib import Path

import numpy as np
from PIL import Image
from osgeo import gdal, gdal_array, osr

from runtime.overview_renderer import render_mosaic_tile
from runtime.tile_math import WEB_MERCATOR_HALF_WORLD


class NativeGDALTests(unittest.TestCase):
    def setUp(self):
        gdal.UseExceptions()
        osr.UseExceptions()

    def test_isolated_matching_native_binding(self):
        self.assertIn("include-system-site-packages = false", Path(sys.prefix, "pyvenv.cfg").read_text())
        self.assertTrue(Path(gdal.__file__).resolve().is_relative_to(Path(sys.prefix).resolve()))
        self.assertEqual(gdal.VersionInfo("RELEASE_NAME"), os.environ["ADDP_GDAL_VERSION"])
        self.assertEqual(osr.SpatialReference().ImportFromEPSG(4326), 0)

    def test_cog_numpy_and_real_tile_render(self):
        with tempfile.TemporaryDirectory(prefix="addp-raster-native-") as directory:
            source = gdal.GetDriverByName("MEM").Create("", 256, 256, 1, gdal.GDT_Float32)
            self.assertIsNotNone(source)
            world = WEB_MERCATOR_HALF_WORLD
            source.SetGeoTransform((-world, 2 * world / 256, 0, world, 0, -2 * world / 256))
            projection = osr.SpatialReference()
            projection.ImportFromEPSG(3857)
            source.SetProjection(projection.ExportToWkt())
            values = np.full((256, 256), 80, dtype=np.float32)
            values[:64, :] = 0
            values[-64:, :] = 100
            source.GetRasterBand(1).WriteArray(values)
            path = str(Path(directory, "overview.tif"))
            cog = gdal.GetDriverByName("COG").CreateCopy(path, source)
            self.assertIsNotNone(cog)
            cog = source = None
            reopened = gdal.Open(path)
            self.assertEqual(reopened.GetDriver().ShortName, "GTiff")
            self.assertEqual(reopened.GetMetadata("IMAGE_STRUCTURE").get("LAYOUT"), "COG")
            np.testing.assert_array_equal(reopened.ReadAsArray(), values)
            reopened = None
            tile = render_mosaic_tile({
                "dataset": {"dataset_root_uri": directory, "overview_ref": "overview.tif"},
                "tile": {"z": 0, "x": 0, "y": 0, "tile_size": 256},
                "render": {"format": "png", "display_min": 0, "display_max": 100, "gamma": 1},
            })
            self.assertEqual(tile.content_type, "image/png")
            self.assertEqual(tile.source, "overview")
            image = Image.open(BytesIO(tile.data)).convert("RGBA")
            self.assertEqual(image.size, (256, 256))
            self.assertEqual(image.getpixel((128, 128)), (204, 204, 204, 255))
