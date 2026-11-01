"""Native Runtime tests use an explicit System-shaped policy fixture."""
import pytest
from osgeo import gdal

@pytest.fixture(autouse=True)
def authoritative_raster_policy(monkeypatch):
    import api_server
    from raster_resources import RasterResources
    previous_cache = gdal.GetCacheMax()

    class Client:
        def fetch(self):
            return {'policy': {'engine_id': 1, 'version': 1, 'running': 2, 'waiting': 2,
                    'cache_mib': 256, 'default_tenant_running': 2, 'default_tenant_waiting': 2}, 'quotas': []}

    monkeypatch.setattr(api_server, 'raster_resources', RasterResources(lambda: Client(), poll=False))
    yield
    gdal.SetCacheMax(previous_cache)


@pytest.hookimpl(hookwrapper=True, trylast=True)
def pytest_runtest_makereport(item, call):
    """Retain named failures even when the enclosing CI job ends before summary."""
    outcome = yield
    report = outcome.get_result()
    if report.failed:
        terminal = item.config.pluginmanager.getplugin('terminalreporter')
        if terminal is not None:
            terminal.write_sep('=', f'NATIVE TEST FAILURE {report.nodeid} [{report.when}]')
            terminal.write_line(report.longreprtext)
