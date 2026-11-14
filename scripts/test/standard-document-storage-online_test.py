"""Check that the observer rejects missing byte and tenant-isolation evidence."""
from contextlib import redirect_stdout
from email.parser import BytesParser
from importlib import import_module
from io import StringIO
import os
import unittest
from unittest.mock import patch

scenario = import_module("scripts.test.standard-document-storage-online")


class ObserverTest(unittest.TestCase):
    def exercise(self, *, corrupt=False, cross_status=404):
        class Client:
            def __init__(self, url, token, timeout):
                self.token = token
                self.version = 1

            def request(self, method, path, expected, body=None, **kwargs):
                payload, status = {}, next(iter(expected))
                if method == "POST" and path.endswith("/documents"):
                    payload = {"id": 11, "draft_revision": {"id": 12}, "version": 1}
                elif method == "POST":
                    mime = BytesParser().parsebytes(
                        f'Content-Type: {kwargs["content_type"]}\r\nMIME-Version: 1.0\r\n\r\n'.encode()
                        + kwargs["encoded_body"])
                    parts = mime.get_payload()
                    self.content = parts[1].get_payload(decode=True)
                    self.version += 1
                    payload = {"version": self.version}
                elif path.endswith("/file") and method == "GET":
                    if self.token == "foreign":
                        status = cross_status
                    else:
                        payload = self.content + (b"corruption" if corrupt else b"")
                elif "/revisions/" in path:
                    if self.token == "owner":
                        payload = {"file_key": f"key-{self.version}", "content_sha256": scenario.hashlib.sha256(self.content).hexdigest(),
                                   "file_size": len(self.content)}
                return scenario.api.Response(status, payload)

        instances = []
        def client(*args):
            result = Client(*args)
            instances.append(result)
            return result
        def physical(bucket, key):
            return instances[0].content
        env = {"ADDP_ONLINE_TEST_TENANT_ID": "2", "ADDP_ONLINE_STANDARD_FOREIGN_TENANT_ID": "3",
               "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": "owner", "ADDP_ONLINE_STANDARD_FOREIGN_TOKEN": "foreign",
               "GATEWAY_URL": "http://localhost", "STANDARD_HTTP_SOURCE_COMMIT": "a"*40, "GITHUB_SHA": "b"*40}
        with patch.dict(os.environ, env), patch.object(scenario.api, "GatewayClient", side_effect=client),              patch.object(scenario.api, "validate_user_identity", return_value={}) as identity,              patch.object(scenario, "object_bytes", side_effect=physical),              patch.object(scenario, "absent"), redirect_stdout(StringIO()) as output:
            scenario.main()
            self.assertEqual(identity.call_count, 2)
            return scenario.json.loads(output.getvalue())

    def test_downloads_are_checked_after_both_uploads(self):
        report = self.exercise()
        self.assertEqual(report["revision_id"], 12)
        self.assertEqual(len(report["observations"]), 2)
        self.assertEqual(report["cross_tenant_http_status"], 404)

    def test_corrupt_download_fails(self):
        with self.assertRaises(scenario.api.SuiteError):
            self.exercise(corrupt=True)


if __name__ == "__main__":
    unittest.main()
