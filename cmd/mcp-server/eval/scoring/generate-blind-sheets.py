#!/usr/bin/env python3
"""Generate blind scoring CSV from anonymized outputs.

Usage: python3 generate-blind-sheets.py --blind-dir ../results/blind-scoring
"""

import argparse
import csv
import random
import sys
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--blind-dir", required=True)
    parser.add_argument("--seed", type=int, default=42)
    args = parser.parse_args()

    blind_dir = Path(args.blind_dir)
    if not blind_dir.is_dir():
        print("ERROR: Not found: %s" % blind_dir, file=sys.stderr)
        sys.exit(1)

    entries = []
    for scenario_dir in sorted(blind_dir.iterdir()):
        if not scenario_dir.is_dir():
            continue
        for txt in sorted(scenario_dir.glob("*.txt")):
            try:
                preview = txt.read_text()[:500].replace("\n", " ").strip()
            except (UnicodeDecodeError, OSError) as e:
                print("WARNING: %s: %s" % (txt, e), file=sys.stderr)
                continue
            entries.append({
                "scenario": scenario_dir.name,
                "output_id": txt.stem,
                "preview": preview,
            })

    if not entries:
        print("ERROR: No outputs in %s" % blind_dir, file=sys.stderr)
        sys.exit(1)

    rng = random.Random(args.seed)
    rng.shuffle(entries)

    sheet = blind_dir / "scoring-sheet.csv"
    with open(sheet, "w", newline="") as f:
        w = csv.writer(f)
        w.writerow([
            "scenario", "output_id", "preview",
            "root_cause(0/1)", "remediation(0/1)",
            "false_positives(count)", "completeness(1-5)",
            "clarity(1-5)", "notes",
        ])
        for e in entries:
            w.writerow([
                e["scenario"], e["output_id"], e["preview"],
                "", "", "", "", "", "",
            ])

    print("Created: %s (%d entries)" % (sheet, len(entries)))


if __name__ == "__main__":
    main()
