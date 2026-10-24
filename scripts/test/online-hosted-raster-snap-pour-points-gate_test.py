import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedSnapPourPointsGateTest(base.HostedContourGateTest):
    prefix='snap-pour-points'
