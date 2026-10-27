import importlib
base=importlib.import_module('scripts.test.online-raster-profile-curvature-fixture_test')

class CutFillFixtureTest(base.ProfileCurvatureFixtureTest):
    fixture=importlib.import_module('business.scripts.online-raster-cut-fill-fixture')
