import importlib
base=importlib.import_module('scripts.test.online-hosted-raster-contour-gate_test')


class HostedVectorZonalGateTest(base.HostedContourGateTest):
    prefix='zonal-statistics'
    family='vector'
    runtime_family='vector'

    def setUp(self):
        super().setUp()
        fixture=self.host.repository/'business/scripts/online-vector-zonal-statistics-fixture.py'
        fixture.write_text(fixture.read_text().replace("root/'zonal-statistics-","root/'vector-zonal-statistics-"))
        make=self.host.bin/'make'
        make.write_text(make.read_text().replace('ADDP_ONLINE_ZONAL_STATISTICS_', 'ADDP_ONLINE_VECTOR_ZONAL_STATISTICS_'))
        subprocess=importlib.import_module('subprocess')
        subprocess.run(['git','add','.'],cwd=self.host.repository,check=True)
        subprocess.run(['git','commit','-qm','vector zone fixture state'],cwd=self.host.repository,check=True)
