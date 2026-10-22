import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedFlowLengthD8GateTest(base.HostedContourGateTest):
    prefix='flow-length-d8'
