import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedWatershedD8GateTest(base.HostedContourGateTest):
    prefix='watershed-d8'
