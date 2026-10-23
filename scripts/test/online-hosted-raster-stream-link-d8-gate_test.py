import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedStreamLinkD8GateTest(base.HostedContourGateTest):
    prefix='stream-link-d8'
