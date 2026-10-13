"""Sample the generated Learn URLs and report any that do not resolve (run after a scan)."""
from __future__ import annotations

import random
import sys

import requests

from tracker.common import DATA_DIR, read_csv

rows = [r for p in DATA_DIR.glob("*/changes/*.csv") for r in read_csv(p) if r["url"]]
sample = random.sample(rows, min(25, len(rows)))
bad = 0
for r in sample:
    try:
        code = requests.head(r["url"], allow_redirects=True, timeout=20).status_code
    except requests.RequestException as exc:
        code = str(exc)[:60]
    if code != 200:
        bad += 1
        print(f"{code}  {r['url']}  <- {r['file_path']}")
print(f"{len(sample) - bad}/{len(sample)} sampled URLs resolved")
sys.exit(0)
