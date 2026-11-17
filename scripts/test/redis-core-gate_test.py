"""The owned core gate must observe every real Redis/PG test, not a skipped run."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]

class RedisCoreGateTest(unittest.TestCase):
    def test_contract_requires_actual_cases_and_rejects_skip_or_missing_case(self):
        with tempfile.TemporaryDirectory(prefix="redis-core-entry-") as directory:
            root=Path(directory)
            (root/"scripts/test").mkdir(parents=True)
            for module in ("common", "gateway", "system/backend"):
                (root/module).mkdir(parents=True)
            contract=root/"scripts/test/common-redis-core-contract.sh"
            contract.write_text((ROOT/"scripts/test/common-redis-core-contract.sh").read_text())
            binary=root/"bin"; binary.mkdir()
            go=binary/"go"
            go.write_text("""#!/bin/sh
if [ "$PROBE_RESULT" = skip ]; then echo '--- SKIP: TestRequiredRedisAgainstRedis'; exit 0; fi
if [ "$PROBE_RESULT" = missing ]; then echo 'PASS'; exit 0; fi
case "$PWD" in
 */common) echo '--- PASS: TestRequiredRedisAgainstRedis (0.01s)'; echo '--- PASS: TestRedisReadinessAgainstRedis (0.01s)' ;;
 */gateway) echo '--- PASS: TestConsumerRateLimitAgainstRedis (0.01s)' ;;
 */system/backend) echo '--- PASS: TestOAuthRateLimitAgainstRedis (0.01s)' ;;
esac
""")
            make=binary/"make"
            make.write_text("""#!/bin/sh
if [ "$PROBE_RESULT" = missing-pg ]; then echo PASS; else echo '    --- PASS: TestIAMOAuthClientCredentialsAuthContextAgainstPostgres/Redis_core_outage_preserves_Token_and_grant_facts (0.01s)'; fi
""")
            go.chmod(0o755); make.chmod(0o755)
            for result,code in (("pass",0),("skip",1),("missing",1),("missing-pg",1)):
                with self.subTest(result=result):
                    run=subprocess.run(["bash",str(contract)],env={**os.environ,"PATH":str(binary)+os.pathsep+os.environ["PATH"],"PROBE_RESULT":result,
                        "ADDP_REDIS_CORE_ENDPOINT":"127.0.0.1:6379","ADDP_REDIS_CORE_PASSWORD":"fixture-only"},capture_output=True,text=True)
                    self.assertEqual(run.returncode,code,run.stdout+run.stderr)

    def test_missing_pg_configuration_fails_before_docker(self):
        env=dict(os.environ);env.pop("ADDP_SYSTEM_POSTGRES_TEST_DSN",None)
        run=subprocess.run(["bash",str(ROOT/"scripts/test/common-redis-core-gate.sh")],env=env,capture_output=True,text=True)
        self.assertNotEqual(run.returncode,0)
        self.assertIn("disposable System IAM PostgreSQL",run.stderr)

if __name__=="__main__": unittest.main()
