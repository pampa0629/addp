import importlib
base=importlib.import_module('scripts.test.online-raster-polygonize-fixture_test')


class ThinFixtureTest(base.PolygonizeFixtureTest):
    fixture=importlib.import_module('business.scripts.online-raster-thin-fixture')
