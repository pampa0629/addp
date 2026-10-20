import importlib
base=importlib.import_module('scripts.test.online-raster-polygonize-fixture_test')


class FocalModeFixtureTest(base.PolygonizeFixtureTest):
    fixture=importlib.import_module('business.scripts.online-raster-focal-mode-fixture')
