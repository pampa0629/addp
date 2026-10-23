import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedStreamOrderD8GateTest(base.HostedContourGateTest):
    prefix='stream-order-d8'
