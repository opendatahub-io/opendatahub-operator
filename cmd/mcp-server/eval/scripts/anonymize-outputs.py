#!/usr/bin/env python3
"""Anonymize eval outputs for blind scoring.

Strips config labels, shuffles output order per scenario, writes to
results/blind-scoring/ with mapping.json for unblinding.

Usage: python3 anonymize-outputs.py --results-dir ../results
"""

import argparse
import json
import random
import re
import sys
from pathlib import Path

_LABELS = ("Alpha", "Beta", "Gamma")

_CONFIG_PATTERN = re.compile(
    r"config[_-]?[abc]|baseline|diagnostic agent|classifier|"
    r"raw llm|no system prompt|one-shot|one shot|deterministic",
    re.IGNORECASE,
)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--results-dir", required=True)
    parser.add_argument("--seed", type=int, default=42)
    args = parser.parse_args()

    results_dir = Path(args.results_dir)
    if not results_dir.is_dir():
        print("ERROR: Results dir not found: %s" % results_dir, file=sys.stderr)
        sys.exit(1)

    config_dirs = sorted(
        d for d in results_dir.iterdir()
        if d.is_dir() and d.name.startswith("config-")
    )
    if not config_dirs:
        print("ERROR: No config-* dirs in %s" % results_dir, file=sys.stderr)
        sys.exit(1)

    scenarios = set()
    for config_dir in config_dirs:
        for entry in config_dir.iterdir():
            if entry.is_dir():
                scenarios.add(entry.name)
    if not scenarios:
        print("ERROR: No scenarios found", file=sys.stderr)
        sys.exit(1)

    if len(config_dirs) > len(_LABELS):
        print("ERROR: %d configs but only %d anonymous labels available"
              % (len(config_dirs), len(_LABELS)), file=sys.stderr)
        sys.exit(1)

    blind_dir = results_dir / "blind-scoring"
    blind_dir.mkdir(exist_ok=True)
    rng = random.Random(args.seed)
    mapping = {}
    count = 0

    for scenario in sorted(scenarios):
        out_dir = blind_dir / scenario
        out_dir.mkdir(exist_ok=True)

        labels = list(_LABELS[:len(config_dirs)])
        rng.shuffle(labels)

        scenario_map = {}
        for config_dir, label in zip(config_dirs, labels):
            source = config_dir / scenario / "diagnosis.txt"
            if not source.is_file():
                continue
            try:
                text = source.read_text()
            except (UnicodeDecodeError, OSError) as e:
                print("  SKIP: %s (%s)" % (source, e), file=sys.stderr)
                continue
            if len(text.strip()) < 10:
                continue

            scrubbed = _CONFIG_PATTERN.sub("[REDACTED]", text)
            anon_id = "Output-%s" % label
            (out_dir / ("%s.txt" % anon_id)).write_text(scrubbed)
            scenario_map[anon_id] = config_dir.name
            count += 1

        mapping[scenario] = scenario_map

    mapping_file = blind_dir / "mapping.json"
    with open(mapping_file, "w") as f:
        json.dump(mapping, f, indent=2, sort_keys=True)

    # Verify no labels leaked
    leaks = 0
    for txt in blind_dir.rglob("*.txt"):
        try:
            if _CONFIG_PATTERN.search(txt.read_text()):
                print("  LEAK: %s" % txt, file=sys.stderr)
                leaks += 1
        except (UnicodeDecodeError, OSError) as e:
            print("  WARNING: Cannot verify %s: %s" % (txt, e), file=sys.stderr)
            leaks += 1

    print("Anonymized %d outputs across %d scenarios" % (count, len(scenarios)))
    if leaks:
        print("ERROR: %d files have leaked config labels" % leaks, file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
