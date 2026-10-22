import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedDrainageBasinsD8GateTest(base.HostedContourGateTest):
    prefix='drainage-basins-d8'
