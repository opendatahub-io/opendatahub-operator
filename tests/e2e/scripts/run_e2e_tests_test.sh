#!/bin/bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT
mkdir -p "$TMP_DIR/bin" "$TMP_DIR/results"

cat > "$TMP_DIR/bin/gotestsum" <<'EOF'
#!/bin/bash
set -eu
while [ "$#" -gt 0 ]; do
  case "$1" in
    --junitfile)
      printf '<testsuite><failure/></testsuite>\n' > "$2"
      shift 2
      ;;
    --jsonfile)
      printf '{}\n' > "$2"
      shift 2
      ;;
    *) shift ;;
  esac
done
case "${E2E_TEST_PROFILE:-}" in
  core) exit "${FAKE_CORE_EXIT:-${FAKE_TEST_EXIT:-0}}" ;;
  dag) exit "${FAKE_DAG_EXIT:-${FAKE_TEST_EXIT:-0}}" ;;
  *) exit "${FAKE_TEST_EXIT:-0}" ;;
esac
EOF

cat > "$TMP_DIR/bin/jq" <<'EOF'
#!/bin/bash
set -eu
if [ "${FAKE_JQ_EXIT:-0}" -ne 0 ]; then
  exit "${FAKE_JQ_EXIT}"
fi
last_arg="${!#}"
cat "$last_arg"
EOF

cat > "$TMP_DIR/bin/go-junit-report" <<'EOF'
#!/bin/bash
set -eu
output=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -out)
      output=$2
      shift 2
      ;;
    *) shift ;;
  esac
done
printf '<testsuite></testsuite>\n' > "$output"
EOF

cat > "$TMP_DIR/bin/test-retry" <<'EOF'
#!/bin/bash
case "${1:-}" in
  verdict)
    shift
    printf '%s\n' "$*" > "${FAKE_VERDICT_ARGS}"
    exit 1
    ;;
  filter-gate-skips)
    exit 0
    ;;
  *)
    echo "unexpected test-retry subcommand: ${1:-}" >&2
    exit 2
    ;;
esac
EOF

chmod +x "$TMP_DIR/bin/gotestsum" "$TMP_DIR/bin/jq" "$TMP_DIR/bin/go-junit-report" "$TMP_DIR/bin/test-retry"

status=0
PATH="$TMP_DIR/bin:$PATH" \
FAKE_TEST_EXIT=7 \
FAKE_JQ_EXIT=9 \
FAKE_VERDICT_ARGS="$TMP_DIR/verdict-args" \
E2E_RESULTS_DIR="$TMP_DIR/results" \
E2E_RUN_TIMESTAMP=20261109T102500Z \
E2E_TEST_WORKBENCHES_NAMESPACE=opendatahub \
"$SCRIPT_DIR/run_e2e_tests.sh" core || status=$?

if [ "$status" -ne 7 ]; then
  echo "expected underlying test exit 7, got $status" >&2
  exit 1
fi
if [ ! -s "$TMP_DIR/verdict-args" ]; then
  echo "expected final verdict to be attempted" >&2
  exit 1
fi
if ! grep -q -- '--core-exit=7' "$TMP_DIR/verdict-args" || ! grep -q -- '--core-only' "$TMP_DIR/verdict-args"; then
  echo "unexpected verdict arguments: $(cat "$TMP_DIR/verdict-args")" >&2
  exit 1
fi
if ! grep -q -- '--output=.*/e2e-summary-20261109T102500Z.json' "$TMP_DIR/verdict-args"; then
  echo "expected timestamped summary path: $(cat "$TMP_DIR/verdict-args")" >&2
  exit 1
fi
if [ ! -s "$TMP_DIR/results/junit-core-20261109T102500Z.xml" ]; then
  echo "expected timestamped core JUnit report" >&2
  exit 1
fi

echo "runner failure-preservation and timestamp test passed"

rm -f "$TMP_DIR/bin/test-retry"
rm -rf "$TMP_DIR/results"
mkdir -p "$TMP_DIR/results"
status=0
PATH="$TMP_DIR/bin:$PATH" \
FAKE_CORE_EXIT=0 \
FAKE_DAG_EXIT=7 \
FAKE_JQ_EXIT=0 \
E2E_RESULTS_DIR="$TMP_DIR/results" \
E2E_RUN_TIMESTAMP=20261109T103000Z \
E2E_TEST_WORKBENCHES_NAMESPACE=opendatahub \
"$SCRIPT_DIR/run_e2e_tests.sh" all || status=$?

if [ "$status" -ne 7 ]; then
  echo "expected missing-verdict fallback to preserve DAG exit 7, got $status" >&2
  exit 1
fi
if [ ! -s "$TMP_DIR/results/junit-core-20261109T103000Z.xml" ] || \
   [ ! -s "$TMP_DIR/results/junit-dag-20261109T103000Z.xml" ]; then
  echo "expected both timestamped JUnit reports before missing-verdict fallback" >&2
  exit 1
fi

echo "runner missing-verdict fallback test passed"
