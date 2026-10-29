import importlib
base=importlib.import_module('scripts.test.online-raster-profile-curvature-fixture_test')

class SurfaceAreaFixtureTest(base.ProfileCurvatureFixtureTest):
    fixture=importlib.import_module('business.scripts.online-raster-surface-area-fixture')
