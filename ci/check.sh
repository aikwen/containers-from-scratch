#!/usr/bin/env bash
set -euo pipefail

echo "Checking the built containers-from-scratch binary."
test -x ./containers-from-scratch

echo "No network access is needed for this project check."
