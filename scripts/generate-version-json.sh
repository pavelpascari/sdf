#!/usr/bin/env bash
#
# Generate www/public/_/version.json from .release-please-manifest.json.
#
# The generated file is served as a static asset at /_/version.json and
# consumed by the CLI's update checker.
#
# Usage:
#   scripts/generate-version-json.sh
#
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MANIFEST="$ROOT/.release-please-manifest.json"
OUT_DIR="$ROOT/www/public/_"
OUT_FILE="$OUT_DIR/version.json"

CHANGELOG="https://github.com/pavelpascari/sdf/blob/main/CHANGELOG.md"

# ── Read version from release-please manifest ──────────────────────
if [[ ! -f "$MANIFEST" ]]; then
  echo "ERROR: $MANIFEST not found." >&2
  exit 1
fi

VERSION=$(jq -r '.["." ]' "$MANIFEST")
if [[ -z "$VERSION" || "$VERSION" == "null" ]]; then
  echo "ERROR: could not read version from $MANIFEST" >&2
  exit 1
fi

# ── Write the JSON file ───────────────────────────────────────────
mkdir -p "$OUT_DIR"
jq -n --arg v "$VERSION" --arg c "$CHANGELOG" \
  '{ version: $v, changelog: $c }' > "$OUT_FILE"

echo "wrote $OUT_FILE — version $VERSION"
