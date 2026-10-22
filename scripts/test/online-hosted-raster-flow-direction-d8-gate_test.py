import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedFlowDirectionD8GateTest(base.HostedContourGateTest):
    prefix='flow-direction-d8'
