#!/usr/bin/env bash
# check-encoding.sh -- Reject curly quotes and em dashes in www/ source files.
#
# Scans text files under www/src and www/public for characters that cause
# mojibake when served without explicit UTF-8 charset headers:
#   - Em dash:          U+2014  (UTF-8: e2 80 94)
#   - Left double quote:  U+201C  (UTF-8: e2 80 9c)
#   - Right double quote: U+201D  (UTF-8: e2 80 9d)
#   - Left single quote:  U+2018  (UTF-8: e2 80 98)
#   - Right single quote: U+2019  (UTF-8: e2 80 99)
#
# Returns non-zero if any violation is found.

set -euo pipefail

DIRS=("www/src" "www/public")

# Match UTF-8 byte sequences for the forbidden characters.
PATTERN='\xe2\x80(\x94|\x9c|\x9d|\x98|\x99)'

# File extensions to check.
EXTENSIONS=("*.astro" "*.mdx" "*.md" "*.txt" "*.ts" "*.tsx" "*.js" "*.jsx" "*.json" "*.css" "*.html" "*.yml" "*.yaml" "*.svelte" "*.vue")

INCLUDE_ARGS=()
for ext in "${EXTENSIONS[@]}"; do
  INCLUDE_ARGS+=(--include="$ext")
done

echo "=== Encoding check: no curly quotes or em dashes in www/ sources ==="
echo ""

MATCHES=$(grep -rPn "${INCLUDE_ARGS[@]}" "$PATTERN" "${DIRS[@]}" 2>/dev/null || true)

if [ -n "$MATCHES" ]; then
  echo "FAIL: Found curly quotes or em dashes in the following files:"
  echo ""
  echo "$MATCHES"
  echo ""
  ERRORS=$(echo "$MATCHES" | wc -l)
  echo "FAILED: $ERRORS violation(s) found."
  echo ""
  echo "Replace em dashes with \"--\" and curly quotes with straight quotes."
  exit 1
else
  echo "  ok: No curly quotes or em dashes found."
  exit 0
fi
