#!/bin/bash

# INTENTIONAL SHELL SCRIPT SECURITY ISSUES FOR POC TESTING
# This file contains intentional security vulnerabilities to validate
# CodeRabbit security scanning configuration (JIRA: RHOAIENG-38196)
#
# DO NOT MERGE TO PRODUCTION - PoC VALIDATION ONLY

# SECURITY ISSUE 1: Hardcoded credentials
# Expected: Gitleaks should detect these
DB_PASSWORD="MySecretPassword123!"
API_KEY="secret_key_EXAMPLE_not_real_1234567890"
AWS_ACCESS_KEY="AKIAIOSFODNN7EXAMPLE"

# SECURITY ISSUE 2: Use of eval (CWE-94)
# Expected: semgrep rule 'shell-eval' should trigger
USER_INPUT=$1
eval "$USER_INPUT"

# SECURITY ISSUE 3: Unquoted variables in dangerous commands (CWE-78)
# Expected: semgrep rule 'shell-missing-quotes-dangerous' should trigger
FILE_TO_DELETE=$2
rm $FILE_TO_DELETE

# SECURITY ISSUE 4: Command injection via unquoted variable
USER_FILE=$3
cp $USER_FILE /tmp/backup

# SECURITY ISSUE 5: Missing error handling (no set -e)
# Expected: ShellCheck should warn about this

# SECURITY ISSUE 6: Unquoted variables in chmod
PERMISSIONS=$4
chmod $PERMISSIONS /tmp/file

# Valid example with proper quoting (should NOT trigger)
function secure_delete() {
    local file="$1"
    if [[ -f "$file" ]]; then
        rm -f "$file"
    fi
}

# Valid example with input validation
function secure_copy() {
    local source="$1"
    local dest="$2"

    if [[ ! -f "$source" ]]; then
        echo "Error: Source file does not exist"
        return 1
    fi

    cp "$source" "$dest"
}
