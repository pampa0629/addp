import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedStreamToVectorD8GateTest(base.HostedContourGateTest):
    prefix='stream-to-vector-d8'
