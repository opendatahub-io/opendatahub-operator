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
    local mode="${3:-}"  # Optional mode parameter for HTML-specific sanitization
    
    if [[ ! -f "$input_file" ]]; then
        print_error "Input file $input_file does not exist"
        return 1
    fi
    
    print_status "Sanitizing $input_file -> $output_file${mode:+ (mode: $mode)}"
    
    # Create a temporary file for processing
    local temp_file
    # Portable across GNU (Linux) and BSD (macOS)
    temp_file="$(mktemp 2>/dev/null || mktemp -t sanitize-coverage)"
    
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
    
    # Replace email-like patterns (including mailto: prefixes and HTML-escaped @ symbols)
    sed -i.bak 's|\(mailto:\)\?[a-zA-Z0-9._%+-]\{1,\}\(@\|&commat;\|&#64;\)[a-zA-Z0-9.-]\{1,\}\.[a-zA-Z]\{2,\}|REDACTED_EMAIL|g' "$temp_file"
    # Replace any remaining absolute paths that might contain usernames
    sed -i.bak 's|/[a-zA-Z0-9._-]*/[^/]*/[^/]*|/REDACTED_PATH|g' "$temp_file"
    
    # Remove any build timestamps or machine-specific information
    sed -i.bak 's|[0-9]\{4\}-[0-9]\{2\}-[0-9]\{2\}T[0-9]\{2\}:[0-9]\{2\}:[0-9]\{2\}Z|REDACTED_TIMESTAMP|g' "$temp_file"
    
    # HTML-specific sanitization if mode is "html"
    if [[ "$mode" == "html" ]]; then
        print_status "Applying HTML-specific sanitization patterns"
        
        # Remove class attributes that might contain sensitive information
        sed -i.bak 's/class="[^"]*"/class="REDACTED_CLASS"/g' "$temp_file"
        
        # Remove data-* attributes that might contain sensitive information
        sed -i.bak 's/data-[a-zA-Z0-9_-]*="[^"]*"/data-REDACTED="REDACTED_VALUE"/g' "$temp_file"
        
        # Remove inline event handlers (onclick, onload, etc.)
        sed -i.bak 's/on[a-zA-Z]*="[^"]*"/onREDACTED="REDACTED_HANDLER"/g' "$temp_file"
        
        # Remove script tag contents that might contain sensitive information
        sed -i.bak 's|<script[^>]*>[^<]*</script>|<script>REDACTED_SCRIPT_CONTENT</script>|g' "$temp_file"
        
        # Remove href/src attributes with absolute file paths
        sed -i.bak 's|href="[^"]*[a-zA-Z]:[^"]*"|href="REDACTED_ABSOLUTE_PATH"|g' "$temp_file"
        sed -i.bak 's|src="[^"]*[a-zA-Z]:[^"]*"|src="REDACTED_ABSOLUTE_PATH"|g' "$temp_file"
        sed -i.bak 's|href="[^"]*/Users/[^"]*"|href="REDACTED_ABSOLUTE_PATH"|g' "$temp_file"
        sed -i.bak 's|src="[^"]*/Users/[^"]*"|src="REDACTED_ABSOLUTE_PATH"|g' "$temp_file"
        sed -i.bak 's|href="[^"]*/home/[^"]*"|href="REDACTED_ABSOLUTE_PATH"|g' "$temp_file"
        sed -i.bak 's|src="[^"]*/home/[^"]*"|src="REDACTED_ABSOLUTE_PATH"|g' "$temp_file"
        
        # Remove mailto links and email patterns in href
        sed -i.bak 's|href="mailto:[^"]*"|href="mailto:REDACTED_EMAIL"|g' "$temp_file"
        sed -i.bak 's|href="[^"]*@[^"]*"|href="REDACTED_EMAIL_LINK"|g' "$temp_file"
    fi
    
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
    
    # Use the centralized sanitize_file function with HTML mode to ensure consistent behavior
    # and apply HTML-specific sanitization patterns
    sanitize_file "$input_file" "$output_file" "html"
    
    print_status "HTML sanitization complete for $output_file"
}

# Main function
main() {
    print_status "Starting coverage report sanitization"
    
    # Check if we have any coverage files to sanitize
    local has_coverage_files=false
    
    if [[ -f "combined-cover.out" ]]; then
        has_coverage_files=true
        sanitize_file "combined-cover.out" "combined-cover-sanitized.out"
        
        # Generate coverage.html from combined-cover.out if it doesn't exist
        if [[ ! -f "coverage.html" ]]; then
            print_status "Generating coverage.html from combined-cover.out"
            if go tool cover -html=combined-cover.out -o coverage.html; then
                print_status "Successfully generated coverage.html"
                sanitize_html_coverage "coverage.html" "coverage-sanitized.html"
            else
                print_warning "Failed to generate coverage.html from combined-cover.out"
            fi
        else
            sanitize_html_coverage "coverage.html" "coverage-sanitized.html"
        fi
    elif [[ -f "coverage.html" ]]; then
        has_coverage_files=true
        sanitize_html_coverage "coverage.html" "coverage-sanitized.html"
    fi
    
    if [[ -f "cover.out" ]]; then
        has_coverage_files=true
        sanitize_file "cover.out" "cover-sanitized.out"
    fi
    
    # Check for any other coverage files
    local other_coverage_files
    other_coverage_files=$(find . -path './vendor' -prune -o -path './vendor/bin/bundle/catalog' -prune -o -name "*.cover.out" -o -name "*.coverprofile.out" -print 2>/dev/null || true)
    if [[ -n "$other_coverage_files" ]]; then
        has_coverage_files=true
        for file in $other_coverage_files; do
            local dir
            dir=$(dirname "$file")
            local base
            base=$(basename "$file")
            local sanitized_name="sanitized-${base}"
            sanitize_file "$file" "${dir}/${sanitized_name}"
        done
    fi
    
    if [[ "$has_coverage_files" == "false" ]]; then
        print_warning "No coverage files found to sanitize"
        return 0
    fi
    
    print_status "Coverage report sanitization completed successfully"
    
    # Build list of actually created sanitized files
    local created_files=()
    
    # Check for each expected sanitized file
    if [[ -f "coverage-sanitized.html" ]]; then
        created_files+=("coverage-sanitized.html")
    fi
    
    if [[ -f "combined-cover-sanitized.out" ]]; then
        created_files+=("combined-cover-sanitized.out")
    fi
    
    if [[ -f "cover-sanitized.out" ]]; then
        created_files+=("cover-sanitized.out")
    fi
    
    # Check for sanitized-*.cover.out files
    local sanitized_cover_files
    sanitized_cover_files=$(find . -path './vendor' -prune -o -path './vendor/bin/bundle/catalog' -prune -o -name "sanitized-*.cover.out" -print 2>/dev/null || true)
    if [[ -n "$sanitized_cover_files" ]]; then
        for file in $sanitized_cover_files; do
            created_files+=("$file")
        done
    fi
    
    # Print the list of actually created files
    if [[ ${#created_files[@]} -gt 0 ]]; then
        local files_list
        files_list=$(IFS=', '; echo "${created_files[*]}")
        print_status "Sanitized files created: $files_list"
    else
        print_status "No sanitized files were created"
    fi
    
    print_status "Note: coverage.html is automatically generated from combined-cover.out when needed"
    print_warning "Original coverage files should not be committed to version control"
}

# Run main function
main "$@"
