import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedZonalGateTest(base.HostedContourGateTest):
    prefix='zonal-statistics'
