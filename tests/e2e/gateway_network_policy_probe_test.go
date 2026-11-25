package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// networkPolicyCurlProbeShell classifies curl outcomes using its stable exit
// status and machine-readable remote IP rather than version-dependent verbose
// text. A connect failure (7) or timeout before a connection (28) is valid
// denial evidence. A completed request proves unexpected reachability, while
// every other result is ambiguous and fails with curl diagnostics.
const networkPolicyCurlProbeShell = `set -eu

: "${NETWORK_POLICY_PROBE_TERMINATION_LOG:=/dev/termination-log}"

network_policy_expect_connection_denied() {
  probe_url=$1
  probe_error_file="/tmp/network-policy-probe-curl.$$.err"
  probe_rc=0
  : > "$probe_error_file"
  probe_remote_ip="$(curl --noproxy '*' -ksS --connect-timeout 5 --max-time 8 \
    -o /dev/null --write-out '%{remote_ip}' "$probe_url" 2>"$probe_error_file")" || probe_rc=$?

  if [ "$probe_rc" -eq 0 ]; then
    printf 'unexpected successful connection to %s (curl exit 0)\n' "$probe_url" > "$NETWORK_POLICY_PROBE_TERMINATION_LOG"
    rm -f "$probe_error_file"
    return 1
  fi

  case "$probe_rc:$probe_remote_ip" in
    7:|28:)
      rm -f "$probe_error_file"
      return 0
      ;;
  esac

  {
    printf 'ambiguous curl failure for %s (curl exit %s, remote IP %s)\n' \
      "$probe_url" "$probe_rc" "${probe_remote_ip:-none}"
    if [ -s "$probe_error_file" ]; then
      printf 'curl stderr: '
      cat "$probe_error_file"
    fi
  } > "$NETWORK_POLICY_PROBE_TERMINATION_LOG"
  rm -f "$probe_error_file"
  return 1
}
`

func TestNetworkPolicyCurlProbeClassification(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		exitCode     string
		remoteIP     string
		stderr       string
		wantSuccess  bool
		wantLogParts []string
	}{
		{
			name:         "successful request is unexpected reachability",
			exitCode:     "0",
			remoteIP:     "10.0.0.8",
			wantLogParts: []string{"unexpected successful connection", "curl exit 0"},
		},
		{
			name:        "connect failure is denial evidence",
			exitCode:    "7",
			stderr:      "curl: (7) Failed to connect",
			wantSuccess: true,
		},
		{
			name:        "connect timeout is denial evidence",
			exitCode:    "28",
			stderr:      "curl: (28) Connection timed out",
			wantSuccess: true,
		},
		{
			name:         "timeout after connecting is ambiguous",
			exitCode:     "28",
			remoteIP:     "10.0.0.8",
			stderr:       "curl: (28) Operation timed out",
			wantLogParts: []string{"ambiguous curl failure", "curl exit 28", "remote IP 10.0.0.8", "Operation timed out"},
		},
		{
			name:         "protocol failure is ambiguous",
			exitCode:     "35",
			stderr:       "curl: (35) TLS connect error",
			wantLogParts: []string{"ambiguous curl failure", "curl exit 35", "remote IP none", "TLS connect error"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			fakeCurl := filepath.Join(tempDir, "curl")
			require.NoError(t, os.WriteFile(fakeCurl, []byte(`#!/bin/sh
printf '%s' "$FAKE_CURL_REMOTE_IP"
if [ -n "$FAKE_CURL_STDERR" ]; then
  printf '%s\n' "$FAKE_CURL_STDERR" >&2
fi
exit "$FAKE_CURL_EXIT_CODE"
`), 0o600))
			require.NoError(t, os.Chmod(fakeCurl, 0o700))

			terminationLog := filepath.Join(tempDir, "termination.log")
			command := exec.CommandContext(t.Context(), "/bin/sh", "-c", networkPolicyCurlProbeShell+`
network_policy_expect_connection_denied "$TEST_URL"
`)
			command.Env = append(os.Environ(),
				"PATH="+tempDir+":"+os.Getenv("PATH"),
				"NETWORK_POLICY_PROBE_TERMINATION_LOG="+terminationLog,
				"TEST_URL=https://10.0.0.8:8443",
				"FAKE_CURL_EXIT_CODE="+testCase.exitCode,
				"FAKE_CURL_REMOTE_IP="+testCase.remoteIP,
				"FAKE_CURL_STDERR="+testCase.stderr,
			)

			err := command.Run()
			if testCase.wantSuccess {
				require.NoError(t, err)
				_, statErr := os.Stat(terminationLog)
				require.ErrorIs(t, statErr, os.ErrNotExist)
				return
			}

			require.Error(t, err)
			logBytes, readErr := os.ReadFile(terminationLog)
			require.NoError(t, readErr)
			logOutput := string(logBytes)
			for _, part := range testCase.wantLogParts {
				assert.Contains(t, logOutput, part, "termination log should contain expected diagnostics")
			}
		})
	}
}
