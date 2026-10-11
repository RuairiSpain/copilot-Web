import subprocess
import sys

from conftest import ROOT


def test_stage1_examples_document_is_up_to_date():
    # docs/STAGE1_EXAMPLES.md is generated from the catalogs and stage 1; rerun the script after changing either.
    result = subprocess.run([sys.executable, str(ROOT / "scripts/stage1_examples.py"), "--check"],
                            capture_output=True, text=True)
    assert result.returncode == 0, result.stderr
