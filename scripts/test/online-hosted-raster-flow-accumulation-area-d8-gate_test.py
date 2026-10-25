import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedFlowAccumulationAreaD8GateTest(base.HostedContourGateTest):
    prefix='flow-accumulation-area-d8'
