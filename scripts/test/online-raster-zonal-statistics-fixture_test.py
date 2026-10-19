import importlib
base=importlib.import_module('scripts.test.online-raster-polygonize-fixture_test')


class ZonalFixtureTest(base.PolygonizeFixtureTest):
    fixture=importlib.import_module('business.scripts.online-raster-zonal-statistics-fixture')
