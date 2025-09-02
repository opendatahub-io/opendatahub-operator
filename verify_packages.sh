#!/bin/bash

# Script to verify package declarations across the repository
# and identify any mismatches

echo "=== Package Declaration Verification Script ==="
echo

# Find all Go files in the repository
echo "Scanning for Go files..."
go_files=$(find . -name "*.go" -type f | grep -v vendor | grep -v .git)

echo "Found $(echo "$go_files" | wc -l) Go files"
echo

# Extract package declarations and file paths
echo "Package declarations found:"
echo "---------------------------"

# Use a simpler approach without associative arrays
echo "$go_files" | while read -r file; do
    if [ -f "$file" ]; then
        # Extract package declaration (first line that starts with "package ")
        package_line=$(grep -m 1 "^package " "$file" 2>/dev/null)
        if [ -n "$package_line" ]; then
            package_name=$(echo "$package_line" | sed 's/^package //')
            echo "$file: $package_name"
        fi
    fi
done

echo
echo "=== Files with 'package main' ==="
echo "--------------------------------"
echo "$go_files" | while read -r file; do
    if [ -f "$file" ]; then
        package_line=$(grep -m 1 "^package " "$file" 2>/dev/null)
        if [ -n "$package_line" ]; then
            package_name=$(echo "$package_line" | sed 's/^package //')
            if [ "$package_name" = "main" ]; then
                echo "$file: $package_name"
            fi
        fi
    fi
done

echo
echo "=== Test files with potential package issues ==="
echo "----------------------------------------------"
echo "$go_files" | while read -r file; do
    if [ -f "$file" ] && [[ "$file" == *_test.go ]]; then
        package_line=$(grep -m 1 "^package " "$file" 2>/dev/null)
        if [ -n "$package_line" ]; then
            package_name=$(echo "$package_line" | sed 's/^package //')
            dir_name=$(dirname "$file" | sed 's|^\./||')
            
            # Extract expected package name from directory structure
            expected_package=$(echo "$dir_name" | sed 's|/|_|g' | sed 's|^_||')
            
            # For test files, the package should typically match the directory or be "main" for standalone tests
            if [ "$package_name" != "$expected_package" ] && [ "$package_name" != "main" ]; then
                echo "WARNING: $file declares package '$package_name' but expected '$expected_package'"
            fi
        fi
    fi
done

echo
echo "=== Verification complete ==="
