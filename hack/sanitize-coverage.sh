#!/bin/bash

# Script to sanitize coverage reports to remove sensitive information
# This script removes absolute paths, usernames, and other PII from coverage reports

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to sanitize a file
sanitize_file() {
    local input_file="$1"
    local output_file="$2"
    
    if [[ ! -f "$input_file" ]]; then
        print_error "Input file $input_file does not exist"
        return 1
    fi
    
    print_status "Sanitizing $input_file -> $output_file"
    
    # Create a temporary file for processing
    local temp_file=$(mktemp)
    
    # Copy the original file
    cp "$input_file" "$temp_file"
    
    # Replace absolute paths with relative paths
    # This handles various path patterns that might contain sensitive information
    
    # Replace absolute Unix paths (/Users/, /home/, etc.)
    sed -i.bak 's|/Users/[^/]*|/Users/REDACTED|g' "$temp_file"
    sed -i.bak 's|/home/[^/]*|/home/REDACTED|g' "$temp_file"
    sed -i.bak 's|/tmp/[^/]*|/tmp/REDACTED|g' "$temp_file"
    
    # Replace absolute Windows paths (C:\Users\, etc.)
    sed -i.bak 's|C:\\Users\\[^\\]*|C:\\Users\\REDACTED|g' "$temp_file"
    sed -i.bak 's|C:/Users/[^/]*|C:/Users/REDACTED|g' "$temp_file"
    
    # Replace email-like patterns
    sed -i.bak 's|[a-zA-Z0-9._%+-]\+@[a-zA-Z0-9.-]\+\.[a-zA-Z]\{2,\}|REDACTED_EMAIL|g' "$temp_file"
    
    # Replace any remaining absolute paths that might contain usernames
    sed -i.bak 's|/[a-zA-Z0-9._-]*/[^/]*/[^/]*|/REDACTED_PATH|g' "$temp_file"
    
    # Remove any build timestamps or machine-specific information
    sed -i.bak 's|[0-9]\{4\}-[0-9]\{2\}-[0-9]\{2\}T[0-9]\{2\}:[0-9]\{2\}:[0-9]\{2\}Z|REDACTED_TIMESTAMP|g' "$temp_file"
    
    # Move the sanitized file to the output location
    mv "$temp_file" "$output_file"
    
    # Clean up backup files
    rm -f "$temp_file.bak"
    
    print_status "Sanitization complete for $output_file"
}

# Function to sanitize HTML coverage report
sanitize_html_coverage() {
    local input_file="$1"
    local output_file="$2"
    
    if [[ ! -f "$input_file" ]]; then
        print_error "Input file $input_file does not exist"
        return 1
    fi
    
    print_status "Sanitizing HTML coverage report $input_file -> $output_file"
    
    # Create a temporary file for processing
    local temp_file=$(mktemp)
    
    # Copy the original file
    cp "$input_file" "$temp_file"
    
    # Replace absolute paths in HTML content
    sed -i.bak 's|/Users/[^/]*|/Users/REDACTED|g' "$temp_file"
    sed -i.bak 's|/home/[^/]*|/home/REDACTED|g' "$temp_file"
    sed -i.bak 's|C:\\Users\\[^\\]*|C:\\Users\\REDACTED|g' "$temp_file"
    sed -i.bak 's|C:/Users/[^/]*|C:/Users/REDACTED|g' "$temp_file"
    
    # Replace email-like patterns in HTML
    sed -i.bak 's|[a-zA-Z0-9._%+-]\+@[a-zA-Z0-9.-]\+\.[a-zA-Z]\{2,\}|REDACTED_EMAIL|g' "$temp_file"
    
    # Replace any machine-specific paths
    sed -i.bak 's|/[a-zA-Z0-9._-]*/[^/]*/[^/]*|/REDACTED_PATH|g' "$temp_file"
    
    # Move the sanitized file to the output location
    mv "$temp_file" "$output_file"
    
    # Clean up backup files
    rm -f "$temp_file.bak"
    
    print_status "HTML sanitization complete for $output_file"
}

# Main function
main() {
    print_status "Starting coverage report sanitization"
    
    # Check if we have any coverage files to sanitize
    local has_coverage_files=false
    
    if [[ -f "coverage.html" ]]; then
        has_coverage_files=true
        sanitize_html_coverage "coverage.html" "coverage-sanitized.html"
    fi
    
    if [[ -f "combined-cover.out" ]]; then
        has_coverage_files=true
        sanitize_file "combined-cover.out" "combined-cover-sanitized.out"
    fi
    
    if [[ -f "cover.out" ]]; then
        has_coverage_files=true
        sanitize_file "cover.out" "cover-sanitized.out"
    fi
    
    # Check for any other coverage files
    local other_coverage_files=$(find . -name "*.cover.out" -o -name "*.coverprofile.out" 2>/dev/null || true)
    if [[ -n "$other_coverage_files" ]]; then
        has_coverage_files=true
        for file in $other_coverage_files; do
            local dir=$(dirname "$file")
            local base=$(basename "$file")
            local sanitized_name="sanitized-${base}"
            sanitize_file "$file" "${dir}/${sanitized_name}"
        done
    fi
    
    if [[ "$has_coverage_files" == "false" ]]; then
        print_warning "No coverage files found to sanitize"
        return 0
    fi
    
    print_status "Coverage report sanitization completed successfully"
    print_status "Sanitized files have been created with 'sanitized-' prefix"
    print_warning "Original coverage files should not be committed to version control"
}

# Run main function
main "$@"
