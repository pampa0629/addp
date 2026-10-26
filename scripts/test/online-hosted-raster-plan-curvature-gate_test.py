import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedPlanCurvatureGateTest(base.HostedContourGateTest):
    prefix='plan-curvature'
