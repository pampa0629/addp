import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedDistanceToStreamD8GateTest(base.HostedContourGateTest):
    prefix='hand-d8'
