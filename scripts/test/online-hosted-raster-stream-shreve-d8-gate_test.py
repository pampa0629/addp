import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedStreamShreveD8GateTest(base.HostedContourGateTest):
    prefix='stream-shreve-d8'
