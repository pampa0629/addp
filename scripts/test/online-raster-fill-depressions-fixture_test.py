import importlib
base=importlib.import_module('scripts.test.online-raster-polygonize-fixture_test')


class FillDepressionsFixtureTest(base.PolygonizeFixtureTest):
    fixture=importlib.import_module('business.scripts.online-raster-fill-depressions-fixture')
