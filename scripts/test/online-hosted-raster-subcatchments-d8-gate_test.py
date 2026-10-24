import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedSubcatchmentsD8GateTest(base.HostedContourGateTest):
    prefix='subcatchments-d8'
