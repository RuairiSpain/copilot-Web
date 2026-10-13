"""Sample the generated Learn URLs and report any that do not resolve (run after a scan)."""
from __future__ import annotations

import random

from tracker.common import DATA_DIR, http_request, read_csv

SAMPLE = 25


def main() -> int:
    rows = [r for p in DATA_DIR.glob("*/changes/*.csv") for r in read_csv(p) if r["url"]]
    sample = random.sample(rows, min(SAMPLE, len(rows)))
    bad = 0
    for r in sample:
        try:
            code: int | str = http_request("HEAD", r["url"], allow_redirects=True, timeout=20, attempts=1).status_code
        except OSError as exc:
            code = str(exc)[:60]
        if code != 200:
            bad += 1
            print(f"{code}  {r['url']}  <- {r['file_path']}")
    print(f"{len(sample) - bad}/{len(sample)} sampled URLs resolved")
    return bad


if __name__ == "__main__":
    main()
