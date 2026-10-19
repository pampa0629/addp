import importlib
base=importlib.import_module('scripts.test.online-raster-polygonize-fixture_test')


class VectorZonalFixtureTest(base.PolygonizeFixtureTest):
    fixture=importlib.import_module('business.scripts.online-vector-zonal-statistics-fixture')
