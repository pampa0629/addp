import importlib

base=importlib.import_module('scripts.test.online-hosted-raster-line-of-sight-gate_test')

class HostedViewshedGateTest(base.HostedLineOfSightGateTest):
    prefix='viewshed'
