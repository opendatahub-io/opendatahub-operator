#!/usr/bin/env python3
"""Parse stream-json from claude --print into diagnosis.txt and tool-calls.json.

Usage: python3 parse-stream.py <stream.jsonl> <output-dir>
"""

import json
import sys
from pathlib import Path


def main():
    if len(sys.argv) < 3:
        print("Usage: %s <stream.jsonl> <output-dir>" % sys.argv[0],
              file=sys.stderr)
        sys.exit(1)

    stream_file = Path(sys.argv[1])
    output_dir = Path(sys.argv[2])

    if not stream_file.exists():
        print("ERROR: Stream file not found: %s" % stream_file,
              file=sys.stderr)
        sys.exit(1)

    try:
        content = stream_file.read_text()
    except (UnicodeDecodeError, OSError) as e:
        print("ERROR: Cannot read %s: %s" % (stream_file, e),
              file=sys.stderr)
        sys.exit(1)

    text_parts = []
    tool_calls = []
    parse_errors = 0

    for line in content.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            event = json.loads(line)
        except (json.JSONDecodeError, ValueError):
            parse_errors += 1
            continue

        if not isinstance(event, dict):
            continue

        event_type = event.get("type", "")

        if event_type == "assistant":
            message = event.get("message", {})
            if not isinstance(message, dict):
                continue
            for block in message.get("content", []):
                if not isinstance(block, dict):
                    continue
                if block.get("type") == "text":
                    text_parts.append(block.get("text", ""))
                elif block.get("type") == "tool_use":
                    tool_calls.append({
                        "name": block.get("name", ""),
                        "input": block.get("input", {}),
                    })

        elif event_type == "result":
            result = event.get("result", {})
            if isinstance(result, str):
                text_parts.append(result)
            elif isinstance(result, dict):
                for block in result.get("content", []):
                    if isinstance(block, dict) and block.get("type") == "text":
                        text_parts.append(block.get("text", ""))

    output_dir.mkdir(parents=True, exist_ok=True)
    diagnosis = "\n".join(text_parts).strip()
    (output_dir / "diagnosis.txt").write_text(diagnosis)

    with open(output_dir / "tool-calls.json", "w") as f:
        json.dump(tool_calls, f, indent=2)

    if parse_errors:
        print("  WARNING: %d unparseable lines in %s" %
              (parse_errors, stream_file), file=sys.stderr)

    print("  Parsed: %d chars text, %d tool calls" %
          (len(diagnosis), len(tool_calls)))


if __name__ == "__main__":
    main()
