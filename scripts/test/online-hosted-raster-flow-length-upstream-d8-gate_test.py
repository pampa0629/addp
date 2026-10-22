import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedFlowLengthUpstreamD8GateTest(base.HostedContourGateTest):
    prefix='flow-length-upstream-d8'
