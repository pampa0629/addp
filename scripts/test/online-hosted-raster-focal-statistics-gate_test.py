import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedFocalGateTest(base.HostedContourGateTest):
    prefix='focal-statistics'
