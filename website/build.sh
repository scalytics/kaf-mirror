#!/usr/bin/env bash
# Assemble the static site. Usage: website/build.sh [outdir]
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
outdir="${1:-"${here}/../_site"}"
python3 "$here/build.py" "$outdir"
