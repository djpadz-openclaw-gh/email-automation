#!/bin/bash
# Frontend Smoke Tests
# Tests the frontend is serving correctly and key UI elements are present

set -e

PASS=0
FAIL=0

green() { echo -e "\033[32m$1\033[0m"; }
red() { echo -e "\033[31m$1\033[0m"; }

check() {
    local name="$1"
    local result="$2"
    if [ "$result" = "true" ]; then
        green "  ✓ $name"
        PASS=$((PASS + 1))
    else
        red "  ✗ $name"
        FAIL=$((FAIL + 1))
    fi
}

# Get frontend pod
FPOD=$(kubectl get pods -n email-automation -l "app.kubernetes.io/component=frontend" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

if [ -z "$FPOD" ]; then
    red "Frontend pod not found"
    exit 1
fi

echo "=== Frontend Page Load ==="

# Fetch the main page
PAGE=$(kubectl exec -n email-automation "$FPOD" -- wget -qO- http://127.0.0.1:3000/ 2>/dev/null)

# Check basic HTML structure
check "Returns HTML document" "$(echo "$PAGE" | grep -q '<!DOCTYPE html>' && echo true || echo false)"
check "Has html tag" "$(echo "$PAGE" | grep -q '<html' && echo true || echo false)"
check "Has head tag" "$(echo "$PAGE" | grep -q '<head' && echo true || echo false)"
check "Has body tag" "$(echo "$PAGE" | grep -q '<body' && echo true || echo false)"

echo ""
echo "=== Frontend Assets ==="

# Check that Next.js chunks are referenced
check "References JS chunks" "$(echo "$PAGE" | grep -q '/_next/' && echo true || echo false)"
check "Has meta viewport" "$(echo "$PAGE" | grep -q 'viewport' && echo true || echo false)"

echo ""
echo "=== Frontend Title ==="

TITLE=$(echo "$PAGE" | grep -oP '<title[^>]*>\K[^<]+' 2>/dev/null || echo "")
if [ -n "$TITLE" ]; then
    check "Has page title: $TITLE" "true"
else
    check "Has page title" "false"
fi

echo ""
echo "=== Next.js Static Assets ==="

# Check that static assets are accessible
STATIC_PATH=$(echo "$PAGE" | grep -oP '/_next/static/[^"]+' | head -1)
if [ -n "$STATIC_PATH" ]; then
    ASSET_STATUS=$(kubectl exec -n email-automation "$FPOD" -- wget -qO /dev/null -S "http://127.0.0.1:3000${STATIC_PATH}" 2>&1 | grep "HTTP/" | tail -1 | awk '{print $2}')
    check "Static asset accessible (${STATIC_PATH:0:50}...)" "$([ "$ASSET_STATUS" = "200" ] && echo true || echo false)"
else
    # Try alternative check
    check "Static assets referenced in HTML" "$(echo "$PAGE" | grep -q '_next/static' && echo true || echo false)"
fi

echo ""
echo "========================================="
echo "Frontend Smoke: $PASS passed, $FAIL failed"
echo "========================================="

if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
