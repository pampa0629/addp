import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedWeightedAccumulationD8GateTest(base.HostedContourGateTest):
    prefix='flow-accumulation-weighted-d8'
