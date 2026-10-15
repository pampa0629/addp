"""Real native conversion gate, invoked only by make test-document-native."""
import tempfile
import unittest
import zipfile
from pathlib import Path
from xml.etree import ElementTree as ET

from pypdf import PdfReader

from native_setup import ENGINE, native_paths, sha256
from operators import invoke_operator


class NativeConversionTests(unittest.TestCase):
    def test_chinese_three_page_pdf_and_cleanup(self):
        root = ENGINE.parents[1]
        _, _, prefix, executable, _ = native_paths(root / ".dev-state/document-native")
        fixture = root / "business/fixtures/manager/addp_online_preview_fixture.pptx"
        before = sha256(fixture)
        title = "ADDP 文档原生转换实验：中文与 English"
        with tempfile.TemporaryDirectory(prefix="addp-document-test-") as directory:
            directory = Path(directory)
            source, target = directory / "source.pptx", directory / "converted.pdf"
            with zipfile.ZipFile(fixture) as original, zipfile.ZipFile(source, "w") as output:
                for member in original.infolist():
                    data = original.read(member.filename)
                    if member.filename == "ppt/slides/slide1.xml":
                        tree = ET.fromstring(data)
                        next(tree.iter("{http://schemas.openxmlformats.org/drawingml/2006/main}t")).text = title
                        data = ET.tostring(tree, encoding="utf-8", xml_declaration=True)
                    output.writestr(member, data)
            plan = {"schema_version": "addp.workflow.access-plan/v1",
                    "source": {"kind": "file", "format": "pptx",
                               "access": {"method": "mounted_path", "path": str(source)}},
                    "target": {"kind": "file", "format": "pdf", "name": target.name, "write_mode": "create",
                               "access": {"method": "mounted_path", "path": str(target)}}}
            work = directory / "work"
            invoke_operator("document_to_pdf", {"access_plan": plan},
                            env={"DOCUMENT_LIBREOFFICE_BIN": str(executable),
                                 "FONTCONFIG_FILE": str(prefix / "fonts.conf"),
                                 "DOCUMENT_WORK_DIR": str(work)}, timeout_seconds=60)
            reader = PdfReader(target)
            self.assertEqual(len(reader.pages), 3)
            self.assertIn(title, reader.pages[0].extract_text())
            self.assertFalse(work.exists() and any(work.iterdir()), "conversion workspace was not cleaned")
        self.assertEqual(before, sha256(fixture), "conversion changed the source fixture")
