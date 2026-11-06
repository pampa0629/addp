import importlib

surface = importlib.import_module('scripts.test.online-hosted-raster-surface-area-gate_test')


class HostedLineOfSightGateTest(surface.HostedSurfaceAreaGateTest):
    prefix = 'line-of-sight'
