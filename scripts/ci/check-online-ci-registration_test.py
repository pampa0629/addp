import importlib.util
import re
import shutil
import sys
import tempfile
import textwrap
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("check-online-ci-registration.py")
SPEC = importlib.util.spec_from_file_location("check_online_ci_registration", SCRIPT)
CHECK = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = CHECK
SPEC.loader.exec_module(CHECK)


class OnlineCIRegistrationTest(unittest.TestCase):
    def test_catalog_overview_requires_cleanup_permissions_and_browser_contract(self):
        source = SCRIPT.parents[2]
        paths = ("scripts/test/online-host-gate.sh", "scripts/test/catalog-domain-overview-online.py",
                 "scripts/test/catalog-domain-overview-online_test.py", "console/frontend/e2e/catalog-domain-overview.spec.js", "Makefile")
        for relative in paths:
            target = self.repository / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source / relative, target)
        registered = {"catalog-domain-overview"}
        CHECK.validate_catalog_domain_overview_profile(self.repository, registered)
        for relative, fragment in ((paths[0], "ADDP_ONLINE_CATALOG_READER_ACCESS_TOKEN"),
                                   (paths[1], "reference_element_unchanged"),
                                   (paths[2], "test_cleanup_attempts_all_exact_resources_and_reports_failure"),
                                   (paths[3], "catalog-domain-overview"),
                                   (paths[4], "$(MAKE) test-catalog-domain-online-runner")):
            target = self.repository / relative
            original = target.read_text()
            target.write_text(original.replace(fragment, "omitted"))
            with self.subTest(fragment=fragment), self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_catalog_domain_overview_profile(self.repository, registered)
            target.write_text(original)

    def test_disk_pressure_requires_owned_ext4_exact_rejection_recovery_and_cleanup(self):
        source = SCRIPT.parents[2]
        paths = ('.github/workflows/online-t4-gates.yml', 'scripts/test/online-gate.py', 'Makefile',
                 'scripts/test/online-hosted-raster-disk-pressure-gate.sh',
                 'scripts/test/raster-disk-pressure-online.py', 'business/scripts/online-raster-disk-pressure-fixture.py',
                 'engines/geopython-workflow/test_online_disk_pressure_fixture.py')
        for relative in paths:
            target = self.repository/relative; target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source/relative, target)
        registered = {'raster-disk-pressure'}
        CHECK.validate_raster_disk_pressure_profile(self.repository, registered)
        mutations = ((paths[0], "inputs.suite != 'raster-disk-pressure'"),
            (paths[0], 'nfs-kernel-server nfs-common e2fsprogs util-linux'),
            (paths[3], '--suite raster-workflow'), (paths[3], 'TMPDIR=/raster-pressure-tmp'),
            (paths[3], 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'), (paths[4], 'raster.strict_sizes'),
            (paths[4], 'capacity.validate_scan'), (paths[4], 'SubmittedLanguageClient'),
            (paths[4], 'product Runtime or master/worker restarted'), (paths[4], 'pending_bytes'), (paths[4], 'workflow_sha256'),
            (paths[5], 'refusing foreign loop cleanup'), (paths[5], 'nodiscard'), (paths[5], 'loop_detached'),
            (paths[5], 'np.array_equal(band.ReadAsArray(), expected, equal_nan=True)'),
            (paths[6], 'test_pressure_native_exact_phase_rejection_and_same_process_recovery'))
        for relative, fragment in mutations:
            target = self.repository/relative; original = target.read_text()
            target.write_text(original.replace(fragment, 'omitted'))
            with self.subTest(fragment=fragment), self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_disk_pressure_profile(self.repository, registered)
            target.write_text(original)

    def test_workflow_runtime_health_requires_manual_hosted_lifecycle_and_regressions(self):
        source = SCRIPT.parents[2]
        paths = ("Makefile", "scripts/test/online-host-gate.sh", ".github/workflows/online-t4-gates.yml",
                 "scripts/test/workflow-runtime-health-recovery-online.py", "scripts/test/workflow-runtime-health-recovery-online_test.py",
                 "scripts/test/workflow-runtime-health-tool.py", "scripts/test/online-hosted-workflow-health-gate.sh",
                 "scripts/test/online-hosted-workflow-health-gate_test.py")
        for relative in paths:
            target = self.repository / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source / relative, target)
        registered = {"workflow-runtime-health-recovery"}
        CHECK.validate_workflow_runtime_health_profile(self.repository, registered)
        for relative, fragment in ((paths[0], "$(MAKE) test-workflow-runtime-health-online-runner"),
                                   (paths[0], "WorkflowRuntimeHealth"),
                                   (paths[2], "inputs.suite != 'workflow-runtime-health-recovery'"),
                                   (paths[6], "workflow-runtime-health-recovery-online.py --restore-only"),
                                   (paths[6], "--suite workflow-runtime-health-recovery"),
                                   (paths[6], "workflow-runtime-health-recovery-online.py --assert-empty"),
                                   (paths[3], '"startup_cycles": startup_cycles'),
                                   (paths[4], "test_startup_recovery_uses_only_automatic_registration_and_periodic_observation")):
            target = self.repository / relative
            original = target.read_text()
            target.write_text(original.replace(fragment, "omitted"))
            with self.subTest(fragment=fragment), self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_workflow_runtime_health_profile(self.repository, registered)
            target.write_text(original)
        host = self.repository / paths[1]
        host.write_text(host.read_text() + "\n# workflow-runtime-health-recovery\n")
        with self.assertRaises(CHECK.RegistrationError):
            CHECK.validate_workflow_runtime_health_profile(self.repository, registered)

    def test_administrator_governance_requires_manual_isolated_profile(self):
        source = SCRIPT.parents[2]
        paths = ('.github/workflows/online-t4-gates.yml', 'Makefile',
                 'scripts/test/online-hosted-administrator-governance-gate.sh',
                 'scripts/test/platform-administrator-governance-online.py',
                 'scripts/test/platform-administrator-governance-online_test.py',
                 'scripts/test/online-hosted-administrator-governance-gate_test.py',
                 'system/backend/cmd/online-test-fixture/administrator_governance.go',
                 'system/backend/cmd/online-test-fixture/administrator_governance_test.go',
                 'console/frontend/e2e/online/platform-administrator-governance.spec.js',
                 'system/backend/cmd/iam-recovery/main.go', 'system/backend/cmd/iam-bootstrap/main.go')
        for relative in paths:
            target = self.repository / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source / relative, target)
        registered = {'platform-administrator-governance'}
        CHECK.validate_administrator_governance_profile(self.repository, registered)
        for relative, fragment in ((paths[0], "inputs.suite != 'platform-administrator-governance'"),
                                   (paths[0], "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'platform-administrator-governance'"),
                                   (paths[1], '$(MAKE) test-platform-administrator-online-runner'),
                                   (paths[2], 'source "$ROOT_DIR/scripts/utils/hosted-online.sh"'),
                                   (paths[2], '--suite platform-administrator-governance'),
                                   (paths[2], 'make build-iam-recovery build-iam-bootstrap GOOS=linux GOARCH=amd64 OUT_DIR=dist BUILD_TYPE=release')):
            target = self.repository / relative; original = target.read_text()
            target.write_text(original.replace(fragment, 'omitted'))
            with self.subTest(fragment=fragment), self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_administrator_governance_profile(self.repository, registered)
            target.write_text(original)

    def test_vector_zonal_requires_manual_nfs_complete_gpkg_and_regressions(self):
        source=SCRIPT.parents[2]
        paths=('.github/workflows/online-t4-gates.yml','scripts/test/online-gate.py','Makefile',
            'scripts/test/online-hosted-vector-zonal-statistics-gate.sh','scripts/test/vector-zonal-statistics-persistence-online.py',
            'business/scripts/online-vector-zonal-statistics-fixture.py','scripts/utils/online_nfs_fixture.py',
            'engines/geopython-workflow/test_online_raster_fixture.py')
        for relative in paths:
            target=self.repository/relative;target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source/relative,target)
        registered={'vector-zonal-statistics-persistence'}
        CHECK.validate_vector_zonal_statistics_profile(self.repository,registered)
        for relative,fragment in ((paths[0],"inputs.suite != 'vector-zonal-statistics-persistence'"),
                (paths[0],'nfs-kernel-server nfs-common'),(paths[2],'@$(MAKE) test-vector-zonal-statistics-online-runner'),
                (paths[3],'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),(paths[3],'--mount'),
                (paths[3],'--wait-protection-owners develop,manager'),(paths[4],'triggered_by'),
                (paths[4],'validate_item'),(paths[4],'complete_geometry_attributes_statistics_verified'),(paths[4],'scan_ids(client)!=before_scans'),
                (paths[5],'expected_rows'),(paths[6],'root_squash'),(paths[7],'test_vector_zonal_persistence_oracle_rejects_corruption')):
            target=self.repository/relative;original=target.read_text();target.write_text(original.replace(fragment,'omitted'))
            with self.subTest(fragment=fragment),self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_vector_zonal_statistics_profile(self.repository,registered)
            target.write_text(original)

    def test_proximity_requires_manual_nfs_identity_exact_distances_and_regressions(self):
        source=SCRIPT.parents[2]
        paths=('.github/workflows/online-t4-gates.yml','scripts/test/online-gate.py','Makefile',
            'scripts/test/online-hosted-raster-proximity-gate.sh','scripts/test/raster-proximity-persistence-online.py',
            'business/scripts/online-raster-proximity-fixture.py','scripts/utils/online_nfs_fixture.py',
            'engines/geopython-workflow/test_online_raster_fixture.py')
        for relative in paths:
            target=self.repository/relative;target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source/relative,target)
        registered={'raster-proximity-persistence'}
        CHECK.validate_raster_proximity_profile(self.repository,registered)
        for relative,fragment in ((paths[0],"inputs.suite != 'raster-proximity-persistence'"),
                (paths[0],'nfs-kernel-server nfs-common'),(paths[2],'@$(MAKE) test-raster-proximity-online-runner'),
                (paths[3],'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),(paths[3],'--mount'),
                (paths[3],'--wait-protection-owners develop,manager'),(paths[4],'triggered_by'),
                (paths[4],'raster.strict_sizes'),(paths[4],'full_pixel_and_mask_verification'),(paths[4],'scan_ids(client)!=before_scans'),
                (paths[5],'expected_pixels'),(paths[6],'root_squash'),(paths[7],'test_proximity_persistence_oracle_rejects_corruption')):
            target=self.repository/relative;original=target.read_text();target.write_text(original.replace(fragment,'omitted'))
            with self.subTest(fragment=fragment),self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_proximity_profile(self.repository,registered)
            target.write_text(original)

    def test_focal_mode_requires_manual_nfs_exact_integer_pixels_and_regressions(self):
        source=SCRIPT.parents[2]
        paths=('.github/workflows/online-t4-gates.yml','scripts/test/online-gate.py','Makefile',
            'scripts/test/online-hosted-raster-focal-mode-gate.sh','scripts/test/raster-focal-mode-persistence-online.py',
            'business/scripts/online-raster-focal-mode-fixture.py','scripts/utils/online_nfs_fixture.py',
            'engines/geopython-workflow/test_online_focal_mode_fixture.py')
        for relative in paths:
            target=self.repository/relative;target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source/relative,target)
        registered={'raster-focal-mode-persistence'}
        CHECK.validate_raster_focal_mode_profile(self.repository,registered)
        for relative,fragment in ((paths[0],"inputs.suite != 'raster-focal-mode-persistence'"),
                (paths[0],'nfs-kernel-server nfs-common'),(paths[2],'@$(MAKE) test-raster-focal-mode-online-runner'),(paths[2],'test_online_focal_mode_fixture.py'),
                (paths[3],'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),(paths[3],'--mount'),
                (paths[3],'--wait-protection-owners develop,manager'),(paths[4],'triggered_by'),
                (paths[4],'raster.strict_sizes'),(paths[4],'full_pixel_and_mask_verification'),(paths[4],'scan_ids(client)!=before_scans'),
                (paths[5],'expected_pixels'),(paths[5],'Counter'),(paths[5],'np.array_equal(actual,expected,equal_nan=True)'),(paths[4],'exact_integer_pixels_verified'),(paths[6],'root_squash'),(paths[7],'test_focal_mode_persistence_oracle_rejects_corruption')):
            target=self.repository/relative;original=target.read_text();target.write_text(original.replace(fragment,'omitted'))
            with self.subTest(fragment=fragment),self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_focal_mode_profile(self.repository,registered)
            target.write_text(original)

    def test_boundary_clean_requires_manual_nfs_exact_integer_pixels_and_regressions(self):
        source=SCRIPT.parents[2]
        paths=('.github/workflows/online-t4-gates.yml','scripts/test/online-gate.py','Makefile',
            'scripts/test/online-hosted-raster-boundary-clean-gate.sh','scripts/test/raster-boundary-clean-persistence-online.py',
            'business/scripts/online-raster-boundary-clean-fixture.py','scripts/utils/online_nfs_fixture.py',
            'engines/geopython-workflow/test_online_boundary_clean_fixture.py')
        for relative in paths:
            target=self.repository/relative;target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source/relative,target)
        registered={'raster-boundary-clean-persistence'}
        CHECK.validate_raster_boundary_clean_profile(self.repository,registered)
        for relative,fragment in ((paths[0],"inputs.suite != 'raster-boundary-clean-persistence'"),
                (paths[0],'nfs-kernel-server nfs-common'),(paths[2],'@$(MAKE) test-raster-boundary-clean-online-runner'),(paths[2],'test_online_boundary_clean_fixture.py'),
                (paths[3],'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),(paths[3],'--mount'),
                (paths[3],'--wait-protection-owners develop,manager'),(paths[4],'validate_scan(scanned,scan,identifier'),
                (paths[4],'raster.strict_sizes'),(paths[4],'full_pixel_and_mask_verification'),(paths[4],'scan_ids(client)!=before_scans'),
                (paths[5],'expected_pixels'),(paths[5],'Counter'),(paths[5],'np.array_equal(actual,expected,equal_nan=True)'),(paths[4],'exact_integer_pixels_verified'),(paths[4],'source_edges(client'),(paths[5],'empty-clean.cog.tif'),(paths[6],'root_squash'),(paths[7],'test_boundary_clean_persistence_oracle_rejects_corruption')):
            target=self.repository/relative;original=target.read_text();target.write_text(original.replace(fragment,'omitted'))
            with self.subTest(fragment=fragment),self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_boundary_clean_profile(self.repository,registered)
            target.write_text(original)

    def test_thin_requires_manual_nfs_exact_integer_pixels_and_regressions(self):
        source=SCRIPT.parents[2]
        paths=('.github/workflows/online-t4-gates.yml','scripts/test/online-gate.py','Makefile',
            'scripts/test/online-hosted-raster-thin-gate.sh','scripts/test/raster-thin-persistence-online.py',
            'business/scripts/online-raster-thin-fixture.py','scripts/utils/online_nfs_fixture.py',
            'engines/geopython-workflow/test_online_thin_fixture.py')
        for relative in paths:
            target=self.repository/relative;target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source/relative,target)
        registered={'raster-thin-persistence'}
        CHECK.validate_raster_thin_profile(self.repository,registered)
        for relative,fragment in ((paths[0],"inputs.suite != 'raster-thin-persistence'"),
                (paths[0],'nfs-kernel-server nfs-common'),(paths[2],'@$(MAKE) test-raster-thin-online-runner'),(paths[2],'test_online_thin_fixture.py'),
                (paths[3],'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),(paths[3],'--mount'),
                (paths[3],'--wait-protection-owners develop,manager'),(paths[4],'validate_scan(scanned,scan,identifier'),
                (paths[4],'raster.strict_sizes'),(paths[4],'full_pixel_and_mask_verification'),(paths[4],'scan_ids(client)!=before_scans'),
                (paths[5],'expected_pixels'),(paths[5],'adjacent_a'),(paths[5],'np.array_equal(actual,expected,equal_nan=True)'),(paths[4],'exact_integer_pixels_verified'),(paths[4],'source_edges(client'),(paths[4],'binary_skeleton_verified'),(paths[4],'invalid_diagnostic'),(paths[4],'foreground_value'),(paths[5],'empty-thin.cog.tif'),(paths[6],'root_squash'),(paths[7],'test_thin_persistence_oracle_rejects_corruption')):
            target=self.repository/relative;original=target.read_text();target.write_text(original.replace(fragment,'omitted'))
            with self.subTest(fragment=fragment),self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_thin_profile(self.repository,registered)
            target.write_text(original)

    def test_fill_depressions_requires_manual_nfs_full_pixels_and_regressions(self):
        source=SCRIPT.parents[2]
        paths=('.github/workflows/online-t4-gates.yml','scripts/test/online-gate.py','Makefile',
            'scripts/test/online-hosted-raster-fill-depressions-gate.sh','scripts/test/raster-fill-depressions-persistence-online.py',
            'business/scripts/online-raster-fill-depressions-fixture.py','scripts/utils/online_nfs_fixture.py',
            'engines/geopython-workflow/test_online_fill_depressions_fixture.py')
        for relative in paths:
            target=self.repository/relative;target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source/relative,target)
        registered={'raster-fill-depressions-persistence'}
        CHECK.validate_raster_fill_depressions_profile(self.repository,registered)
        for relative,fragment in ((paths[0],"inputs.suite != 'raster-fill-depressions-persistence'"),
                (paths[0],'nfs-kernel-server nfs-common'),(paths[2],'@$(MAKE) test-raster-fill-depressions-online-runner'),(paths[2],'test_online_fill_depressions_fixture.py'),
                (paths[3],'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),(paths[3],'--mount'),
                (paths[3],'--wait-protection-owners develop,manager'),(paths[4],'validate_scan(scanned,scan,identifier'),
                (paths[4],'raster.strict_sizes'),(paths[4],'full_pixel_and_mask_verification'),(paths[4],'scan_ids(client)!=before_scans'),
                (paths[5],'expected_pixels'),(paths[5],'raw_filled_pixels'),(paths[5],'np.array_equal(actual,expected,equal_nan=True)'),(paths[4],'unit_metres_verified'),(paths[4],'source_edges(client'),(paths[4],'depression_fill_verified'),(paths[4],'invalid_diagnostic'),(paths[4],'elevation_unit'),(paths[5],'empty-filled.cog.tif'),(paths[6],'root_squash'),(paths[7],'test_fill_depressions_persistence_oracle_rejects_corruption')):
            target=self.repository/relative;original=target.read_text();target.write_text(original.replace(fragment,'omitted'))
            with self.subTest(fragment=fragment),self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_fill_depressions_profile(self.repository,registered)
            target.write_text(original)

    def test_flow_direction_d8_requires_manual_nfs_full_pixels_and_regressions(self):
        source=SCRIPT.parents[2]
        paths=('.github/workflows/online-t4-gates.yml','scripts/test/online-gate.py','Makefile',
            'scripts/test/online-hosted-raster-flow-direction-d8-gate.sh','scripts/test/raster-flow-direction-d8-persistence-online.py',
            'business/scripts/online-raster-flow-direction-d8-fixture.py','scripts/utils/online_nfs_fixture.py',
            'engines/geopython-workflow/test_online_flow_direction_d8_fixture.py')
        for relative in paths:
            target=self.repository/relative;target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source/relative,target)
        registered={'raster-flow-direction-d8-persistence'}
        CHECK.validate_raster_flow_direction_d8_profile(self.repository,registered)
        for relative,fragment in ((paths[0],"inputs.suite != 'raster-flow-direction-d8-persistence'"),
                (paths[0],'nfs-kernel-server nfs-common'),(paths[2],'@$(MAKE) test-raster-flow-direction-d8-online-runner'),(paths[2],'test_online_flow_direction_d8_fixture.py'),
                (paths[3],'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),(paths[3],'--mount'),
                (paths[3],'--wait-protection-owners develop,manager'),(paths[4],'validate_scan(scanned,scan,identifier'),
                (paths[4],'raster.strict_sizes'),(paths[4],'full_pixel_and_mask_verification'),(paths[4],'scan_ids(client)!=before_scans'),
                (paths[5],'expected_pixels'),(paths[5],'routing_evidence'),(paths[5],'np.array_equal(actual,expected,equal_nan=True)'),(paths[4],'direction_unit_cleared'),(paths[4],'source_edges(client'),(paths[4],'shortest_flat_paths_verified'),(paths[4],'invalid_diagnostic'),(paths[4],'elevation_unit'),(paths[4],'SubmittedLanguageClient'),(paths[4],"for language in ('zh-cn','en')"),(paths[4],'acyclic_verified'),(paths[4],'final_open_outlets_verified'),(paths[5],'empty-direction.cog.tif'),(paths[6],'root_squash'),(paths[7],'test_flow_direction_d8_persistence_oracle_rejects_corruption')):
            target=self.repository/relative;original=target.read_text();target.write_text(original.replace(fragment,'omitted'))
            with self.subTest(fragment=fragment),self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_flow_direction_d8_profile(self.repository,registered)
            target.write_text(original)

    def test_flow_accumulation_d8_requires_manual_nfs_full_pixels_and_regressions(self):
        source=SCRIPT.parents[2]
        paths=('.github/workflows/online-t4-gates.yml','scripts/test/online-gate.py','Makefile',
            'scripts/test/online-hosted-raster-flow-accumulation-d8-gate.sh','scripts/test/raster-flow-accumulation-d8-persistence-online.py',
            'business/scripts/online-raster-flow-accumulation-d8-fixture.py','scripts/utils/online_nfs_fixture.py',
            'engines/geopython-workflow/test_online_flow_accumulation_d8_fixture.py')
        for relative in paths:
            target=self.repository/relative;target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source/relative,target)
        registered={'raster-flow-accumulation-d8-persistence'}
        CHECK.validate_raster_flow_accumulation_d8_profile(self.repository,registered)
        for relative,fragment in ((paths[0],"inputs.suite != 'raster-flow-accumulation-d8-persistence'"),
                (paths[0],'nfs-kernel-server nfs-common'),(paths[2],'@$(MAKE) test-raster-flow-accumulation-d8-online-runner'),(paths[2],'test_online_flow_accumulation_d8_fixture.py'),
                (paths[3],'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),(paths[3],'--mount'),
                (paths[3],'--wait-protection-owners develop,manager'),(paths[4],'validate_scan(scanned,scan,identifier'),
                (paths[4],'raster.strict_sizes'),(paths[4],'full_pixel_and_mask_verification'),(paths[4],'scan_ids(client)!=before_scans'),
                (paths[5],'expected_pixels'),(paths[5],'conservation_evidence'),(paths[5],'np.array_equal(actual,expected,equal_nan=True)'),(paths[4],'count_unit_cleared'),(paths[4],'source_edges(client'),(paths[4],'terminal_conservation_verified'),(paths[4],'invalid_diagnostic'),(paths[4],'elevation_unit'),(paths[4],'SubmittedLanguageClient'),(paths[4],"for language in ('zh-cn','en')"),(paths[4],'counts_exclude_self'),(paths[5],'empty-accumulation.cog.tif'),(paths[6],'root_squash'),(paths[7],'test_flow_accumulation_d8_persistence_oracle_rejects_corruption')):
            target=self.repository/relative;original=target.read_text();target.write_text(original.replace(fragment,'omitted'))
            with self.subTest(fragment=fragment),self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_flow_accumulation_d8_profile(self.repository,registered)
            target.write_text(original)

    def test_drainage_basins_d8_requires_manual_nfs_full_pixels_and_regressions(self):
        source=SCRIPT.parents[2]
        paths=('.github/workflows/online-t4-gates.yml','scripts/test/online-gate.py','Makefile',
            'scripts/test/online-hosted-raster-drainage-basins-d8-gate.sh','scripts/test/raster-drainage-basins-d8-persistence-online.py',
            'business/scripts/online-raster-drainage-basins-d8-fixture.py','scripts/utils/online_nfs_fixture.py',
            'engines/geopython-workflow/test_online_drainage_basins_d8_fixture.py')
        for relative in paths:
            target=self.repository/relative;target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source/relative,target)
        registered={'raster-drainage-basins-d8-persistence'}
        CHECK.validate_raster_drainage_basins_d8_profile(self.repository,registered)
        for relative,fragment in ((paths[0],"inputs.suite != 'raster-drainage-basins-d8-persistence'"),
                (paths[0],'nfs-kernel-server nfs-common'),(paths[2],'@$(MAKE) test-raster-drainage-basins-d8-online-runner'),(paths[2],'test_online_drainage_basins_d8_fixture.py'),
                (paths[3],'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),(paths[3],'--mount'),
                (paths[3],'--wait-protection-owners develop,manager'),(paths[4],'validate_scan(scanned,scan,identifier'),
                (paths[4],'raster.strict_sizes'),(paths[4],'full_pixel_and_mask_verification'),(paths[4],'scan_ids(client)!=before_scans'),
                (paths[5],'expected_pixels'),(paths[5],'basin_evidence'),(paths[5],'np.array_equal(actual,expected,equal_nan=True)'),(paths[4],'basin_conservation_verified'),(paths[4],'basin_unit_cleared'),(paths[4],'source_edges(client'),(paths[4],'outlet_order_verified'),(paths[4],'invalid_diagnostic'),(paths[4],'elevation_unit'),(paths[4],'SubmittedLanguageClient'),(paths[4],"for language in ('zh-cn','en')"),(paths[4],'unique_membership_verified'),(paths[5],'empty-basins.cog.tif'),(paths[6],'root_squash'),(paths[7],'test_drainage_basins_d8_persistence_oracle_rejects_corruption')):
            target=self.repository/relative;original=target.read_text();target.write_text(original.replace(fragment,'omitted'))
            with self.subTest(fragment=fragment),self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_drainage_basins_d8_profile(self.repository,registered)
            target.write_text(original)

    def test_flow_length_d8_requires_manual_nfs_full_pixels_and_regressions(self):
        source=SCRIPT.parents[2]
        paths=('.github/workflows/online-t4-gates.yml','scripts/test/online-gate.py','Makefile',
            'scripts/test/online-hosted-raster-flow-length-d8-gate.sh','scripts/test/raster-flow-length-d8-persistence-online.py',
            'business/scripts/online-raster-flow-length-d8-fixture.py','scripts/utils/online_nfs_fixture.py',
            'engines/geopython-workflow/test_online_flow_length_d8_fixture.py')
        for relative in paths:
            target=self.repository/relative;target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source/relative,target)
        registered={'raster-flow-length-d8-persistence'}
        CHECK.validate_raster_flow_length_d8_profile(self.repository,registered)
        for relative,fragment in ((paths[0],"inputs.suite != 'raster-flow-length-d8-persistence'"),
                (paths[0],'nfs-kernel-server nfs-common'),(paths[2],'@$(MAKE) test-raster-flow-length-d8-online-runner'),(paths[2],'test_online_flow_length_d8_fixture.py'),
                (paths[3],'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),(paths[3],'--mount'),
                (paths[3],'--wait-protection-owners develop,manager'),(paths[4],'validate_scan(scanned,scan,identifier'),
                (paths[4],'raster.strict_sizes'),(paths[4],'full_pixel_and_mask_verification'),(paths[4],'scan_ids(client)!=before_scans'),
                (paths[5],'expected_pixels'),(paths[5],'distance_evidence'),(paths[5],'np.allclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True)'),(paths[4],'metre_unit_verified'),(paths[4],'source_edges(client'),(paths[4],'centre_steps_verified'),(paths[4],'invalid_diagnostic'),(paths[4],'elevation_unit'),(paths[4],'SubmittedLanguageClient'),(paths[4],"for language in ('zh-cn','en')"),(paths[4],'terminals_zero'),(paths[5],'empty-length.cog.tif'),(paths[5],'feet-length.cog.tif'),(paths[5],'survey-feet-length.cog.tif'),(paths[5],'SOURCE_UNITS'),(paths[4],'fixture.artifact_expectation(name)'),(paths[6],'root_squash'),(paths[7],'test_flow_length_d8_persistence_oracle_rejects_corruption')):
            target=self.repository/relative;original=target.read_text();target.write_text(original.replace(fragment,'omitted'))
            with self.subTest(fragment=fragment),self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_flow_length_d8_profile(self.repository,registered)
            target.write_text(original)

    def test_flow_length_upstream_d8_requires_manual_nfs_full_pixels_and_regressions(self):
        source=SCRIPT.parents[2]
        paths=('.github/workflows/online-t4-gates.yml','scripts/test/online-gate.py','Makefile',
            'scripts/test/online-hosted-raster-flow-length-upstream-d8-gate.sh','scripts/test/raster-flow-length-upstream-d8-persistence-online.py',
            'business/scripts/online-raster-flow-length-upstream-d8-fixture.py','scripts/utils/online_nfs_fixture.py',
            'engines/geopython-workflow/test_online_flow_length_upstream_d8_fixture.py')
        for relative in paths:
            target=self.repository/relative;target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source/relative,target)
        registered={'raster-flow-length-upstream-d8-persistence'}
        CHECK.validate_raster_flow_length_upstream_d8_profile(self.repository,registered)
        for relative,fragment in ((paths[0],"inputs.suite != 'raster-flow-length-upstream-d8-persistence'"),
                (paths[0],'nfs-kernel-server nfs-common'),(paths[2],'@$(MAKE) test-raster-flow-length-upstream-d8-online-runner'),(paths[2],'test_online_flow_length_upstream_d8_fixture.py'),
                (paths[3],'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),(paths[3],'--mount'),
                (paths[3],'--wait-protection-owners develop,manager'),(paths[4],'validate_scan(scanned,scan,identifier'),
                (paths[4],'raster.strict_sizes'),(paths[4],'full_pixel_and_mask_verification'),(paths[4],'scan_ids(client)!=before_scans'),
                (paths[5],'expected_pixels'),(paths[5],'distance_evidence'),(paths[5],'np.allclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True)'),(paths[4],'metre_unit_verified'),(paths[4],'source_edges(client'),(paths[4],'centre_steps_verified'),(paths[4],'invalid_diagnostic'),(paths[4],'elevation_unit'),(paths[4],'SubmittedLanguageClient'),(paths[4],"for language in ('zh-cn','en')"),(paths[4],'sources_zero'),(paths[4],'longest_path_verified'),(paths[4],'confluence_max_verified'),(paths[4],'outlets_receive_upstream'),(paths[5],'empty-length.cog.tif'),(paths[5],'feet-length.cog.tif'),(paths[5],'survey-feet-length.cog.tif'),(paths[5],'SOURCE_UNITS'),(paths[4],'fixture.artifact_expectation(name)'),(paths[6],'root_squash'),(paths[7],'test_flow_length_upstream_d8_persistence_oracle_rejects_corruption')):
            target=self.repository/relative;original=target.read_text();target.write_text(original.replace(fragment,'omitted'))
            with self.subTest(fragment=fragment),self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_flow_length_upstream_d8_profile(self.repository,registered)
            target.write_text(original)

    def test_band_recommendation_requires_manual_nfs_readonly_oif_and_regressions(self):
        source=SCRIPT.parents[2]
        paths=('.github/workflows/online-t4-gates.yml', 'scripts/test/online-gate.py', 'scripts/test/online-hosted-raster-band-recommendation-gate.sh', 'scripts/test/raster-band-recommendation-workflow-online.py', 'business/scripts/online-raster-band-recommendation-fixture.py', 'scripts/utils/online_nfs_fixture.py', 'engines/geopython-workflow/test_online_band_recommendation_fixture.py', 'Makefile')
        for relative in paths:
            target=self.repository/relative;target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source/relative,target)
        registered={'raster-band-recommendation-workflow'}
        CHECK.validate_raster_band_recommendation_profile(self.repository,registered)
        for relative,fragment in (('.github/workflows/online-t4-gates.yml', "inputs.suite != 'raster-band-recommendation-workflow'"), ('.github/workflows/online-t4-gates.yml', 'nfs-kernel-server nfs-common'), ('Makefile', 'test_online_band_recommendation_fixture.py'), ('scripts/test/online-hosted-raster-band-recommendation-gate.sh', 'source "$ROOT_DIR/scripts/utils/hosted-online.sh"'), ('scripts/test/online-hosted-raster-band-recommendation-gate.sh', 'start_online_geopython_runtime'), ('scripts/test/online-hosted-raster-band-recommendation-gate.sh', '--mount'), ('scripts/test/raster-band-recommendation-workflow-online.py', 'raster.validate_identity'), ('scripts/test/raster-band-recommendation-workflow-online.py', 'raster.EDITOR_PERMISSIONS'), ('scripts/test/raster-band-recommendation-workflow-online.py', 'raster.transient_result'), ('business/scripts/online-raster-band-recommendation-fixture.py', 'NFSExport'), ('business/scripts/online-raster-band-recommendation-fixture.py', 'owned_root'), ('business/scripts/online-raster-band-recommendation-fixture.py', 'expected_result'), ('scripts/utils/online_nfs_fixture.py', 'root_squash'), ('scripts/utils/online_nfs_fixture.py', 'nfs_export_removed'), ('scripts/utils/online_nfs_fixture.py', 'owned_root'), ('engines/geopython-workflow/test_online_band_recommendation_fixture.py', 'test_band_recommendation_fixture_complete_sources_and_full_product_object'), ('engines/geopython-workflow/test_online_band_recommendation_fixture.py', 'test_band_recommendation_fixture_object_comparison_is_complete_and_strict')):
            target=self.repository/relative;original=target.read_text();target.write_text(original.replace(fragment,'omitted'))
            with self.subTest(fragment=fragment),self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_band_recommendation_profile(self.repository,registered)
            target.write_text(original)

    def test_nibble_capacity_requires_manual_limits_disk_and_full_oracle(self):
        source = SCRIPT.parents[2]
        paths = ('.github/workflows/online-t4-gates.yml', 'scripts/test/online-gate.py', 'Makefile',
                 'scripts/test/online-hosted-raster-nibble-capacity-gate.sh',
                 'scripts/test/raster-nibble-capacity-online.py', 'business/scripts/online-raster-nibble-capacity-fixture.py',
                 'engines/geopython-workflow/test_online_nibble_capacity_fixture.py',
                 'system/backend/cmd/online-test-fixture/main.go', 'system/backend/cmd/online-test-fixture/main_test.go')
        for relative in paths:
            target = self.repository/relative; target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source/relative, target)
        registered = {'raster-nibble-capacity'}
        CHECK.validate_raster_nibble_capacity_profile(self.repository, registered)
        for relative, fragment in ((paths[0], "inputs.suite != 'raster-nibble-capacity'"),
                (paths[3], '--suite raster-nibble-capacity'),
                (paths[3], '--cpus 4 --memory 2g --memory-swap 2g'),
                (paths[3], 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=10800'), (paths[3], 'TMPDIR='),
                (paths[4], 'raster.strict_sizes'), (paths[4], 'capacity.validate_measurement'),
                (paths[4], 'validate_disk'), (paths[5], 'expected_block'),
                (paths[5], 'running*size**2*64'), (paths[6], 'test_capacity_analytical_oracle_matches_original_donor_bruteforce'),
                (paths[7], 'case "raster-workflow", "raster-nibble-capacity":'),
                (paths[8], 'TestNibbleCapacityRetainsExactlyTheOrdinaryRasterPermissions')):
            target = self.repository/relative; original = target.read_text()
            target.write_text(original.replace(fragment, 'omitted'))
            with self.subTest(fragment=fragment), self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_nibble_capacity_profile(self.repository, registered)
            target.write_text(original)

    def test_nibble_requires_manual_nfs_exact_donors_and_regressions(self):
        source=SCRIPT.parents[2]
        paths=('.github/workflows/online-t4-gates.yml', 'scripts/test/online-gate.py', 'scripts/test/online-hosted-raster-nibble-gate.sh', 'scripts/test/raster-nibble-persistence-online.py', 'business/scripts/online-raster-nibble-fixture.py', 'scripts/utils/online_nfs_fixture.py', 'engines/geopython-workflow/test_online_nibble_fixture.py', 'Makefile')
        for relative in paths:
            target=self.repository/relative;target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source/relative,target)
        registered={'raster-nibble-persistence'}
        CHECK.validate_raster_nibble_profile(self.repository,registered)
        for relative,fragment in (('.github/workflows/online-t4-gates.yml', "inputs.suite != 'raster-nibble-persistence'"), ('.github/workflows/online-t4-gates.yml', 'nfs-kernel-server nfs-common'), ('Makefile', 'test_online_nibble_fixture.py'), ('scripts/test/online-hosted-raster-nibble-gate.sh', 'source "$ROOT_DIR/scripts/utils/hosted-online.sh"'), ('scripts/test/online-hosted-raster-nibble-gate.sh', 'start_online_geopython_runtime'), ('scripts/test/online-hosted-raster-nibble-gate.sh', '--mount'), ('scripts/test/raster-nibble-persistence-online.py', 'raster.validate_identity'), ('scripts/test/raster-nibble-persistence-online.py', 'raster.EDITOR_PERMISSIONS'), ('scripts/test/raster-nibble-persistence-online.py', 'fixture.SOURCES'), ('business/scripts/online-raster-nibble-fixture.py', 'NFSExport'), ('business/scripts/online-raster-nibble-fixture.py', 'owned_root'), ('business/scripts/online-raster-nibble-fixture.py', 'expected_pixels'), ('scripts/utils/online_nfs_fixture.py', 'root_squash'), ('scripts/utils/online_nfs_fixture.py', 'nfs_export_removed'), ('scripts/utils/online_nfs_fixture.py', 'owned_root'), ('engines/geopython-workflow/test_online_nibble_fixture.py', 'test_nibble_persistence_real_gdal_three_cog_chains_and_complete_pixels'), ('engines/geopython-workflow/test_online_nibble_fixture.py', 'test_nibble_persistence_oracle_rejects_corruption')):
            target=self.repository/relative;original=target.read_text();target.write_text(original.replace(fragment,'omitted'))
            with self.subTest(fragment=fragment),self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_nibble_profile(self.repository,registered)
            target.write_text(original)

    def test_focal_requires_manual_nfs_identity_strict_sizes_and_regressions(self):
        source=SCRIPT.parents[2]
        paths=('.github/workflows/online-t4-gates.yml','scripts/test/online-gate.py','Makefile',
            'scripts/test/online-hosted-raster-focal-statistics-gate.sh','scripts/test/raster-focal-statistics-persistence-online.py',
            'business/scripts/online-raster-focal-statistics-fixture.py','scripts/utils/online_nfs_fixture.py',
            'engines/geopython-workflow/test_online_raster_fixture.py')
        for relative in paths:
            target=self.repository/relative;target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source/relative,target)
        registered={'raster-focal-statistics-persistence'}
        CHECK.validate_raster_focal_statistics_profile(self.repository,registered)
        for relative,fragment in ((paths[0],"inputs.suite != 'raster-focal-statistics-persistence'"),
                (paths[0],'nfs-kernel-server nfs-common'),(paths[2],'@$(MAKE) test-raster-focal-statistics-online-runner'),
                (paths[3],'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),(paths[3],'--mount'),
                (paths[3],'--wait-protection-owners develop,manager'),(paths[4],'triggered_by'),
                (paths[4],'raster.strict_sizes'),(paths[4],'full_pixel_and_mask_verification'),(paths[4],'scan_ids(client)!=before_scans'),
                (paths[5],'expected_pixels'),(paths[6],'root_squash'),(paths[7],'test_focal_persistence_oracle_rejects_corruption')):
            target=self.repository/relative;original=target.read_text();target.write_text(original.replace(fragment,'omitted'))
            with self.subTest(fragment=fragment),self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_focal_statistics_profile(self.repository,registered)
            target.write_text(original)

    def test_zonal_requires_manual_nfs_identity_complete_csv_and_regressions(self):
        source=SCRIPT.parents[2]
        paths=('.github/workflows/online-t4-gates.yml','scripts/test/online-gate.py','Makefile',
            'scripts/test/online-hosted-raster-zonal-statistics-gate.sh','scripts/test/raster-zonal-statistics-persistence-online.py',
            'business/scripts/online-raster-zonal-statistics-fixture.py','scripts/utils/online_nfs_fixture.py',
            'engines/geopython-workflow/test_online_raster_fixture.py')
        for relative in paths:
            target=self.repository/relative;target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source/relative,target)
        registered={'raster-zonal-statistics-persistence'}
        CHECK.validate_raster_zonal_statistics_profile(self.repository,registered)
        for relative,fragment in ((paths[0],"inputs.suite != 'raster-zonal-statistics-persistence'"),
                (paths[0],'nfs-kernel-server nfs-common'),(paths[2],'@$(MAKE) test-raster-zonal-statistics-online-runner'),
                (paths[3],'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),(paths[3],'--mount'),
                (paths[3],'--wait-protection-owners develop,manager'),(paths[4],'triggered_by'),
                (paths[4],'validate_preview'),(paths[4],'validate_item'),(paths[4],'scan_ids(client)!=before_scans'),
                (paths[5],'validate_records'),(paths[6],'root_squash'),(paths[7],'test_zonal_persistence_oracle_rejects_csv_corruption')):
            target=self.repository/relative;original=target.read_text();target.write_text(original.replace(fragment,'omitted'))
            with self.subTest(fragment=fragment),self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_zonal_statistics_profile(self.repository,registered)
            target.write_text(original)

    def test_rasterize_requires_manual_nfs_identity_strict_sizes_and_regressions(self):
        source=SCRIPT.parents[2]
        paths=('.github/workflows/online-t4-gates.yml','scripts/test/online-gate.py','Makefile',
            'scripts/test/online-hosted-vector-rasterize-gate.sh','scripts/test/vector-rasterize-persistence-online.py',
            'business/scripts/online-vector-rasterize-fixture.py','scripts/utils/online_nfs_fixture.py',
            'engines/geopython-workflow/test_online_raster_fixture.py')
        for relative in paths:
            target=self.repository/relative;target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source/relative,target)
        registered={'vector-rasterize-persistence'}
        CHECK.validate_vector_rasterize_profile(self.repository,registered)
        for relative,fragment in ((paths[0],"inputs.suite != 'vector-rasterize-persistence'"),
                (paths[0],'nfs-kernel-server nfs-common'),(paths[2],'online-vector-rasterize-fixture_test.py'),
                (paths[2],'@$(MAKE) test-vector-rasterize-online-runner'),
                (paths[3],'--wait-protection-owners develop,manager'),(paths[3],'--mount'),
                (paths[4],'raster.strict_sizes'),(paths[4],'triggered_by'),(paths[4],'scan_ids(client)!=before_scans'),
                (paths[6],'root_squash'),(paths[7],'test_rasterize_persistence_oracle_rejects_real_file_corruption')):
            target=self.repository/relative;original=target.read_text();target.write_text(original.replace(fragment,'omitted'))
            with self.subTest(fragment=fragment),self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_vector_rasterize_profile(self.repository,registered)
            target.write_text(original)

    def test_contour_requires_manual_nfs_identity_and_registered_regressions(self):
        source = SCRIPT.parents[2]
        paths = ('.github/workflows/online-t4-gates.yml', 'scripts/test/online-gate.py', 'Makefile',
                 'scripts/test/online-hosted-raster-contour-gate.sh', 'scripts/test/raster-contour-persistence-online.py',
                 'business/scripts/online-raster-contour-fixture.py', 'engines/geopython-workflow/test_online_raster_fixture.py', 'scripts/utils/online_nfs_fixture.py', 'scripts/utils/online_vector_persistence.py')
        for relative in paths:
            target = self.repository / relative; target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source / relative, target)
        registered = {'raster-contour-persistence'}
        CHECK.validate_raster_contour_profile(self.repository, registered)
        for relative, fragment in ((paths[0], "inputs.suite != 'raster-contour-persistence'"),
                (paths[0], 'nfs-kernel-server nfs-common'), (paths[2], 'online-raster-contour-fixture_test.py'),
                (paths[3], '--wait-protection-owners develop,manager'), (paths[3], '--mount'),
                (paths[8], 'triggered_by'), (paths[7], 'root_squash'), (paths[6], 'test_contour_persistence_oracle_rejects_real_file_corruption')):
            target = self.repository / relative; original = target.read_text(); target.write_text(original.replace(fragment, 'omitted'))
            with self.subTest(fragment=fragment), self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_contour_profile(self.repository, registered)
            target.write_text(original)

    def test_polygonize_requires_manual_nfs_identity_and_registered_regressions(self):
        source = SCRIPT.parents[2]
        paths = ('.github/workflows/online-t4-gates.yml', 'scripts/test/online-gate.py', 'Makefile',
                 'scripts/test/online-hosted-raster-polygonize-gate.sh', 'scripts/test/raster-polygonize-persistence-online.py',
                 'business/scripts/online-raster-polygonize-fixture.py', 'engines/geopython-workflow/test_online_raster_fixture.py', 'scripts/utils/online_nfs_fixture.py', 'scripts/utils/online_vector_persistence.py')
        for relative in paths:
            target = self.repository / relative; target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source / relative, target)
        registered = {'raster-polygonize-persistence'}
        CHECK.validate_raster_polygonize_profile(self.repository, registered)
        for relative, fragment in ((paths[0], "inputs.suite != 'raster-polygonize-persistence'"),
                (paths[0], 'nfs-kernel-server nfs-common'), (paths[2], 'online-raster-polygonize-fixture_test.py'),
                (paths[3], '--wait-protection-owners develop,manager'), (paths[3], '--mount'),
                (paths[8], 'triggered_by'), (paths[7], 'root_squash'), (paths[6], 'test_polygonize_persistence_oracle_rejects_real_file_corruption')):
            target = self.repository / relative; original = target.read_text(); target.write_text(original.replace(fragment, 'omitted'))
            with self.subTest(fragment=fragment), self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_polygonize_profile(self.repository, registered)
            target.write_text(original)

    def test_topology_requires_manual_nfs_identity_and_regressions(self):
        source = SCRIPT.parents[2]
        paths = ('.github/workflows/online-t4-gates.yml', 'scripts/test/online-gate.py', 'Makefile',
            'scripts/test/online-hosted-topology-gate.sh', 'scripts/test/vector-topology-persistence-online.py',
            'business/scripts/online-vector-topology-fixture.py', 'scripts/utils/online_nfs_fixture.py',
            'console/frontend/e2e/online/vector-topology-persistence.spec.js', 'system/backend/cmd/online-test-fixture/main.go',
            'engines/geopython-workflow/test_topology_operators.py',
            'manager/frontend/e2e/geometry-preview-support.js', 'manager/frontend/tests/previewGeometryWire.test.js',
            'manager/frontend/e2e/geometry-preview-coordinate.js')
        for relative in paths:
            target = self.repository / relative; target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source / relative, target)
        registered = {'vector-topology-persistence'}
        CHECK.validate_vector_topology_profile(self.repository, registered)
        for relative, fragment in ((paths[0], "inputs.suite != 'vector-topology-persistence'"),
            (paths[0], 'nfs-kernel-server nfs-common'), (paths[2], 'online-hosted-topology-gate_test.py'),
            (paths[3], '--suite vector-topology-persistence'), (paths[4], 'triggered_by'), (paths[7], 'browserAPIFromResponse('),
            (paths[5], 'validate_features'), (paths[5], 'SNAP_EXPECTED'), (paths[5], 'verify_snap'), (paths[5], 'archive_physical_evidence'), (paths[7], 'graph, preview'), (paths[4], 'snap_geometry'), (paths[4], 'check_polygon_overlap'), (paths[4], 'check_polygon_gap'), (paths[4], 'check_line_self_overlap'), (paths[4], 'line_self_overlap_expected'), (paths[4], 'line_self_overlap_input'), (paths[5], 'verify_line_self_overlap'), (paths[5], 'line_self_overlap_expected_features'), (paths[5], 'line-self-overlap-input.gpkg'), (paths[5], 'line-self-overlaps.gpkg'), (paths[7], 'line_self_overlap_expected'), (paths[7], '[evidence.sources[9].id]'), (paths[4], 'check_line_overlap'), (paths[4], 'line_overlap_expected'), (paths[4], 'line_overlap_input'), (paths[5], 'verify_line_overlap'), (paths[5], 'line_overlap_expected_features'), (paths[5], 'line-overlap-input.gpkg'), (paths[5], 'line-overlaps.gpkg'), (paths[7], 'line_overlap_expected'), (paths[7], 'overlap_length'), (paths[7], '[evidence.sources[8].id]'), (paths[4], 'check_dangling_points'), (paths[4], 'dangling_expected'), (paths[4], 'dangling_input'), (paths[5], 'verify_dangling'), (paths[5], 'dangling_expected_features'), (paths[5], 'dangling-input.gpkg'), (paths[5], 'dangling.gpkg'), (paths[7], 'dangling_expected'), (paths[7], "source_id:feature.get('source_id')"), (paths[7], "geometry.type).toBe('Point')"), (paths[7], '[evidence.sources[7].id]'), (paths[4], 'gap_coverage'), (paths[5], 'verify_gap'), (paths[5], 'gap_expected_features'), (paths[5], 'gap-input.gpkg'), (paths[5], 'gap-coverage.gpkg'), (paths[5], 'gaps.gpkg'), (paths[7], 'gap_expected'), (paths[7], 'gapRings'), (paths[7], 'gap_area'), (paths[7], '[evidence.sources[5].id, evidence.sources[6].id]'), (paths[4], 'post_browser_physical'), (paths[5], 'verify_overlap'), (paths[5], 'overlap_expected_features'), (paths[7], 'overlap_expected'), (paths[7], 'rendered'), (paths[4], 'validate_failed_snap'), (paths[4], 'collapsed_snap_diagnostic'), (paths[4], 'topology-snap-failure.json'), (paths[4], 'scan_ids(client)'), (paths[7], 'decodePreviewEWKB'), (paths[7], 'snapCoordinates'), (paths[7], 'failed_execution_id'), (paths[5], 'validate_repaired'), (paths[4], 'repair_geometry'), (paths[7], 'row.after_valid'), (paths[7], 'topology-source-lineage-raw.json'), (paths[6], 'root_squash'), (paths[7], 'getImageData'),
            (paths[7], 'toEqual(snapCoordinates.map(line => line.map(webMercatorPreviewCoordinate))'),
            (paths[7], "getProjection().getCode() !== 'EPSG:3857'"), (paths[12], 'webMercatorPreviewCoordinate'),
            (paths[11], 'display projection round trip exactly'),
            (paths[8], 'case "vector-topology-persistence":'), (paths[9], 'test_hosted_topology_physical_oracle')):
            target = self.repository / relative; original = target.read_text(); target.write_text(original.replace(fragment, 'omitted'))
            with self.subTest(fragment=fragment), self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_vector_topology_profile(self.repository, registered)
            target.write_text(original)

    def test_graph_protection_requires_synthetic_lifecycle_and_regressions(self):
        source = SCRIPT.parents[2]
        paths = ('.github/workflows/online-t4-gates.yml', 'Makefile',
                 'scripts/test/online-hosted-security-gate.sh', 'scripts/test/graph-protection-online.py',
                 'business/scripts/online-graph-fixture.py', 'business/docker-compose.online-graph.yml',
                 'console/frontend/e2e/online/graph-protection.spec.js',
                 'console/frontend/e2e/online/develop-graph-query.spec.js',
                 'scripts/test/docker-compose.graph-protection-t4.yml', 'graph/frontend/Dockerfile', 'graph/frontend/nginx.conf',
                 'scripts/test/graph-review-online.py', 'scripts/test/graph-review-extractor.py', 'console/frontend/e2e/online/graph-review.spec.js',
                 'scripts/test/online-gate.py', 'scripts/utils/hosted-online.sh', 'console/frontend/e2e/graph-visual.fixture.js')
        for relative in paths:
            target = self.repository / relative; target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source / relative, target)
        CHECK.validate_graph_protection_profile(self.repository, {'graph-protection'})
        for relative, fragment in ((paths[0], "inputs.suite != 'graph-protection'"),
                                   (paths[1], 'graph-protection-online_test.py'), (paths[1], 'graph-review-online_test.py'),
                                   (paths[1], 'console/frontend/e2e/online/graph-review.spec.js'),
                                   (paths[2], 'develop,graph,manager,service,transfer'),
                                   (paths[3], 'same_engine_control'), (paths[3], 'separate_engine_control'),
                                   (paths[0], '- neo4j'), (paths[0], 'ADDP_ONLINE_GRAPH_ENGINE: ${{ inputs.graph_engine }}'),
                                   (paths[0], "group: online-t4-${{ inputs.suite }}${{ inputs.suite == 'graph-protection' && format('-{0}', inputs.graph_engine) || '' }}"),
                                   (paths[0], "cancel-in-progress: false"),
                                   (paths[2], 'graph-control.json'), (paths[2], 'graph-review.json'), (paths[3], 'run_review'),
                                   ('scripts/test/graph-review-online.py', 'cross_graph_404=True'),
                                   ('console/frontend/e2e/online/graph-review.spec.js', 'batch_status: 500'), (paths[5], 'ports: !reset []'), (paths[4], '--volumes'), (paths[5], 'tmpfs:'),
                                   (paths[6], 'public_switch_disabled: true'), (paths[3], 'report["browser"] = run_browser('),
                                   (paths[2], 'playwright install --with-deps chromium'),
                                   (paths[1], 'console/frontend/e2e/online/graph-protection.spec.js'),
                                   (paths[3], 'report["develop_unmanaged"] = run_develop_browser('),
                                   (paths[3], 'report["develop_protected"] = run_develop_browser('),
                                   (paths[7], 'typed_parameters: true'), (paths[7], 'protected_result_absent: true'),
                                   (paths[8], '127.0.0.1:8180:8180'), (paths[9], 'COPY graph/frontend/nginx.conf'),
                                   (paths[10], 'try_files $uri $uri/ /index.html;'), (paths[6], 'theme_canvas_pixels: true'),
                                   (paths[6], 'graph-browser-diagnostics.json'), (paths[6], 'graphBrowserDiagnostic('),
                                   (paths[6], 'addp.graph-protection-browser/v7'), (paths[6], 'theme_typography: true'),
                                   (paths[6], 'graphDocumentHasThemeTypography)'),
                                   (paths[6], 'search_stable_visible: true'), (paths[6], 'expansion_stable_visible: true'), (paths[6], 'layout_stable_visible: true'), (paths[6], 'graphNodeHits('),
                                   (paths[13], 'selection_stable_visible: true'), (paths[13], 'stableEmptyReviewSelection)'),
                                   (paths[3], 'relationship_pixels'), (paths[16], 'stableRelationshipPixels'), (paths[3], 'verify_dense'), (paths[3], 'run_dense'), (paths[3], 'graph-dense-stable-frame.json'), (paths[4], 'def exploration('), (paths[16], 'denseGraphStages'), (paths[3], 'verify_search_frame'), (paths[3], 'verify_exploration'), (paths[3], 'verify_native_edges'), (paths[3], 'native_path_edges(reader, target, ids)'), (paths[6], 'nativeNodes, expected.native_edges,'), (paths[3], 'verify_pixel_frame'), (paths[11], 'verify_selection'),
                                   (paths[16], 'fillPixelCount'), (paths[6], 'count: found.nodes.length, color, fillColor'), (paths[16], 'getImageData'), (paths[16], 'exploreGraphLayouts'), (paths[16], 'clickGraphNode'), (paths[16], 'getAnimations'),
                                   (paths[8], '127.0.0.1:8186:8186'),
                                   (paths[2], 'GRAPH_URL=http://127.0.0.1:8186'),
                                   (paths[1], 'console/frontend/e2e/online/develop-graph-query.spec.js')):
            target = self.repository / relative; original = target.read_text()
            target.write_text(original.replace(fragment, 'removed'))
            with self.subTest(fragment=fragment), self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_graph_protection_profile(self.repository, {'graph-protection'})
            target.write_text(original)

        overlay = self.repository / paths[8]
        original = overlay.read_text()
        for binding in ('0.0.0.0:8186:8186', '127.0.0.1:8187:8186', '127.0.0.1:8186:8187',
                        '127.0.0.1:8186:8186", "0.0.0.0:18186:8186'):
            overlay.write_text(original.replace('127.0.0.1:8186:8186', binding))
            with self.subTest(binding=binding), self.assertRaisesRegex(CHECK.RegistrationError, 'GRAPH_URL'):
                CHECK.validate_graph_protection_profile(self.repository, {'graph-protection'})
        for owner, variable in CHECK.load_suite_registry(self.repository)['graph-protection'].services:
            service = 'gateway' if owner == 'gateway' else owner + '-backend'
            block = CHECK.compose_service_block(original, service)
            overlay.write_text(original.replace(block, ''))
            with self.subTest(missing=service), self.assertRaisesRegex(CHECK.RegistrationError, variable):
                CHECK.validate_graph_protection_profile(self.repository, {'graph-protection'})
        overlay.write_text(original)

    def test_online_aggregate_follows_focused_security_owner(self):
        makefile = "test-online-runner:\n\t@$(MAKE) test-security-online-runner\n\ntest-security-online-runner:\n\t@python3 -m unittest security_fixture_test.py\n"
        self.assertIn("security_fixture_test.py", CHECK.make_target_commands(makefile, "test-online-runner"))
        self.assertNotIn("security_fixture_test.py", CHECK.make_target_commands(makefile.replace("@$(MAKE) test-security-online-runner", "@true"), "test-online-runner"))

    def test_pg_authorization_requires_isolated_manual_profile(self):
        source = SCRIPT.parents[2]
        paths = ('.github/workflows/online-t4-gates.yml', 'scripts/test/online-gate.py',
                 'scripts/test/online-hosted-pg-authorization-gate.sh',
                 'scripts/test/pg-source-authorization-online.py', 'Makefile',
                 'console/frontend/e2e/online/pg-source-authorization.spec.js',
                 'business/scripts/online-metric-postgres-fixture.sh')
        for relative in paths:
            destination = self.repository / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source / relative, destination)
        CHECK.validate_pg_authorization_profile(self.repository, {'pg-source-authorization'})
        for relative, fragment in ((paths[0], "inputs.suite != 'pg-source-authorization'"),
                                   (paths[2], 'online-metric-postgres-fixture.sh stop'),
                                   (paths[3], 'lifecycle.cleanup()'),
                                   (paths[4], '@$(MAKE) test-pg-authorization-online-runner'),
                                   (paths[2], 'playwright install --with-deps chromium'),
                                   (paths[3], 'browser.finish()'), (paths[5], 'same_iframe_without_relogin'),
                                   (paths[3], 'self.meta_recovery_flow('), (paths[3], "with DependencyOutage('meta')"),
                                   (paths[3], 'lifecycle.catalog_flow('), (paths[2], '-meta -security -manager -catalog'),
                                   (paths[2], 'export SECURITY_URL='),
                                   (paths[2], 'export TRANSFER_SERVICE_CLIENT_SECRET='),
                                   (paths[2], 'export META_SERVICE_CLIENT_SECRET='),
                                   (paths[3], 'lifecycle.production_flow('),
                                   (paths[2], '-system -meta'), (paths[3], 'lifecycle.workspace_flow('),
                                   (paths[5], 'browser_revocation_denies_manager_read'),
                                   (paths[5], 'integrated_create_picker_without_write'),
                                   (paths[3], "with DependencyOutage('catalog')"),
                                   (paths[3], 'catalog_offline_no_direct_approval_fallback'),
                                   (paths[3], 'catalog_recovery_preserves_original_rules'),
                                   (paths[3], 'self.approval_mode_flow('),
                                   (paths[3], 'old_version_new_catalog_request_closed_without_upgrade'),
                                   (paths[3], 'self.content_change_flow('),
                                   (paths[3], 'deny_survives_content_change_and_basic_rescan'),
                                   (paths[6], 'LOCK TABLE public.metric_people'),
                                   (paths[6], 'pg-content:baseline'),
                                   (paths[2], 'export CATALOG_SERVICE_CLIENT_SECRET=')):
            path = self.repository / relative
            original = path.read_text()
            path.write_text(original.replace(fragment, 'removed'))
            with self.subTest(relative=relative), self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_pg_authorization_profile(self.repository, {'pg-source-authorization'})
            path.write_text(original)

    def test_capacity_requires_full_scene_resource_profile_and_owner_regressions(self):
        source = SCRIPT.parents[2]
        paths = ('.github/workflows/online-t4-gates.yml', 'scripts/test/online-gate.py',
                 'scripts/test/online-hosted-raster-capacity-gate.sh', 'scripts/utils/hosted-product-runtime.sh',
                 'scripts/test/raster-workflow-capacity-online.py', 'business/scripts/online-raster-capacity-fixture.py', 'Makefile', 'scripts/test/raster-workflow-online.py',
                 'system/backend/cmd/online-test-fixture/main.go', 'engines/geopython-workflow/test_online_raster_fixture.py', 'scripts/utils/capacity_resources.py')
        for relative in paths:
            destination = self.repository / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source / relative, destination)
        registered = {'raster-workflow-capacity'}
        CHECK.validate_raster_capacity_profile(self.repository, registered)
        for relative, fragment in ((paths[0], "inputs.suite == 'raster-workflow-capacity'"),
                (paths[2], '--cpus 4 --memory 2g --memory-swap 2g'),
                (paths[2], 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
                (paths[3], 'Runtime default entry mismatch'), (paths[4], 'strict_sizes(artifact,actual,item)'),
                (paths[4], 'CACHE_VALUES=(64,128,256)'), (paths[5], 'public asset checksum changed'),
                (paths[5], 'full_check=True'), (paths[6], 'scripts/test/online-raster-capacity-fixture_test.py'),
                (paths[7], 'type(value) is not int'), (paths[7], 'len(set(sizes)) != 1'),
                (paths[7], 'size_evidence'), (paths[4], 'def run_directories('),
                (paths[4], 'automatic_scans=28'), (paths[5], 'def verify_mosaic_directory('),
                (paths[8], 'case "raster-workflow-capacity":'),
                (paths[9], 'test_capacity_directory_oracle_rejects_corrupt_components_and_scope'),
                (paths[4], 'from scripts.utils import capacity_resources'),
                (paths[4], 'capacity_resources.cgroup_directory'),
                (paths[4], 'capacity_resources.read_metrics'),
                (paths[10], 'memory.peak'), (paths[10], 'oom_kill'),
                (paths[10], 'memory.swap.max'), (paths[10], 'quota/period!=4'),
                (paths[10], 'maximum!=2*(1<<30)')):
            path = self.repository / relative
            original = path.read_text()
            path.write_text(original.replace(fragment, 'removed'))
            with self.subTest(relative=relative), self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_raster_capacity_profile(self.repository, registered)
            path.write_text(original)

    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-online-registration-")
        self.repository = Path(self.temporary.name)
        (self.repository / "scripts/test").mkdir(parents=True)
        (self.repository / ".github/workflows").mkdir(parents=True)
        (self.repository / "scripts/test/online-gate.py").write_text(
            textwrap.dedent(
                """\
                from dataclasses import dataclass
                @dataclass(frozen=True)
                class Suite:
                    command: tuple[str, ...]
                    services: tuple[tuple[str, str], ...]
                SUITES = {
                    "first-suite": Suite(("first",), (("system", "SYSTEM_URL"),)),
                    "second-suite": Suite(("second",), (("gateway", "GATEWAY_URL"),)),
                }
                """
            ),
            encoding="utf-8",
        )
        (self.repository / "scripts/test/online-host-gate.sh").write_text(
            textwrap.dedent(
                """\
                case "$ONLINE_SUITE" in
                  first-suite)
                    START_TARGET=-system
                    ;;
                  second-suite)
                    START_TARGET=-model
                    ;;
                esac
                python3 scripts/test/online-preflight.py --environment-only
                printf 'database=%s\\n' "$POSTGRES_DB"
                run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
                """
            ),
            encoding="utf-8",
        )
        self.workflow = self.repository / ".github/workflows/online-t4-gates.yml"
        self.workflow.write_text(self._workflow(), encoding="utf-8")

    def tearDown(self) -> None:
        self.temporary.cleanup()

    @staticmethod
    def _workflow() -> str:
        return textwrap.dedent(
            """\
            on:
              workflow_dispatch:
                inputs:
                  suite:
                    options:
                      - first-suite
                      - second-suite
            permissions:
              contents: read
            jobs:
              online:
                if: github.event_name == 'workflow_dispatch'
                runs-on:
                  - self-hosted
                  - macOS
                  - addp-online
                environment: addp-online
                env:
                  ADDP_ONLINE_ENV_FILE: ${{ vars.ADDP_ONLINE_ENV_FILE }}
                steps:
                  - env:
                      ADDP_ONLINE_ARTIFACT_DIR: ${{ runner.temp }}/addp-online-${{ github.run_id }}
                    run: bash scripts/test/online-host-gate.sh --check-only
                  - env:
                      ADDP_ONLINE_ARTIFACT_DIR: ${{ runner.temp }}/addp-online-${{ github.run_id }}
                    run: bash scripts/test/online-host-gate.sh
                  - uses: actions/upload-artifact@0123456789abcdef0123456789abcdef01234567
            """
        )

    def test_public_origin_requires_browser_preparation_dispatch_and_source(self) -> None:
        source = Path(__file__).resolve().parents[2]
        paths = (
            "scripts/test/online-hosted-public-origin-gate.sh", "scripts/test/compose-public-origin-online.py",
            "console/frontend/e2e/online/compose-public-origin.spec.js", "system/backend/cmd/online-test-fixture/main.go",
        )
        for relative in paths:
            destination = self.repository / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source / relative, destination)
        registered = {"compose-public-origin"}
        CHECK.validate_public_origin_browser_profile(self.repository, registered)
        for relative, fragment in (
            (paths[0], "playwright install --with-deps chromium"),
            (paths[0], "up -d --no-deps --wait security-backend"),
            (paths[0], "--wait-protection-owners manager,transfer"), (paths[1], "browser = run_browser()"),
            (paths[2], "ADDP_ONLINE_PUBLIC_ORIGIN_BROWSER_REPORT"), (paths[3], 'values["ADDP_ONLINE_READ_USER_PASSWORD"]'),
        ):
            path = self.repository / relative
            original = path.read_text()
            path.write_text(original.replace(fragment, "removed"))
            with self.subTest(relative=relative), self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_public_origin_browser_profile(self.repository, registered)
            path.write_text(original)
        (self.repository / paths[2]).unlink()
        with self.assertRaises(CHECK.RegistrationError):
            CHECK.validate_public_origin_browser_profile(self.repository, registered)

    def test_accepts_one_profile_and_workflow_choice_per_registered_suite(self) -> None:
        CHECK.check_registration(self.repository)

    def test_hdfs_requires_manual_java11_lifecycle_browser_and_owner_tests(self) -> None:
        source = SCRIPT.parents[2]
        paths = (
            "scripts/test/online-hosted-hdfs-gate.sh", "scripts/test/online-gate.py",
            "business/scripts/online-hdfs-spark-fixture.sh", "scripts/test/hdfs-spark-consumer-flow-online.py",
            "console/frontend/e2e/online/hdfs-spark-consumer-flow.spec.js", "Makefile",
            ".github/workflows/online-t4-gates.yml", "scripts/test/spark-online-evidence.py", "scripts/utils/hosted-online.sh", "scripts/utils/hosted-product-runtime.sh",
            'scripts/test/spark-table-consumer-online.py', 'business/scripts/online-spark-table-oracle.py',
        )
        for relative in paths:
            destination = self.repository / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source / relative, destination)
        registered = {"hdfs-spark-consumer-flow"}
        CHECK.validate_hdfs_spark_profile(self.repository, registered)
        self.assertIn(('security', 'SECURITY_URL'), CHECK.load_suite_registry(self.repository)['hdfs-spark-consumer-flow'].services)
        for relative, fragment in (
            (paths[0], 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
            (paths[0], '-security'), (paths[0], '--wait-protection-owners develop,manager'),
            (paths[0], 'SECURITY_URL=http://127.0.0.1:8194'),
            (paths[0], 'start_online_spark_runtime online-hdfs hdfs'),
            (paths[8], 'make build-images'), (paths[9], '${RUNTIME_REPORT}-runtime-build.txt'),
            (paths[8], '${RUNTIME_REPORT}-runtime.log'), (paths[8], 'com.addp.online-runtime'),
            (paths[2], 'refusing to delete a foreign container'),
            (paths[0], 'ADDP_ONLINE_DORIS_ENGINE_ID'), (paths[2], 'addp-hdfs-online-mysql'),
            (paths[3], 'TABLES.cleanup'), (paths[10], 'physical_after'), (paths[11], 'INNODB_TABLES'),
            (paths[3], 'SPARK.worker_evidence'), (paths[7], 'Finished task'), (paths[4], 'login('),
            (paths[4], "getByRole('dialog'"), (paths[4], 'Confirm and start scan'),
            (paths[4], 'postDataJSON()'), (paths[4], "catalog_paths: ['samples']"),
            (paths[4], 'The selected scope has been scanned'),
            (paths[5], '$(MAKE) test-hdfs-online-runner'),
            (paths[6], "java-version: '11'"),
            (paths[6], "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'hdfs-spark-consumer-flow'\n"),
        ):
            path = self.repository / relative
            original = path.read_text()
            path.write_text(original.replace(fragment, 'removed'))
            with self.subTest(relative=relative, fragment=fragment), self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_hdfs_spark_profile(self.repository, registered)
            path.write_text(original)

        registry = self.repository / paths[1]
        original = registry.read_text()
        registry.write_text(original.replace('("security", "SECURITY_URL"),', ''))
        with self.assertRaisesRegex(CHECK.RegistrationError, 'Security'):
            CHECK.validate_hdfs_spark_profile(self.repository, registered)
        registry.write_text(original)

    def test_elasticsearch_spark_requires_product_container_worker_and_console_evidence(self) -> None:
        source = SCRIPT.parents[2]
        paths = ('.github/workflows/online-t4-gates.yml', 'scripts/test/online-hosted-elasticsearch-gate.sh',
                 'scripts/test/elasticsearch-consumer-flow-online.py', 'scripts/test/spark-online-evidence.py',
                 'console/frontend/e2e/online/elasticsearch-consumer-flow.spec.js', 'Makefile', 'scripts/utils/hosted-online.sh', 'scripts/utils/hosted-product-runtime.sh')
        for relative in paths:
            destination = self.repository / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source / relative, destination)
        CHECK.validate_elasticsearch_spark_profile(self.repository, {'elasticsearch-consumer-flow'})
        for relative, fragment in ((paths[6], '--network host'), (paths[1], 'start_online_spark_runtime online-elasticsearch elasticsearch'),
                                   (paths[2], 'validate_workflow_nodes'), (paths[3], 'Finished task'),
                                   (paths[4], '.workflow-final-result-json'), (paths[5], '$(MAKE) test-elasticsearch-online-runner')):
            path = self.repository / relative
            original = path.read_text()
            path.write_text(original.replace(fragment, 'removed'))
            with self.subTest(relative=relative), self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_elasticsearch_spark_profile(self.repository, {'elasticsearch-consumer-flow'})
            path.write_text(original)

    def test_hosted_setup_node_pin_matches_platform_ci(self) -> None:
        repository = SCRIPT.parents[2]
        online = (
            repository / ".github/workflows/online-t4-gates.yml"
        ).read_text(encoding="utf-8")
        platform = (
            repository / ".github/workflows/platform-ci.yml"
        ).read_text(encoding="utf-8")

        def setup_node_pin(text: str) -> str:
            match = re.search(r"actions/setup-node@([0-9a-f]{40})", text)
            self.assertIsNotNone(match)
            return match.group(1)  # type: ignore[union-attr]

        self.assertEqual(setup_node_pin(online), setup_node_pin(platform))

    def test_metric_engine_variant_is_explicitly_wired_to_hosted_job(self) -> None:
        actual = (SCRIPT.parents[2] / ".github/workflows/online-t4-gates.yml").read_text(encoding="utf-8")
        self.workflow.write_text(actual, encoding="utf-8")
        registered = {"metric-service-revision-lifecycle"}
        CHECK.validate_metric_engine_variant(self.repository, registered)
        for missing in ("        default: postgresql\n", "          - tidb\n",
                        "      fail-fast: false\n",
                        "        metric_engine: ${{ fromJSON(github.event_name == 'schedule' && '[\"postgresql\",\"tidb\"]' || format('[\"{0}\"]', inputs.metric_engine)) }}\n",
                        "      group: online-t4-metric-service-revision-lifecycle-${{ matrix.metric_engine }}\n",
                        "      ADDP_ONLINE_METRIC_ENGINE_TYPE: ${{ matrix.metric_engine }}\n"):
            with self.subTest(missing=missing):
                self.workflow.write_text(actual.replace(missing, "", 1), encoding="utf-8")
                with self.assertRaises(CHECK.RegistrationError):
                    CHECK.validate_metric_engine_variant(self.repository, registered)

    def test_discovers_hosted_profile_from_metadata(self) -> None:
        hosted = self.repository / "scripts/test/online-hosted-example-gate.sh"
        hosted.write_text(
            "# ADDP_ONLINE_SUITES=hosted-suite\n"
            "# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64\n",
            encoding="utf-8",
        )

        profiles = CHECK.load_deployment_profiles(self.repository)

        self.assertEqual(
            profiles["hosted-suite"], "github-hosted-linux-x86_64"
        )

    def test_node_metrics_requires_owned_regressions_and_manual_hosted_dispatch(self):
        root = SCRIPT.parents[2]
        paths = (
            "scripts/test/online-hosted-node-metrics-gate.sh", "scripts/test/platform-node-metrics-online.py",
            "scripts/test/platform-node-metrics-online_test.py", "scripts/test/platform-node-metrics-fixture.py",
            "scripts/test/platform-node-metrics-fixture_test.py", "scripts/test/online-hosted-node-metrics-gate_test.py",
            "system/backend/cmd/online-test-fixture/platform_metrics.go",
            "system/backend/cmd/online-test-fixture/platform_metrics_test.go", "scripts/infra/node-metrics.py",
            "Makefile", ".github/workflows/online-t4-gates.yml",
        )
        for relative in paths:
            target = self.repository / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(root / relative, target)
        CHECK.validate_platform_node_metrics_profile(self.repository, {"platform-node-metrics"})
        gate = self.repository / "scripts/test/online-hosted-node-metrics-gate.sh"
        original_gate = gate.read_text()
        for fragment in ('modules=(-monitor)', 'modules+=(-system)', 'scripts/dev/start.sh "${modules[@]}"'):
            gate.write_text(original_gate.replace(fragment, "removed"))
            with self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_platform_node_metrics_profile(self.repository, {"platform-node-metrics"})
        gate.write_text(original_gate)
        original = self.workflow.read_text()
        for changed in (
            original.replace("github.event_name == 'workflow_dispatch' && inputs.suite == 'platform-node-metrics'", "github.event_name == 'schedule'"),
            original.replace("&& inputs.suite != 'platform-node-metrics'", ""),
            original.replace("      node_metrics_case:\n", "      removed_metrics_case:\n"),
            original.replace("          - cpu-pressure\n", "          - unbounded-pressure\n"),
            original.replace("          - host-admission\n", ""),
            original.replace("          - meta-workers\n", ""),
            original.replace("          - transfer-workers\n", ""),
            original.replace("          - quality-workers\n", ""),
            original.replace("ADDP_ONLINE_NODE_METRICS_CASE: ${{ inputs.node_metrics_case }}", "ADDP_ONLINE_NODE_METRICS_CASE: regression"),
        ):
            self.workflow.write_text(changed)
            with self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_platform_node_metrics_profile(self.repository, {"platform-node-metrics"})
        self.workflow.write_text(original)
        makefile = self.repository / "Makefile"
        original = makefile.read_text()
        makefile.write_text(original.replace("$(MAKE) test-node-metrics-online-runner", ""))
        with self.assertRaises(CHECK.RegistrationError):
            CHECK.validate_platform_node_metrics_profile(self.repository, {"platform-node-metrics"})
        makefile.write_text(original)
        (self.repository / "scripts/test/platform-node-metrics-online_test.py").unlink()
        with self.assertRaises(CHECK.RegistrationError):
            CHECK.validate_platform_node_metrics_profile(self.repository, {"platform-node-metrics"})

    def prepare_orchestrator_profile(self):
        root = SCRIPT.parents[2]
        for relative in (
            "scripts/test/online-hosted-orchestrator-gate.sh",
            "scripts/test/orchestrator-execution-online.py",
            "scripts/test/orchestrator-execution-online_test.py",
            "scripts/test/orchestrator-execution-faults.py",
            "scripts/test/orchestrator-execution-faults_test.py",
            "scripts/test/online-hosted-orchestrator-gate_test.py",
            "business/scripts/online-metric-postgres-fixture.sh",
            "Makefile",
            ".github/workflows/online-t4-gates.yml",
        ):
            target = self.repository / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(root / relative, target)

    def test_orchestrator_profile_is_hosted_manual_and_uses_real_owners(self):
        self.prepare_orchestrator_profile()
        CHECK.validate_orchestrator_execution_profile(self.repository, {"orchestrator-execution"})
        gate = self.repository / "scripts/test/online-hosted-orchestrator-gate.sh"
        gate.write_text(gate.read_text().replace("-meta -orchestrator -monitor", "-orchestrator"))
        with self.assertRaisesRegex(CHECK.RegistrationError, "-meta -orchestrator -monitor"):
            CHECK.validate_orchestrator_execution_profile(self.repository, {"orchestrator-execution"})

    def test_orchestrator_fault_tests_cannot_be_removed_from_the_owner_entry(self):
        self.prepare_orchestrator_profile()
        makefile = self.repository / "Makefile"
        text = makefile.read_text()
        for missing in ("$(MAKE) test-orchestrator-online-runner", "scripts/test/orchestrator-execution-faults_test.py"):
            with self.subTest(missing=missing):
                makefile.write_text(text.replace(missing, ""))
                with self.assertRaises(CHECK.RegistrationError):
                    CHECK.validate_orchestrator_execution_profile(self.repository, {"orchestrator-execution"})

    def test_orchestrator_cannot_start_nightly_or_also_target_personal_runner(self):
        self.prepare_orchestrator_profile()
        original = self.workflow.read_text()
        manual = "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'orchestrator-execution'"
        self.workflow.write_text(original.replace(manual, "    if: github.event_name == 'schedule'"))
        with self.assertRaisesRegex(CHECK.RegistrationError, "manual Hosted"):
            CHECK.validate_orchestrator_execution_profile(self.repository, {"orchestrator-execution"})
        self.workflow.write_text(original.replace("&& inputs.suite != 'orchestrator-execution'", ""))
        with self.assertRaisesRegex(CHECK.RegistrationError, "self-hosted"):
            CHECK.validate_orchestrator_execution_profile(self.repository, {"orchestrator-execution"})

    def test_discovers_owner_managed_profile_from_metadata(self) -> None:
        owner_managed = self.repository / "scripts/test/online-owner-managed-example-gate.sh"
        owner_managed.write_text(
            "# ADDP_ONLINE_SUITES=licensed-suite\n"
            "# ADDP_ONLINE_RUNNER=owner-managed-linux-x86_64\n",
            encoding="utf-8",
        )

        profiles = CHECK.load_deployment_profiles(self.repository)

        self.assertEqual(profiles["licensed-suite"], "owner-managed-linux-x86_64")

    def test_rejects_hosted_profile_without_runner_metadata(self) -> None:
        hosted = self.repository / "scripts/test/online-hosted-example-gate.sh"
        hosted.write_text(
            "# ADDP_ONLINE_SUITES=hosted-suite\n", encoding="utf-8"
        )

        with self.assertRaisesRegex(CHECK.RegistrationError, "Hosted Online metadata"):
            CHECK.load_deployment_profiles(self.repository)

    def test_rejects_missing_deployment_profile(self) -> None:
        script = self.repository / "scripts/test/online-host-gate.sh"
        script.write_text(
            script.read_text(encoding="utf-8").replace(
                "  second-suite)\n    START_TARGET=-model\n    ;;\n", ""
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "do not match"):
            CHECK.check_registration(self.repository)

    def test_rejects_nightly_schedule_before_first_real_run(self) -> None:
        self.workflow.write_text(
            self._workflow().replace(
                "  workflow_dispatch:\n", "  schedule:\n    - cron: '0 1 * * *'\n  workflow_dispatch:\n"
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "must remain manual"):
            CHECK.check_registration(self.repository)

    def test_rejects_self_hosted_job_without_manual_dispatch_guard(self) -> None:
        self.workflow.write_text(
            self._workflow().replace(
                "    if: github.event_name == 'workflow_dispatch'\n", ""
            ),
            encoding="utf-8",
        )

        with self.assertRaisesRegex(
            CHECK.RegistrationError, "workflow_dispatch or a fixed schedule"
        ):
            CHECK.check_registration(self.repository)

    def test_rejects_self_hosted_job_with_repository_secret(self) -> None:
        self.workflow.write_text(
            self._workflow().replace(
                "      ADDP_ONLINE_ENV_FILE: ${{ vars.ADDP_ONLINE_ENV_FILE }}\n",
                "      ADDP_ONLINE_ENV_FILE: ${{ vars.ADDP_ONLINE_ENV_FILE }}\n"
                "      FORBIDDEN_SECRET: ${{ secrets.FORBIDDEN_SECRET }}\n",
            ),
            encoding="utf-8",
        )

        with self.assertRaisesRegex(
            CHECK.RegistrationError, "must not receive repository secrets"
        ):
            CHECK.check_registration(self.repository)

    def test_rejects_self_hosted_job_with_unpinned_action(self) -> None:
        self.workflow.write_text(
            self._workflow().replace(
                "actions/upload-artifact@0123456789abcdef0123456789abcdef01234567",
                "actions/upload-artifact@v7",
            ),
            encoding="utf-8",
        )

        with self.assertRaisesRegex(CHECK.RegistrationError, "immutable commit SHA"):
            CHECK.check_registration(self.repository)

    def test_rejects_online_workflow_with_write_token(self) -> None:
        self.workflow.write_text(
            self._workflow().replace("contents: read", "contents: write"),
            encoding="utf-8",
        )

        with self.assertRaisesRegex(CHECK.RegistrationError, "permissions"):
            CHECK.check_registration(self.repository)

    def test_rejects_graduated_nightly_suite_without_workflow_schedule(self) -> None:
        gate = self.repository / "scripts/test/online-gate.py"
        gate.write_text(
            gate.read_text(encoding="utf-8")
            .replace(
                "    services: tuple[tuple[str, str], ...]\n",
                "    services: tuple[tuple[str, str], ...]\n"
                "    nightly: bool = False\n",
            )
            .replace(
                '"second-suite": Suite(("second",), (("gateway", "GATEWAY_URL"),)),',
                '"second-suite": Suite('
                '("second",), (("gateway", "GATEWAY_URL"),), nightly=True),',
            ),
            encoding="utf-8",
        )

        with self.assertRaisesRegex(CHECK.RegistrationError, "nightly suite"):
            CHECK.check_registration(self.repository)

    def test_rejects_nightly_job_without_fixed_suite_concurrency(self) -> None:
        gate = self.repository / "scripts/test/online-gate.py"
        gate.write_text(
            gate.read_text(encoding="utf-8")
            .replace(
                "    services: tuple[tuple[str, str], ...]\n",
                "    services: tuple[tuple[str, str], ...]\n"
                "    nightly: bool = False\n",
            )
            .replace(
                '"second-suite": Suite(("second",), (("gateway", "GATEWAY_URL"),)),',
                '"second-suite": Suite('
                '("second",), (("gateway", "GATEWAY_URL"),), nightly=True),',
            ),
            encoding="utf-8",
        )
        self.workflow.write_text(
            self._workflow()
            .replace(
                "  workflow_dispatch:\n",
                "  schedule:\n    - cron: '0 1 * * *'\n  workflow_dispatch:\n",
            )
            .replace(
                "  online:\n",
                "  online:\n"
                "    if: github.event_name == 'schedule'\n"
                "    env:\n"
                "      ONLINE_SUITE_INPUT: second-suite\n",
            ),
            encoding="utf-8",
        )

        with self.assertRaisesRegex(CHECK.RegistrationError, "concurrency group"):
            CHECK.check_registration(self.repository)

    def test_repository_schedules_graduated_hosted_profiles(self) -> None:
        repository = SCRIPT.parents[2]

        registry = CHECK.load_suite_registry(repository)
        nightly = CHECK.load_nightly_suites(registry)

        self.assertEqual(
            nightly,
            {
                "compose-public-origin",
                "opengauss-consumer-flow",
                "metric-service-revision-lifecycle",
            },
        )
        CHECK.check_registration(repository)

    def test_rejects_workflow_without_readiness_check(self) -> None:
        self.workflow.write_text(
            self._workflow().replace(
                "      - env:\n"
                "          ADDP_ONLINE_ARTIFACT_DIR: "
                "${{ runner.temp }}/addp-online-${{ github.run_id }}\n"
                "        run: bash scripts/test/online-host-gate.sh --check-only\n",
                "",
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "--check-only"):
            CHECK.check_registration(self.repository)

    def test_rejects_runner_context_in_job_level_environment(self) -> None:
        self.workflow.write_text(
            self._workflow().replace(
                "    steps:\n",
                "    env:\n"
                "      ADDP_ONLINE_ARTIFACT_DIR: ${{ runner.temp }}/invalid\n"
                "    steps:\n",
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "job-level env"):
            CHECK.check_registration(self.repository)

    def test_requires_artifact_directory_on_both_lifecycle_steps(self) -> None:
        self.workflow.write_text(
            self._workflow().replace(
                "      - env:\n"
                "          ADDP_ONLINE_ARTIFACT_DIR: "
                "${{ runner.temp }}/addp-online-${{ github.run_id }}\n"
                "        run: bash scripts/test/online-host-gate.sh\n",
                "      - run: bash scripts/test/online-host-gate.sh\n",
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "both lifecycle steps"):
            CHECK.check_registration(self.repository)

    def test_rejects_host_gate_without_shared_environment_preflight(self) -> None:
        script = self.repository / "scripts/test/online-host-gate.sh"
        script.write_text(
            script.read_text(encoding="utf-8").replace(
                "python3 scripts/test/online-preflight.py --environment-only\n", ""
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "environment-only"):
            CHECK.check_registration(self.repository)

    def test_rejects_module_registry_suite_without_formal_process_profile(self) -> None:
        gate = self.repository / "scripts/test/online-gate.py"
        gate.write_text(
            gate.read_text(encoding="utf-8").replace(
                '"first-suite"', '"module-registry-recovery"'
            ),
            encoding="utf-8",
        )
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(
            host.read_text(encoding="utf-8").replace(
                "first-suite", "module-registry-recovery"
            ),
            encoding="utf-8",
        )
        self.workflow.write_text(
            self.workflow.read_text(encoding="utf-8").replace(
                "first-suite", "module-registry-recovery"
            ),
            encoding="utf-8",
        )

        with self.assertRaisesRegex(CHECK.RegistrationError, "process profile is missing"):
            CHECK.check_registration(self.repository)

    def test_module_profile_requires_force_stage_and_observer_configuration(self):
        source = Path(__file__).resolve().parents[2]
        paths = ("scripts/test/online-host-gate.sh", "scripts/dev/stop-exact-process.sh",
                 "scripts/test/module-lifecycle-process-online.py", "scripts/dev/start.sh")
        for relative in paths:
            target = self.repository / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text((source / relative).read_text())
        CHECK.validate_module_registry_process_profile(self.repository, {"module-registry-recovery"})
        host = self.repository / paths[0]
        original = host.read_text()
        for fragment in ("bash scripts/dev/stop-exact-process.sh --force -manager",
                         "observe_module_lifecycle manager-restarted",
                         "ADDP_ONLINE_TEST_PLATFORM_ACCESS_TOKEN", "ADDP_HOST_NODE_IPS"):
            with self.subTest(fragment=fragment):
                host.write_text(original.replace(fragment, "REMOVED"))
                with self.assertRaisesRegex(CHECK.RegistrationError, "process profile is missing"):
                    CHECK.validate_module_registry_process_profile(self.repository, {"module-registry-recovery"})
        host.write_text(original)

    def test_requires_consumer_engine_recovery_lifecycle_and_browser_assets(self) -> None:
        gate = self.repository / "scripts/test/online-gate.py"
        gate.write_text(
            gate.read_text(encoding="utf-8").replace(
                '"first-suite"', '"consumer-engine-recovery"'
            ),
            encoding="utf-8",
        )
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(
            host.read_text(encoding="utf-8").replace(
                "first-suite", "consumer-engine-recovery"
            )
            + "\nbash business/scripts/online-engine-fixture.sh start\n"
            + "bash business/scripts/online-engine-fixture.sh stop\n"
            + "bash scripts/dev/start.sh\n"
            + "playwright install chromium\n"
            + "python3 scripts/test/consumer-process-stability-online.py\n"
            + "python3 scripts/test/consumer-engine-recovery-online.py --restore-only\n",
            encoding="utf-8",
        )
        self.workflow.write_text(
            self.workflow.read_text(encoding="utf-8").replace(
                "first-suite", "consumer-engine-recovery"
            ),
            encoding="utf-8",
        )
        required = (
            "business/scripts/online-engine-fixture.sh",
            "scripts/test/consumer-engine-recovery-online.py",
            "scripts/test/consumer-process-stability-online.py",
            "console/frontend/playwright.online.config.js",
            "console/frontend/e2e/online/consumer-engine-recovery.spec.js",
        )
        for relative in required:
            path = self.repository / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            content = "ADDP_ONLINE_HOST --env-file /dev/null business-postgres\n" if relative.startswith("business/") else "fixture\n"
            path.write_text(content, encoding="utf-8")

        CHECK.check_registration(self.repository)
        host.write_text(
            host.read_text(encoding="utf-8").replace(
                "python3 scripts/test/consumer-engine-recovery-online.py --restore-only\n",
                "",
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "process profile is missing"):
            CHECK.check_registration(self.repository)

    def test_requires_workbench_mysql_fixture_and_owner_suite(self) -> None:
        gate = self.repository / "scripts/test/online-gate.py"
        gate.write_text(
            gate.read_text(encoding="utf-8").replace(
                '"first-suite"', '"workbench-service-consumption"'
            ),
            encoding="utf-8",
        )
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(
            host.read_text(encoding="utf-8").replace(
                "first-suite)\n    START_TARGET=-system",
                "workbench-service-consumption)\n    START_TARGET=-all",
            )
            + "\nSYSTEM_URL GATEWAY_URL SERVICE_URL WORKBENCH_URL CONSOLE_URL\n"
            + "ADDP_ONLINE_TEST_USER_USERNAME ADDP_ONLINE_TEST_USER_PASSWORD\n"
            + "ADDP_ONLINE_TEST_TENANT_ADMIN_ACCESS_TOKEN\n"
            + "ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID\n"
            + "bash business/scripts/online-workbench-mysql-fixture.sh start\n"
            + "bash business/scripts/online-workbench-mysql-fixture.sh stop\n"
            + 'bash scripts/dev/start.sh "$START_TARGET"\n'
            + "playwright install chromium\n",
            encoding="utf-8",
        )
        self.workflow.write_text(
            self.workflow.read_text(encoding="utf-8").replace(
                "first-suite", "workbench-service-consumption"
            ),
            encoding="utf-8",
        )
        fixture = self.repository / "business/scripts/online-workbench-mysql-fixture.sh"
        fixture.parent.mkdir(parents=True, exist_ok=True)
        fixture.write_text(
            "ADDP_ONLINE_HOST --env-file /dev/null business-mysql "
            "REVOKE ALL PRIVILEGES, GRANT OPTION GRANT SELECT ON\n",
            encoding="utf-8",
        )
        owner = self.repository / "scripts/test/workbench-service-consumption-online.py"
        owner.write_text("fixture\n", encoding="utf-8")
        for relative in (
            "console/frontend/playwright.online.config.js",
            "console/frontend/e2e/online/workbench-service-consumption.spec.js",
        ):
            path = self.repository / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("fixture\n", encoding="utf-8")

        CHECK.check_registration(self.repository)
        owner.unlink()
        with self.assertRaisesRegex(CHECK.RegistrationError, "requires"):
            CHECK.check_registration(self.repository)

    def test_plaintext_hosted_contract_rejects_drift_and_old_route(self):
        root = SCRIPT.parents[2]
        paths = ("scripts/test/online-hosted-security-gate.sh", "scripts/test/online-host-gate.sh",
                 ".github/workflows/online-t4-gates.yml", "Makefile", "business/scripts/online-security-owner-fixture.sh",
                 "scripts/test/security-plaintext-access-online.py", "system/backend/cmd/online-test-fixture/main.go",
                 "console/frontend/e2e/online/security-plaintext-access.spec.js",
                 "scripts/test/security-transfer-protection-online.py")
        for relative in paths:
            target = self.repository / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text((root / relative).read_text())
        registered = {"security-plaintext-access"}
        CHECK.validate_security_plaintext_access_profile(self.repository, registered)
        for relative, fragment in ((paths[0], "# ADDP_ONLINE_SUITES=security-plaintext-access"),
                                   (paths[5], "review-queue?scope=pending"),
                                   (paths[5], "revoked_applicant"),
                                   (paths[5], 'addp.security-plaintext-access-online/v6'),
                                   (paths[5], 'validate_develop_scope_report('),
                                   (paths[5], 'same_history_masked_at_expiry'),
                                   (paths[5], 'same_history_masked_at_revocation'),
                                   (paths[5], 'manager_grant_independent'),
                                   (paths[7], 'multi_outlet_request_verified'),
                                   (paths[5], 'outage = exercise_security_unavailability('),
                                   (paths[5], 'run_security_lifecycle(repository, "stop", deadline)'),
                                   (paths[5], 'run_security_lifecycle(repository, "start", deadline)'),
                                   (paths[5], 'connection_refusals'),
                                   (paths[8], 'def require_hosted_security_lifecycle('),

                                   (paths[6], "plaintextApproverPermissions"),
                                   (paths[6], "ADDP_ONLINE_TEST_APPROVER_PASSWORD"),
                                   (paths[5], 'report["browser"] = run_plaintext_browser('),
                                   (paths[7], "revocation_from_security_ui"),
                                   (paths[7], "expired_history_verified"),
                                   (paths[7], "target.projection_acknowledged === true"),
                                   (paths[7], "await synchronized"),
                                   (paths[7], "await secondSynchronized"),
                                   (paths[7], "state.started = true"),
                                   (paths[2], "&& inputs.suite != 'security-plaintext-access'")):
            with self.subTest(fragment=fragment):
                target = self.repository / relative
                original = target.read_text()
                target.write_text(original.replace(fragment, "REMOVED"))
                try:
                    with self.assertRaises(CHECK.RegistrationError):
                        CHECK.validate_security_plaintext_access_profile(self.repository, registered)
                finally:
                    target.write_text(original)
        profiles = CHECK.load_deployment_profiles(self.repository)
        self.assertEqual(profiles['security-plaintext-access'], 'github-hosted-linux-x86_64')
        self.assertEqual(profiles['security-mysql-owner-protection'], 'github-hosted-linux-x86_64')
        host = self.repository / paths[1]
        host.write_text(host.read_text() + "\nsecurity-plaintext-access)\n")
        with self.assertRaisesRegex(CHECK.RegistrationError, 'self-hosted route'):
            CHECK.validate_security_plaintext_access_profile(self.repository, registered)

    def test_requires_mysql_four_owner_protection_fixtures_and_contract(self) -> None:
        root = SCRIPT.parents[2]
        for relative in (
            "scripts/test/online-hosted-security-gate.sh", "scripts/test/online-host-gate.sh",
            ".github/workflows/online-t4-gates.yml", "Makefile",
            "business/scripts/online-security-owner-fixture.sh",
            "scripts/test/security-mysql-owner-protection-online.py",
            "scripts/test/security-transfer-protection-online.py",
            "scripts/test/online-gate.py",
            "console/frontend/e2e/online/security-manager-export.spec.js",
        ):
            target = self.repository / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(root / relative, target)
        registered = {"security-mysql-owner-protection"}
        CHECK.validate_security_mysql_owner_protection_profile(self.repository, registered)
        for relative, fragment, message in (
            ("scripts/test/security-mysql-owner-protection-online.py", '"rollback_verified"', "owner contract is missing"),
            ("scripts/test/online-hosted-security-gate.sh", "playwright install --with-deps chromium", "Hosted profile is missing"),
            ("scripts/test/online-hosted-security-gate.sh", 'go work init "${go_modules[@]}"', "Hosted profile is missing"),
            ("scripts/test/online-hosted-security-gate.sh", 'cp "$ROOT_DIR/go.work.sum" "${GOWORK}.sum"', "Hosted profile is missing"),
            ("scripts/test/online-hosted-security-gate.sh", 'GOMODCACHE="$ROOT_DIR/.gomodcache"', "Hosted profile is missing"),
            ("scripts/test/online-hosted-security-gate.sh", "-transfer -graph -monitor", "Hosted profile is missing"),
            ("scripts/test/online-gate.py", '("monitor", "MONITOR_URL"),', "must preflight Monitor"),
            ("scripts/test/security-mysql-owner-protection-online.py", "run_export_browser(", "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", "wait_for_technical_field(", "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", "SUPPORT.require_hosted_security_lifecycle(", "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", "exercise_restart_recovery(", "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"verified_before_rescan"', "must dispatch and validate"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "verifyDevelopProtectionRefusal(", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "develop_refusal_diagnostic_verified", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "verifyServiceTransferProtectionRefusal(", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "verifyTransferResultProtection(", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "transfer_result_target_unchanged", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "transfer_result_baseline_restored", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "security-transfer-result-monitor.png", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "service_old_preview_cleared", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "output_protection_permanent_restored", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "output_browser_residual_resources", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "security-transfer-protection-monitor.png", "export browser contract is missing"),
            ("scripts/test/security-mysql-owner-protection-online.py", "validate_develop_protection_refusal(", "must dispatch and validate"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "technical_field_search_verified", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "verifyResourceCombinationFilters(", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "resource_filters_read_only", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "browser_session_rebound_verified", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "security-manager-field-search.png", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "execution.triggered_by", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "download.delete()", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "format: 'csv'", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "csv.DictReader", "export browser contract is missing"),
            ("business/scripts/online-security-owner-fixture.sh", "container ownership mismatch", "fixture contract is missing"),
            ("business/scripts/online-security-owner-fixture.sh", "fixture.Persons.insertMany", "fixture contract is missing"),
            ("scripts/test/security-mysql-owner-protection-online.py", "exercise_mongodb_algorithms(", "owner contract is missing"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"mongodb_algorithms"', "owner contract is missing"),
            ("scripts/test/security-mysql-owner-protection-online.py", "verify_mongodb_raw_export(", "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", "verify_mongodb_query_transfer(", "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", "validate_transfer_protection_refusal(", "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", "validate_service_protection_refusal(", "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"target_empty_before_and_after"', "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"query_output_lineage_unresolved"', "must dispatch and validate"),
            ("business/scripts/online-security-owner-fixture.sh", "addp_online_security.mongodb_query_denied", "fixture contract is missing"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"addp.security-mysql-owner-protection-online/v25"', "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"develop_wildcard"', "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"develop_mixed_wildcard"', "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"develop_page_wildcard"', "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"service_sql_page"', "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"computed_outputs"', "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"history_result_absent"', "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"protection_computed_output_unsupported"', "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"protection_lineage_unverified"', "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"protection_expired"', "must dispatch and validate"),
            ('scripts/test/security-mysql-owner-protection-online.py', 'develop_refusal_with_history(', 'must dispatch and validate'),
            ('scripts/test/security-mysql-owner-protection-online.py', 'def protection_reason_matrix(', 'must dispatch and validate'),
            ('scripts/test/security-mysql-owner-protection-online.py', 'bank_outlet_refusals(', 'must dispatch and validate'),
            ('scripts/test/security-mysql-owner-protection-online.py', 'transfer_refusal_with_history(', 'must dispatch and validate'),
            ('scripts/test/security-mysql-owner-protection-online.py', 'validate_manager_protection_refusal(', 'must dispatch and validate'),
            ('scripts/test/security-mysql-owner-protection-online.py', '"protection_reason_matrix"', 'must dispatch and validate'),
            ('scripts/test/security-mysql-owner-protection-online.py', '"protocol_only"', 'must dispatch and validate'),
            ('scripts/test/security-mysql-owner-protection-online.py', '"coverage_counts"', 'must dispatch and validate'),
            ('scripts/test/security-mysql-owner-protection-online.py', 'exercise_enrolling_refusal(', 'must dispatch and validate'),
            ('scripts/test/security-mysql-owner-protection-online.py', 'exercise_bank_card_refusal_causes(', 'must dispatch and validate'),
            ('scripts/test/security-mysql-owner-protection-online.py', 'run_bank_schema_fixture(', 'must dispatch and validate'),
            ('scripts/test/security-mysql-owner-protection-online.py', '"protection_not_ready"', 'must dispatch and validate'),
            ('scripts/test/security-mysql-owner-protection-online.py', '"protection_schema_mismatch"', 'must dispatch and validate'),
            ('scripts/test/security-mysql-owner-protection-online.py', '"protection_operation_unsupported"', 'must dispatch and validate'),
            ('scripts/test/security-mysql-owner-protection-online.py', '"refusal_causes"', 'must dispatch and validate'),
            ('scripts/test/security-mysql-owner-protection-online.py', '"schema_restored"', 'must dispatch and validate'),
            ('scripts/test/security-mysql-owner-protection-online.py', '"additional_refusals"', 'must dispatch and validate'),
            ('console/frontend/e2e/online/security-manager-export.spec.js', 'verifyAdditionalProtectionHistories(', 'export browser contract is missing'),
            ('console/frontend/e2e/online/security-manager-export.spec.js', 'additional_protection_histories_verified', 'export browser contract is missing'),
            ('console/frontend/e2e/online/security-manager-export.spec.js', 'additional_protection_monitors_verified', 'export browser contract is missing'),
            ('console/frontend/e2e/online/security-manager-export.spec.js', 'additional_protection_refusal_count', 'export browser contract is missing'),
            ('business/scripts/online-security-owner-fixture.sh', 'bank-schema-add|bank-schema-remove', 'fixture contract is missing'),
            ('business/scripts/online-security-owner-fixture.sh', '_security_drift_probe', 'fixture contract is missing'),
            ('business/scripts/online-security-owner-fixture.sh', 'ADD COLUMN', 'fixture contract is missing'),
            ('business/scripts/online-security-owner-fixture.sh', 'DROP COLUMN', 'fixture contract is missing'),
            ("scripts/test/security-mysql-owner-protection-online.py", '"SELECT b.* FROM "', "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"independent_outlet_fields"', "must dispatch and validate"),
            ("business/scripts/online-security-owner-fixture.sh", "CREATE TABLE bank_card_accounts", "fixture contract is missing"),
            ("business/scripts/online-security-owner-fixture.sh", "addp_online_security.bank_card_transfer", "fixture contract is missing"),
            ("scripts/test/security-mysql-owner-protection-online.py", "exercise_bank_card_metadata(", "owner contract is missing"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"negative_fields_not_detected"', "owner contract is missing"),
            ("scripts/test/security-mysql-owner-protection-online.py", "exercise_explicit_rediscovery(", "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"explicit_rediscovery"', "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"all_current_revisions_acknowledged"', "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", '"security.enrollment.update"', "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", 'outage = exercise_owner_unavailability(', "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", 'case["security_unavailability"] = exercise_owner_unavailability(', "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", 'owner_processes_unchanged', "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", 'diagnostics.get("code") != "query_output_lineage_unresolved"', "must dispatch and validate"),
            ("scripts/test/security-mysql-owner-protection-online.py", 'SUPPORT.run_security_lifecycle(repository, "stop", deadline)', "must dispatch and validate"),

            ("business/scripts/online-security-owner-fixture.sh", 'sourceVersion: NumberLong("9007199254740993")', "fixture contract is missing"),
            ("scripts/test/security-mysql-owner-protection-online.py", "def initialize_postgres_read_grants(", "owner contract is missing"),
            ("scripts/test/online-hosted-security-gate.sh", "ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN", "Hosted profile is missing"),
            ("scripts/test/online-hosted-security-gate.sh", "security-mongodb.json", "Hosted profile is missing"),
            (".github/workflows/online-t4-gates.yml", "&& inputs.suite != 'security-mysql-owner-protection'", "must not also dispatch"),
            ("Makefile", "online-hosted-security-gate_test.py", "regression must enter"),
        ):
            with self.subTest(fragment=fragment):
                path = self.repository / relative
                original = path.read_text()
                self.assertIn(fragment, original, f"missing mutation input: {relative}: {fragment}")
                path.write_text(original.replace(fragment, ""))
                try:
                    with self.assertRaisesRegex(CHECK.RegistrationError, message):
                        CHECK.validate_security_mysql_owner_protection_profile(self.repository, registered)
                finally:
                    path.write_text(original)
        owner = self.repository / "scripts/test/security-mysql-owner-protection-online.py"
        original = owner.read_text()
        for before, after in (
            ("postgres_engine_id: int,\n", "postgres_engine_id: int, mysql_engine_id: int,\n"),
            ("postgres_engine_id: int,\n", "postgres_engine_id: int, mongodb_engine_id: int,\n"),
            ("REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, tenant_id, targets)",
             "REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, tenant_id, targets, item_term='collection')"),
        ):
            with self.subTest(reintroduced_source_dependency=after):
                self.assertIn(before, original)
                owner.write_text(original.replace(before, after, 1))
                try:
                    with self.assertRaisesRegex(CHECK.RegistrationError, "must prepare only PostgreSQL"):
                        CHECK.validate_security_mysql_owner_protection_profile(self.repository, registered)
                finally:
                    owner.write_text(original)
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(host.read_text() + "\nsecurity-mysql-owner-protection)\n")
        with self.assertRaisesRegex(CHECK.RegistrationError, "self-hosted route"):
            CHECK.validate_security_mysql_owner_protection_profile(self.repository, registered)

    def test_requires_transfer_field_lineage_services_proofs_and_cleanup(self) -> None:
        root = SCRIPT.parents[2]
        relatives = (
            "scripts/test/online-gate.py", "scripts/test/online-host-gate.sh",
            "scripts/test/online-hosted-transfer-gate.sh", "scripts/test/online-hosted-transfer-gate_test.py",
            ".github/workflows/online-t4-gates.yml", "Makefile",
            "scripts/test/transfer-relational-sql-etl-online.py", "scripts/test/transfer-relational-sql-etl-online_test.py",
            "scripts/test/online-transfer-relational-sql-etl-fixture_test.py",
            "business/scripts/online-transfer-relational-sql-etl-fixture.sh",
            "console/frontend/e2e/online/transfer-relational-sql-etl.spec.js",
            "transfer/backend/internal/planner/online_write_failure_contract_test.go",
            "system/backend/cmd/online-test-fixture/main.go",
            "system/backend/cmd/online-test-fixture/main_test.go",
        )
        for relative in relatives:
            target = self.repository / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(root / relative, target)
        registered = {"transfer-relational-sql-etl"}
        CHECK.validate_transfer_relational_sql_etl_profile(self.repository, registered)
        for relative, fragment in (
            ("Makefile", "go test ./internal/planner -run '^TestHostedWriteFailureTaskContract$$' -count=1"),
            ("transfer/backend/internal/planner/online_write_failure_contract_test.go", "ParseTableExportTaskSpec"),
            ("scripts/test/transfer-relational-sql-etl-online.py", '"meta.lineage.read"'),
            ("scripts/test/transfer-relational-sql-etl-online.py", "validate_mongodb_execution"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "validate_write_failure_physical"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "run_write_failures"),
            ("business/scripts/online-transfer-relational-sql-etl-fixture.sh", "PARTITION BY RANGE(id)"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "write_failure_details_verified: writeFailureDetailsVerified"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "addp.transfer-relational-sql-etl-online/v14"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "verify_scan_trace_visibility"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "owner_visible_observations"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "peer_denials"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "addp.scan-trace-visibility/v1"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "addp.manager-object-upload/v1"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "run_manager_import"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "non_uploader_rejected"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "manager-private-import.json"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "physical_geometry_verified"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "validate_manager_import_browser"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "metadata_wait_verified"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "failure_ui_verified"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "validate_multi_source_execution"),
            ("business/scripts/online-transfer-relational-sql-etl-fixture.sh", "multi-source CTE/JOIN/UNION rows differ"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "verify_historical_schema"),
            ("business/scripts/online-transfer-relational-sql-etl-fixture.sh", "TYPE timestamp without time zone USING activity_date::timestamp"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "manager_evolved_schema_verified: true"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "addp.transfer-relational-sql-etl-browser/v7"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "validate_wide_graph"),
            ("business/scripts/online-transfer-relational-sql-etl-fixture.sh", "EXCEPT ALL"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "manager_wide_field_graph_verified: true"),
            ("business/scripts/online-transfer-relational-sql-etl-fixture.sh", "--tmpfs /data/db --tmpfs /data/configdb"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "manager_mongodb_field_graph_verified: true"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "expect(mongodbGraphRequests).toBe(1)"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "cleanup_tasks(client"),
            ("business/scripts/online-transfer-relational-sql-etl-fixture.sh", "DROP TABLE IF EXISTS public.${NATIVE_DOWNSTREAM}"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "query_field_lineage_verified: true"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "expect(query.graph.field_lineage_status).toBe('complete')"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "expect(query.focused.edges).toHaveLength(1)"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "schema_snapshot_hash"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "verifyTaskConfigChecks"),
            ("system/backend/cmd/online-test-fixture/main.go", "grantTransferConfigCheckEngineReader"),
            ("business/scripts/online-transfer-relational-sql-etl-fixture.sh", "read-only configuration check created a target table"),
        ):
            with self.subTest(fragment=fragment):
                path = self.repository / relative
                original = path.read_text(encoding="utf-8")
                path.write_text(original.replace(fragment, ""), encoding="utf-8")
                with self.assertRaisesRegex(CHECK.RegistrationError, "contract is missing"):
                    CHECK.validate_transfer_relational_sql_etl_profile(self.repository, registered)
                path.write_text(original, encoding="utf-8")
        for relative, fragment, message in (
            ("scripts/test/online-hosted-transfer-gate.sh", "CONSOLE_URL=", "Hosted profile is missing"),
            ("scripts/test/online-hosted-transfer-gate.sh", "SECURITY_URL=", "Hosted profile is missing"),
            ("scripts/test/online-hosted-transfer-gate.sh", "-security ", "Hosted profile is missing"),
            ("scripts/test/online-hosted-transfer-gate.sh", "addp-transfer-mongodb-online-disposable", "Hosted profile is missing"),
            ("scripts/test/online-hosted-transfer-gate.sh", "export ADDP_ONLINE_TEST_MONGODB_ENGINE_ID", "Hosted profile is missing"),
            ("scripts/test/online-hosted-transfer-gate.sh", "ADDP_ONLINE_TRANSFER_MINIO_ENGINE_ID", "Hosted profile is missing"),
            ("scripts/test/online-hosted-transfer-gate.sh", "unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN", "Hosted profile is missing"),
            (".github/workflows/online-t4-gates.yml", "&& inputs.suite != 'transfer-relational-sql-etl'", "must not also dispatch"),
            (".github/workflows/online-t4-gates.yml", "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'transfer-relational-sql-etl'", "manual Ubuntu"),
            ("Makefile", "scripts/test/online-hosted-transfer-gate_test.py", "regression must enter"),
        ):
            with self.subTest(fragment=fragment):
                path = self.repository / relative
                original = path.read_text()
                path.write_text(original.replace(fragment, ""))
                with self.assertRaisesRegex(CHECK.RegistrationError, message):
                    CHECK.validate_transfer_relational_sql_etl_profile(self.repository, registered)
                path.write_text(original)
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(host.read_text() + "\n# transfer-relational-sql-etl\n")
        with self.assertRaisesRegex(CHECK.RegistrationError, "must not keep a self-hosted"):
            CHECK.validate_transfer_relational_sql_etl_profile(self.repository, registered)

    def test_pmtiles_requires_manual_isolation_content_and_cleanup(self):
        source = SCRIPT.parents[2]
        paths = ('.github/workflows/online-t4-gates.yml', 'scripts/test/online-gate.py',
            'scripts/test/online-hosted-vector-pmtiles-gate.sh', 'scripts/utils/hosted-online.sh',
            'business/scripts/online-vector-pmtiles-fixture.py', 'scripts/test/vector-pmtiles-workflow-online.py',
            'console/frontend/e2e/online/vector-pmtiles-workflow.spec.js',
            'system/backend/cmd/online-test-fixture/main.go', 'Makefile')
        for relative in paths:
            destination = self.repository / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source / relative, destination)
        CHECK.validate_vector_pmtiles_profile(self.repository, {'vector-pmtiles-workflow'})
        faults = [(paths[0], "inputs.suite != 'vector-pmtiles-workflow'"),
            (paths[2], '--wait-protection-owners develop,manager'),
            (paths[2], 'for start_target in -security -develop -manager -monitor'),
            (paths[2], 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
            (paths[2], 'business.scripts.online-vector-pmtiles-fixture stop'),
            (paths[3], 'stop_online_product_runtime || status=1'),
            (paths[4], 'shared.owned(name, SUITE)'), (paths[4], 'reader.get(5, 26, 13)'),
            (paths[5], 'before_scans != after_scans'), (paths[5], 'before_graph != after_graph'),
            (paths[6], 'evidence.tile_sha256'), (paths[6], "value === 'no-store'"),
            (paths[8], 'test_online_pmtiles_fixture.py'),
            (paths[8], 'test_pmtiles_requires_manual_isolation_content_and_cleanup')]
        for relative, fragment in faults:
            path = self.repository / relative
            original = path.read_text()
            self.assertIn(fragment, original)
            path.write_text(original.replace(fragment, 'omitted'))
            with self.subTest(relative=relative, fragment=fragment), self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_vector_pmtiles_profile(self.repository, {'vector-pmtiles-workflow'})
            path.write_text(original)

    def test_raster_requires_manual_disposable_profile_and_registered_regressions(self) -> None:
        root = SCRIPT.parents[2]
        files = (
            "scripts/test/online-gate.py", "scripts/test/online-hosted-raster-gate.sh", "scripts/utils/hosted-online.sh",
            "business/scripts/online-raster-minio-fixture.py", "scripts/test/raster-workflow-online.py",
            "scripts/utils/hosted-product-runtime.sh",
            "console/frontend/e2e/online/raster-workflow.spec.js", "Makefile",
            ".github/workflows/online-t4-gates.yml",
        )
        for relative in files:
            destination = self.repository / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(root / relative, destination)
        registered = {"raster-workflow"}
        CHECK.validate_raster_workflow_profile(self.repository, registered)
        faults = (
            ("scripts/utils/hosted-product-runtime.sh", "verify_online_product_runtime", "omitted_identity_check"),
            ("scripts/utils/hosted-online.sh", "stop_online_product_runtime || status=1", "true"),
            ("scripts/test/online-hosted-raster-gate.sh", "start_online_geopython_runtime", "start_native_runtime"),
            (".github/workflows/online-t4-gates.yml", "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-workflow'", "if: github.event_name == 'schedule'"),
            (".github/workflows/online-t4-gates.yml", "&& inputs.suite != 'raster-workflow'", ""),
            ("scripts/test/online-hosted-raster-gate.sh", "online-raster-minio-fixture.py stop", "true"),
            ("scripts/test/online-hosted-raster-gate.sh", "unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN", "true"),
            ("Makefile", "scripts/test/online-raster-minio-fixture_test.py", ""),
            ("console/frontend/e2e/online/raster-workflow.spec.js", "auth.principalID", "other"),
            ("scripts/test/raster-workflow-online.py", "spatial_workflow", "incomplete_case"),
            ("business/scripts/online-raster-minio-fixture.py", "verify-mosaic-last", "omitted"),
            ("console/frontend/e2e/online/raster-workflow.spec.js", "evidence.target_name", "omitted"),
            ("scripts/test/raster-workflow-online.py", "validate_analysis", "incomplete_analysis"),
            ("business/scripts/online-raster-minio-fixture.py", "verify-analysis", "omitted_analysis"),
            ("console/frontend/e2e/online/raster-workflow.spec.js", ".workflow-final-result-json", ".omitted-preview"),
        )
        for relative, old, new in faults:
            with self.subTest(relative=relative, fragment=old):
                path = self.repository / relative
                original = path.read_text()
                self.assertIn(old, original)
                path.write_text(original.replace(old, new))
                with self.assertRaises(CHECK.RegistrationError):
                    CHECK.validate_raster_workflow_profile(self.repository, registered)
                path.write_text(original)

    def test_requires_manager_internal_artifact_lineage_fixture_and_browser_suite(self) -> None:
        gate = self.repository / "scripts/test/online-gate.py"
        gate.write_text(
            gate.read_text(encoding="utf-8").replace(
                '"first-suite"', '"manager-internal-artifact-lineage"'
            ),
            encoding="utf-8",
        )
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(
            host.read_text(encoding="utf-8").replace(
                "  first-suite)\n    START_TARGET=-system\n    ;;\n", ""
            ),
            encoding="utf-8",
        )
        hosted = self.repository / "scripts/test/online-hosted-manager-gate.sh"
        hosted.write_text((SCRIPT.parents[2] / "scripts/test/online-hosted-manager-gate.sh").read_text(), encoding="utf-8")
        workflow = (SCRIPT.parents[2] / ".github/workflows/online-t4-gates.yml").read_text()
        import re
        job = re.search(r"(?ms)^  manager-hosted-t4:\n.*?(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow).group()
        self.workflow.write_text(self.workflow.read_text().replace("first-suite", "manager-internal-artifact-lineage").replace(
            "if: github.event_name == 'workflow_dispatch'", "if: github.event_name == 'workflow_dispatch' && inputs.suite != 'manager-internal-artifact-lineage'")
            + job, encoding="utf-8")
        fixture = self.repository / "business/scripts/online-manager-minio-fixture.sh"
        fixture.parent.mkdir(parents=True, exist_ok=True)
        for name in ("online-manager-nfs-fixture.sh", "online-content-boundary-fixture.py"):
            source = SCRIPT.parents[2] / "business/scripts" / name
            (fixture.parent / name).write_text(source.read_text(), encoding="utf-8")
        fixture.write_text(
            "ADDP_ONLINE_HOST --env-file /dev/null business-minio "
            "pdal_las12_format0.las addp_online_preview_fixture.pptx MC_HOST_fixture "
            "dae/model.dae 3ds/model.3ds texture.png format-samples/gbk-quoted.csv format-samples/gbk-quoted.tsv format-samples/format-validation.xlsx format-samples/document.json format-samples/records.json format-samples/features.geojson format-samples/features.json format-samples/trajectory.csv format-samples/trajectory.json format-samples/trajectory.geojson format-samples/vector-exchange/sites.kml format-samples/vector-exchange/sites.gml format-samples/vector-exchange/projected.topojson index-source-versions/source.csv index-source-versions/source.tsv index-csv-change index-tsv-restore index source restoration refused unexpected bytes\n",
            encoding="utf-8",
        )
        owner = self.repository / "scripts/test/manager-internal-artifact-lineage-online.py"
        owner.write_text(
            "addp.lineage-facts/v1 /api/v1/meta/scan/run/manual "
            "/api/v1/monitor/executions/by-execution-id/ "
            "addp-infra://minio/manager/tenant_ /api/v1/manager/point_cloud_copc/ "
            "/api/v1/manager/quick-view/capability /api/v1/manager/quick-view/actions "
            "/api/v1/manager/tasks/{PPTX_TASK_TYPE}/ "
            '"cache_reused": True model_3d_glb_generation /api/v1/manager/model_3d_glb/ '
            'MODEL_FORMATS MESH_SOURCE_FACTS FBX_BASIC_SOURCE prepare_basic_fbx_source fbx_basic_preview verify_mesh_source validate_model_bundle download_bundle validate_model_glb cleanup_managed_artifact generate_raster_cog raster_to_cog raster_physical DELIMITED_SHA256 delimited_sources WORKBOOK_SHA256 WORKBOOK_SHEETS workbook_source STRUCTURED_JSON_SOURCES structured_json_sources TRAJECTORY_SOURCES trajectory_sources VECTOR_EXCHANGE_SOURCES vector_exchange_sources addp.manager-internal-artifact-lineage/v15 "header_mode" "tested_has_header": False first_record_verified ordinary_refresh_preserved verify_content_boundaries boundaries = verify_content_boundaries boundaries += verify_content_boundaries content-boundaries.json ADDP_ONLINE_MANAGER_NFS_ENGINE_ID addp.content-boundaries/v2 directory-convergence refs-convergence "removed_components" "preserved_initial_items" force=not converging versions = verify_index_source_versions index-source-versions.json addp.index-source-versions/v2 storage in ("nfs", "minio") verify_manager_execution_events addp.manager-execution-events/v1 manager-execution-events.json\n',
            encoding="utf-8",
        )
        browser = self.repository / "console/frontend/e2e/online/manager-internal-artifact-lineage.spec.js"
        browser.parent.mkdir(parents=True, exist_ok=True)
        browser.write_text(
            ".execution-lineage__group .execution-lineage__resource-action "
            "平台内部产物|Platform-internal artifact platform_internal_outputs "
            ".pptx-preview .pdf-preview pptx_page_after_engine_refresh pptx_generation_requests "
            ".model-preview model_loaded content_loaded .raster-tiff-quick-view raster_generation_requests range_loaded map_loaded "
            "ADDP_ONLINE_MANAGER_BROWSER_PHASE generation-entry cached-preview generation_entry_visible source_model_loaded verifyBasicColoredFBX basic_preview_source basic_preview: basicPreviewProof vertex_colors_verified model-preview-support.js "
            "model_generation_requests anonymous_refresh_401 failed_business_responses "
            "addp.manager-internal-artifact-lineage-browser/v15 header_mode: applyHeaderMode(false) verifyHeaderlessPage(20, 6) applyHeaderMode(true) ADDP_ONLINE_MANAGER_DELIMITED_JSON "
            "verifyDelimitedTable delimited_tables encoding_request_verified download_sha256 ADDP_ONLINE_MANAGER_WORKBOOK_JSON verifyWorkbook .container-preview workbook: workbookEvidence unknown_total_verified refresh_preserved_page refresh_preserved_empty_sheet ADDP_ONLINE_MANAGER_STRUCTURED_JSON verifyStructuredJSON structured_json: structuredEvidence content_verified table_verified geometry_verified source_crs source_srid ADDP_ONLINE_MANAGER_TRAJECTORIES_JSON verifyTrajectoryRange trajectories: trajectoryEvidence ADDP_ONLINE_MANAGER_VECTOR_EXCHANGE_JSON verifyVectorExchange vector_exchange: vectorEvidence layer_proofs temporary_reset_verified\n",
            encoding="utf-8",
        )
        config = self.repository / "console/frontend/playwright.online.config.js"
        config.write_text("--use-gl=angle --use-angle=swiftshader-webgl --enable-unsafe-swiftshader\n", encoding="utf-8")
        img_owner = self.repository / 'scripts/test/manager-img-online.py'
        img_browser = browser.with_name('manager-img.spec.js')
        for target in (img_owner, img_browser, img_owner.with_name('manager-img-online_test.py')):
            target.write_text((SCRIPT.parents[2] / target.relative_to(self.repository)).read_text())
        owner.write_text(owner.read_text() + '\nimg_scene.run(repository, environment, sys.modules[__name__])\n')
        make = self.repository / 'Makefile'
        make.write_text((make.read_text() if make.exists() else '') + '\ntest-manager-online-runner:\n\t@node --check --input-type=module < console/frontend/e2e/online/manager-img.spec.js\n\t@python3 -m unittest scripts/test/manager-img-online_test.py\n')
        CHECK.check_registration(self.repository)
        for file, fragment, reason in (
            (img_owner, 'cleanup_managed_artifact', 'IMG Hosted subscene'),
            (img_browser, 'modelPixels', 'IMG Hosted subscene'),
            (owner, 'img_scene.run(repository, environment, sys.modules[__name__])', 'IMG Hosted subscene'),
            (hosted, "start_online_model3d_runtime", "Hosted profile"),
            (hosted, "bash business/scripts/online-manager-nfs-fixture.sh start", "Hosted profile"),
            (hosted, "bash business/scripts/online-manager-nfs-fixture.sh stop", "Hosted profile"),
            (owner, "validate_model_bundle", "owner contract"),
            (owner, "download_bundle", "owner contract"),
            (owner, "verify_content_boundaries", "owner contract"),
            (owner, "boundaries += verify_content_boundaries", "owner contract"),
            (owner, "content-boundaries.json", "owner contract"),
            (owner, "versions = verify_index_source_versions", "owner contract"),
            (owner, "index-source-versions.json", "owner contract"),
            (owner, "directory-convergence", "owner contract"),
            (owner, "refs-convergence", "owner contract"),
            (owner, "force=not converging", "owner contract"),
            (owner, '"removed_components"', "owner contract"),
            (fixture.with_name("online-content-boundary-fixture.py"), "directory-convergence", "fixture contract"),
            (fixture.with_name("online-content-boundary-fixture.py"), "refs-convergence", "fixture contract"),
            (fixture, "texture.png", "fixture contract"),
            (fixture, "format-samples/gbk-quoted.csv", "fixture contract"),
            (fixture, "format-samples/gbk-quoted.tsv", "fixture contract"),
            (fixture, "format-samples/format-validation.xlsx", "fixture contract"),
            (fixture, "format-samples/document.json", "fixture contract"),
            (fixture, "format-samples/records.json", "fixture contract"),
            (fixture, "format-samples/features.geojson", "fixture contract"),
            (fixture, "format-samples/features.json", "fixture contract"),
            (fixture, "format-samples/trajectory.csv", "fixture contract"),
            (fixture, "format-samples/trajectory.json", "fixture contract"),
            (fixture, "format-samples/trajectory.geojson", "fixture contract"),

            (config, "--enable-unsafe-swiftshader", "software WebGL"),
            (owner, "validate_model_glb", "owner contract"),
            (owner, "DELIMITED_SHA256", "owner contract"),
            (owner, "delimited_sources", "owner contract"),
            (owner, "WORKBOOK_SHA256", "owner contract"),
            (owner, "WORKBOOK_SHEETS", "owner contract"),
            (owner, "workbook_source", "owner contract"),
            (owner, "STRUCTURED_JSON_SOURCES", "owner contract"),
            (owner, "structured_json_sources", "owner contract"),
            (owner, "VECTOR_EXCHANGE_SOURCES", "owner contract"),
            (owner, "vector_exchange_sources", "owner contract"),
            (browser, "verifyVectorExchange", "browser contract"),
            (browser, "ADDP_ONLINE_MANAGER_VECTOR_EXCHANGE_JSON", "browser contract"),
            (browser, "layer_proofs", "browser contract"),
            (browser, "temporary_reset_verified", "browser contract"),
            (fixture, "format-samples/vector-exchange/sites.kml", "fixture contract"),
            (fixture, "format-samples/vector-exchange/sites.gml", "fixture contract"),
            (fixture, "format-samples/vector-exchange/projected.topojson", "fixture contract"),
            (owner, "TRAJECTORY_SOURCES", "owner contract"),
            (owner, "trajectory_sources", "owner contract"),
            (owner, "FBX_BASIC_SOURCE", "owner contract"),
            (owner, "prepare_basic_fbx_source", "owner contract"),
            (owner, "fbx_basic_preview", "owner contract"),
            (owner, "addp.manager-internal-artifact-lineage/v15", "owner contract"),
            (owner, '"header_mode"', "owner contract"),
            (owner, '"tested_has_header": False', "owner contract"),
            (owner, "first_record_verified", "owner contract"),
            (owner, "ordinary_refresh_preserved", "owner contract"),
            (owner, "addp.index-source-versions/v2", "owner contract"),
            (owner, 'storage in ("nfs", "minio")', "owner contract"),
            (owner, "verify_manager_execution_events", "owner contract"),
            (owner, "addp.manager-execution-events/v1", "owner contract"),
            (owner, "manager-execution-events.json", "owner contract"),
            (fixture, "index-csv-change", "fixture contract"),
            (fixture, "index-tsv-restore", "fixture contract"),
            (fixture, "index-source-versions/source.csv", "fixture contract"),
            (fixture, "index source restoration refused unexpected bytes", "fixture contract"),
            (browser, "verifyBasicColoredFBX", "browser contract"),
            (browser, "basic_preview_source", "browser contract"),
            (browser, "vertex_colors_verified", "browser contract"),
            (browser, "model-preview-support.js", "browser contract"),
            (browser, ".model-preview", "browser contract"),
            (browser, "addp.manager-internal-artifact-lineage-browser/v15", "browser contract"),
            (browser, "header_mode:", "browser contract"),
            (browser, "applyHeaderMode(false)", "browser contract"),
            (browser, "verifyHeaderlessPage(20, 6)", "browser contract"),
            (browser, "applyHeaderMode(true)", "browser contract"),
            (browser, "ADDP_ONLINE_MANAGER_BROWSER_PHASE", "browser contract"),
            (browser, "ADDP_ONLINE_MANAGER_DELIMITED_JSON", "browser contract"),
            (browser, "verifyDelimitedTable", "browser contract"),
            (browser, "delimited_tables", "browser contract"),
            (browser, "ADDP_ONLINE_MANAGER_WORKBOOK_JSON", "browser contract"),
            (browser, "verifyWorkbook", "browser contract"),
            (browser, ".container-preview", "browser contract"),
            (browser, "workbook: workbookEvidence", "browser contract"),
            (browser, "ADDP_ONLINE_MANAGER_STRUCTURED_JSON", "browser contract"),
            (browser, "verifyStructuredJSON", "browser contract"),
            (browser, "structured_json: structuredEvidence", "browser contract"),
            (browser, "ADDP_ONLINE_MANAGER_TRAJECTORIES_JSON", "browser contract"),
            (browser, "verifyTrajectoryRange", "browser contract"),
            (browser, "trajectories: trajectoryEvidence", "browser contract"),
            (browser, "content_verified", "browser contract"),
            (browser, "table_verified", "browser contract"),
            (browser, "geometry_verified", "browser contract"),
            (browser, "source_crs", "browser contract"),
            (browser, "source_srid", "browser contract"),
            (browser, "unknown_total_verified", "browser contract"),
            (browser, "refresh_preserved_page", "browser contract"),
            (browser, "refresh_preserved_empty_sheet", "browser contract"),
            (browser, "encoding_request_verified", "browser contract"),
            (browser, "download_sha256", "browser contract"),
            (browser, "generation-entry", "browser contract"),
            (browser, "cached-preview", "browser contract"),
            (browser, "anonymous_refresh_401", "browser contract"),
            (browser, "failed_business_responses", "browser contract"),
        ):
            with self.subTest(fragment=fragment):
                original = file.read_text(encoding="utf-8")
                file.write_text(original.replace(fragment, ""), encoding="utf-8")
                with self.assertRaisesRegex(CHECK.RegistrationError, reason):
                    CHECK.check_registration(self.repository)
                file.write_text(original, encoding="utf-8")
        for file, version, reason in (
            (owner, "addp.manager-internal-artifact-lineage/v15", "owner contract"),
            (browser, "addp.manager-internal-artifact-lineage-browser/v15", "browser contract"),
        ):
            with self.subTest(legacy_version=version):
                original = file.read_text(encoding="utf-8")
                file.write_text(original.replace(version, version.replace("/v15", "/v14")), encoding="utf-8")
                with self.assertRaisesRegex(CHECK.RegistrationError, reason):
                    CHECK.check_registration(self.repository)
                file.write_text(original, encoding="utf-8")
        browser.unlink()
        with self.assertRaisesRegex(CHECK.RegistrationError, "requires"):
            CHECK.check_registration(self.repository)

    def test_requires_manager_hybrid_search_fixture_and_owner_suite(self) -> None:
        gate = self.repository / "scripts/test/online-gate.py"
        gate.write_text(
            gate.read_text(encoding="utf-8").replace(
                '"first-suite"', '"manager-hybrid-search"'
            ),
            encoding="utf-8",
        )
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(
            host.read_text(encoding="utf-8").replace(
                "first-suite)\n    START_TARGET=-system",
                "manager-hybrid-search)\n    START_TARGET=-all",
            )
            + "\nSYSTEM_URL GATEWAY_URL META_URL MANAGER_URL INFERENCE_URL\n"
            + "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN ADDP_ONLINE_TEST_TENANT_ID\n"
            + "ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID ADDP_ONLINE_MANAGER_MINIO_PORT "
            + "ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY "
            + "ADDP_ONLINE_MANAGER_MINIO_BUCKET "
            + "ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT "
            + "ADDP_ONLINE_MANAGER_EMBEDDING_MODEL_PROFILE_ID\n"
            + "bash business/scripts/online-manager-minio-fixture.sh start\n"
            + "bash business/scripts/online-manager-minio-fixture.sh stop\n"
            + 'bash scripts/dev/start.sh "$START_TARGET"\n',
            encoding="utf-8",
        )
        self.workflow.write_text(
            self.workflow.read_text(encoding="utf-8").replace(
                "first-suite", "manager-hybrid-search"
            ),
            encoding="utf-8",
        )
        fixture = self.repository / "business/scripts/online-manager-minio-fixture.sh"
        fixture.parent.mkdir(parents=True, exist_ok=True)
        fixture.write_text(
            "ADDP_ONLINE_HOST --env-file /dev/null business-minio Autocop_4X3.jpg "
            "ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT MC_HOST_fixture\n",
            encoding="utf-8",
        )
        owner = self.repository / "scripts/test/manager-hybrid-search-online.py"
        owner.write_text(
            "/api/v1/meta/scan/run/manual /api/v1/manager/embedding_executions "
            "/api/v1/manager/embeddings? /api/v1/manager/search? "
            "expected_methods: list[str] "
            '["keyword", "vector"] ["vector"] vector_hits '
            '"residual_resources": -1\n',
            encoding="utf-8",
        )

        CHECK.check_registration(self.repository)
        owner.unlink()
        with self.assertRaisesRegex(CHECK.RegistrationError, "requires"):
            CHECK.check_registration(self.repository)

    def test_requires_oceanbase_consumer_flow_owner_contract(self) -> None:
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(
            host.read_text(encoding="utf-8")
            + "\noceanbase-consumer-flow)\n"
            + "SYSTEM_URL GATEWAY_URL META_URL MANAGER_URL TRANSFER_URL DEVELOP_URL SERVICE_URL\n"
            + "ADDP_ONLINE_OCEANBASE_ENGINE_ID ADDP_ONLINE_OCEANBASE_PORT\n"
            + "ADDP_ONLINE_OCEANBASE_DATABASE ADDP_ONLINE_OCEANBASE_USER ADDP_ONLINE_OCEANBASE_PASSWORD\n"
            + "bash business/scripts/online-oceanbase-consumer-fixture.sh start\n"
            + "bash business/scripts/online-oceanbase-consumer-fixture.sh stop\n"
            + 'bash scripts/dev/start.sh "$START_TARGET"\n',
            encoding="utf-8",
        )
        fixture = self.repository / "business/scripts/online-oceanbase-consumer-fixture.sh"
        fixture.parent.mkdir(parents=True, exist_ok=True)
        fixture.write_text(
            "ADDP_ONLINE_HOST --env-file /dev/null oceanbase/oceanbase-ce:4.4.2-lts "
            "business-oceanbase addp_online_consumer_source addp_online_consumer_target "
            "start|advance|stop|status reset_fixture\n",
            encoding="utf-8",
        )
        support = self.repository / "scripts/test/security-transfer-protection-online.py"
        support.write_text(
            "/api/v1/meta/scan/run/manual\n/api/v1/manager/preview\n",
            encoding="utf-8",
        )
        owner = self.repository / "scripts/test/relational-consumer-flow-online.py"
        owner.write_text(
            '"verification_owner": "deployment_profile"\n'
            '"type": "watermark"\n"start": "committed"\n'
            '"end": "execution_upper_bound"\n"apply_mode": "upsert"\n'
            '"manager.data_item.read"\n'
            "/api/v1/develop/executions\n/api/query/\nadvance_fixture(profile)\n"
            '"empty_resume"\ncleanup_tasks(client, task_ids)\n'
            'cleanup_service(client, service_id)\n"residual_resources": 0\n',
            encoding="utf-8",
        )
        for relative in (
            "scripts/test/online-oceanbase-consumer-fixture_test.py",
            "scripts/test/relational-consumer-flow-online_test.py",
        ):
            (self.repository / relative).write_text("fixture\n", encoding="utf-8")

        CHECK.validate_oceanbase_consumer_flow_profile(
            self.repository, {"oceanbase-consumer-flow"}
        )
        support_text = support.read_text(encoding="utf-8")
        support.write_text(
            support_text.replace("/api/v1/manager/preview", ""),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "manager/preview"):
            CHECK.validate_oceanbase_consumer_flow_profile(
                self.repository, {"oceanbase-consumer-flow"}
            )
        support.write_text(support_text, encoding="utf-8")
        owner.write_text(
            owner.read_text(encoding="utf-8").replace("advance_fixture(profile)", ""),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "advance_fixture"):
            CHECK.validate_oceanbase_consumer_flow_profile(
                self.repository, {"oceanbase-consumer-flow"}
            )

    def test_requires_tidb_consumer_flow_owner_and_digest_contract(self) -> None:
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(
            host.read_text(encoding="utf-8")
            + "\ntidb-consumer-flow)\n"
            + "SYSTEM_URL GATEWAY_URL META_URL MANAGER_URL TRANSFER_URL DEVELOP_URL SERVICE_URL\n"
            + "ADDP_ONLINE_TIDB_ENGINE_ID ADDP_ONLINE_TIDB_PORT\n"
            + "ADDP_ONLINE_TIDB_DATABASE ADDP_ONLINE_TIDB_USER\n"
            + "bash business/scripts/online-tidb-consumer-fixture.sh start\n"
            + "bash business/scripts/online-tidb-consumer-fixture.sh stop\n"
            + 'bash scripts/dev/start.sh "$START_TARGET"\n',
            encoding="utf-8",
        )
        fixture = self.repository / "business/scripts/online-tidb-consumer-fixture.sh"
        fixture.parent.mkdir(parents=True, exist_ok=True)
        fixture.write_text(
            "ADDP_ONLINE_HOST docker-compose.tidb-t2.yml addp-online-tidb-consumer "
            "addp_online_consumer_source addp_online_consumer_target "
            "start|advance|stop|status reset_fixture "
            "down --volumes --remove-orphans assert_zero_residue\n",
            encoding="utf-8",
        )
        compose = self.repository / "scripts/test/docker-compose.tidb-t2.yml"
        compose.write_text(
            "services:\n"
            "  tidb-pd:\n"
            "    image: pingcap/pd:v8.5.8@sha256:" + "a" * 64 + "\n"
            "  tidb-tikv:\n"
            "    image: pingcap/tikv:v8.5.8@sha256:" + "b" * 64 + "\n"
            "    ulimits:\n"
            "      nofile:\n"
            "        soft: 1000000\n"
            "        hard: 1000000\n"
            "  tidb:\n"
            "    image: pingcap/tidb:v8.5.8@sha256:" + "c" * 64 + "\n",
            encoding="utf-8",
        )
        owner = self.repository / "scripts/test/relational-consumer-flow-online.py"
        owner.write_text(
            '"tidb": ConsumerProfile(\n'
            'namespace_kind="database"\nitem_code_prefix="TIDB"\n'
            'identifier_quote="`"\n'
            'fixture_script="business/scripts/online-tidb-consumer-fixture.sh"\n'
            '"verification_owner": "deployment_profile"\n'
            '"schema_version": "addp.relational-consumer-flow-online/v1"\n'
            '"residual_resources": 0\n',
            encoding="utf-8",
        )
        for relative in (
            "scripts/test/online-tidb-consumer-fixture_test.py",
            "scripts/test/relational-consumer-flow-online_test.py",
        ):
            (self.repository / relative).write_text("fixture\n", encoding="utf-8")

        CHECK.validate_tidb_consumer_flow_profile(
            self.repository, {"tidb-consumer-flow"}
        )
        compose_text = compose.read_text(encoding="utf-8")
        compose.write_text(
            compose_text.replace("hard: 1000000", "hard: 65536"),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "nofile"):
            CHECK.validate_tidb_consumer_flow_profile(
                self.repository, {"tidb-consumer-flow"}
            )
        compose.write_text(compose_text, encoding="utf-8")
        compose.write_text(
            compose.read_text(encoding="utf-8").replace(
                "pingcap/tidb:v8.5.8@sha256:" + "c" * 64,
                "pingcap/tidb:v8.5.8",
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "pin pingcap/tidb"):
            CHECK.validate_tidb_consumer_flow_profile(
                self.repository, {"tidb-consumer-flow"}
            )

    def test_requires_opengauss_hosted_owner_and_identity_contracts(self) -> None:
        shared = self.repository / "scripts/utils/hosted-online.sh"
        shared.parent.mkdir(parents=True, exist_ok=True)
        shared.write_text("# shared test lifecycle\n", encoding="utf-8")
        hosted = self.repository / "scripts/test/online-hosted-opengauss-gate.sh"
        hosted.write_text(
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"\n'
            "# ADDP_ONLINE_SUITES=opengauss-consumer-flow\n"
            "# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64\n"
            "GITHUB_ACTIONS RUNNER_OS Linux x86_64 POSTGRES_DB=addp_online\n"
            "ADDP_ONLINE_SECRET_DIR go run ./cmd/online-test-fixture\n"
            "ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN\n"
            "opengauss-official-media.sh refusing to reuse existing image\n"
            "bash business/scripts/online-opengauss-consumer-fixture.sh start\n"
            "python3 scripts/test/online-engine-registration.py\n"
            '--descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE"\n'
            "for start_target in -security -manager -develop -service -transfer\n"
            "--wait-protection-owners manager,develop,service,transfer\n"
            "unset ADDP_TEST_OPENGAUSS_DSN\n"
            'make test-online "ONLINE_SUITE=$ONLINE_SUITE"\n'
            "bash scripts/dev/stop.sh\n"
            "bash scripts/infra/down.sh --volumes --force\n",
            encoding="utf-8",
        )
        fixture = self.repository / "business/scripts/online-opengauss-consumer-fixture.sh"
        fixture.parent.mkdir(parents=True, exist_ok=True)
        fixture.write_text(
            "opengauss_ensure_official_image x86_64 refusing to reuse existing image addp-opengauss-online-disposable "
            "addp_opengauss_online addp_online_consumer_source addp_online_consumer_target "
            "MERGE INTO container_exists start|advance|stop|status ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE "
            '"engine_type": "opengauss"\n',
            encoding="utf-8",
        )
        postgres_dockerfile = self.repository / "scripts/infra/Dockerfile.postgres"
        postgres_dockerfile.parent.mkdir(parents=True, exist_ok=True)
        postgres_dockerfile.write_text("FROM postgres:15\n", encoding="utf-8")
        (self.repository / "scripts/infra/up.sh").write_text(
            "BUILD_REPOSITORY_POSTGRES_IMAGE=true\n"
            "docker build --file scripts/infra/Dockerfile.postgres \\\n"
            '  --tag "$POSTGRES_IMAGE" scripts/infra\n',
            encoding="utf-8",
        )
        registration = self.repository / "scripts/test/online-engine-registration.py"
        registration.write_text(
            '/api/v1/system/engines\ntest_result.get("success") is not True\n'
            "ADDP_ONLINE_CONSUMER_ENGINE_ID\n",
            encoding="utf-8",
        )
        owner = self.repository / "scripts/test/relational-consumer-flow-online.py"
        owner.write_text(
            '"opengauss": ConsumerProfile(\nnamespace_kind="schema"\n'
            'identifier_quote=\'"\'\n'
            'fixture_script="business/scripts/online-opengauss-consumer-fixture.sh"\n'
            '"verification_owner": "deployment_profile"\n'
            '"schema_version": "addp.relational-consumer-flow-online/v1"\n'
            '"residual_resources": 0\n',
            encoding="utf-8",
        )
        for relative in (
            "scripts/test/online-hosted-opengauss-gate_test.py",
            "scripts/test/online-engine-registration_test.py",
            "scripts/test/online-opengauss-consumer-fixture_test.py",
            "scripts/test/relational-consumer-flow-online_test.py",
            "system/backend/cmd/online-test-fixture/main_test.go",
        ):
            path = self.repository / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("fixture\n", encoding="utf-8")
        identity = self.repository / "system/backend/cmd/online-test-fixture/main.go"
        identity.write_text(
            'engineProvisionerRoleKey = "tenant.infrastructure_administrator"\n'
            'repository.GetActiveBuiltinRoleByKey(ctx, engineProvisionerRoleKey)\n'
            'RoleKey: "online." + strings.ReplaceAll(*suite, "-", "_")\n'
            'permissions, err := suitePermissions(*suite)\n'
            'PermissionKeys: permissions\n'
            '"ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN"\n',
            # The guard owns consumer permissions, not unrelated preparation roles.
            encoding="utf-8",
        )
        identity.write_text(identity.read_text() + 'var consumerPermissions = []string{\n"manager.data_item.read",\n}\n'
                            + 'var pgPreparationPermissions = []string{"system.engine.read"}\n')

        CHECK.validate_opengauss_consumer_flow_profile(
            self.repository, {"opengauss-consumer-flow"}
        )

        owner_text = owner.read_text(encoding="utf-8")
        owner.write_text(owner_text + '"system.engine.read"\n', encoding="utf-8")
        with self.assertRaisesRegex(CHECK.RegistrationError, "consumer must not access"):
            CHECK.validate_opengauss_consumer_flow_profile(
                self.repository, {"opengauss-consumer-flow"}
            )
        owner.write_text(owner_text, encoding="utf-8")

        identity_text = identity.read_text(encoding="utf-8")
        identity.write_text(
            identity_text.replace('var consumerPermissions = []string{', 'var consumerPermissions = []string{\n"system.engine.read",'), encoding="utf-8"
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "consumer role must not"):
            CHECK.validate_opengauss_consumer_flow_profile(
                self.repository, {"opengauss-consumer-flow"}
            )
        identity.write_text(identity_text, encoding="utf-8")

        infra_up = self.repository / "scripts/infra/up.sh"
        infra_up_text = infra_up.read_text(encoding="utf-8")
        infra_up.write_text(
            infra_up_text.replace(
                "docker build --file scripts/infra/Dockerfile.postgres", "docker build"
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "Infra lifecycle"):
            CHECK.validate_opengauss_consumer_flow_profile(
                self.repository, {"opengauss-consumer-flow"}
            )
        infra_up.write_text(infra_up_text, encoding="utf-8")

        fixture.write_text(
            fixture.read_text(encoding="utf-8").replace("MERGE INTO", ""),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "MERGE INTO"):
            CHECK.validate_opengauss_consumer_flow_profile(
                self.repository, {"opengauss-consumer-flow"}
            )


if __name__ == "__main__":
    unittest.main()
