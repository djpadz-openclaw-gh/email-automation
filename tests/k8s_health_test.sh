#!/bin/bash
# Kubernetes Health Check Tests
# Verifies all pods are running, services are accessible, and no crashes

set -e

NAMESPACE="email-automation"
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

echo "=== Pod Status ==="

# Check all pods are running
for component in postgres nats api frontend; do
    READY=$(kubectl get pods -n "$NAMESPACE" -l "app.kubernetes.io/component=$component" -o jsonpath='{.items[0].status.containerStatuses[0].ready}' 2>/dev/null)
    check "$component pod ready" "$READY"
done

echo ""
echo "=== Pod Restarts ==="

# Check no recent restarts (allow up to 5 for initial startup)
for component in postgres nats api frontend; do
    RESTARTS=$(kubectl get pods -n "$NAMESPACE" -l "app.kubernetes.io/component=$component" -o jsonpath='{.items[0].status.containerStatuses[0].restartCount}' 2>/dev/null)
    if [ -n "$RESTARTS" ] && [ "$RESTARTS" -le 5 ]; then
        check "$component restarts <= 5 (actual: $RESTARTS)" "true"
    else
        check "$component restarts <= 5 (actual: ${RESTARTS:-unknown})" "false"
    fi
done

echo ""
echo "=== Services ==="

# Check services exist and have endpoints
for svc in postgres nats api frontend; do
    ENDPOINTS=$(kubectl get endpoints -n "$NAMESPACE" "$svc" -o jsonpath='{.subsets[0].addresses[0].ip}' 2>/dev/null)
    if [ -n "$ENDPOINTS" ]; then
        check "$svc service has endpoints ($ENDPOINTS)" "true"
    else
        check "$svc service has endpoints" "false"
    fi
done

echo ""
echo "=== API Health ==="

# Check API health via pod exec
POD=$(kubectl get pods -n "$NAMESPACE" -l "app.kubernetes.io/component=api" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$POD" ]; then
    HEALTH=$(kubectl exec -n "$NAMESPACE" "$POD" -- wget -qO- http://127.0.0.1:8080/health 2>/dev/null)
    STATUS=$(echo "$HEALTH" | jq -r '.status' 2>/dev/null)
    check "API /health returns ok" "$([ "$STATUS" = "ok" ] && echo true || echo false)"

    READY=$(kubectl exec -n "$NAMESPACE" "$POD" -- wget -qO- http://127.0.0.1:8080/ready 2>/dev/null)
    check "API /ready accessible" "$([ -n "$READY" ] && echo true || echo false)"
else
    check "API pod found" "false"
    check "API /health" "false"
fi

echo ""
echo "=== Frontend Health ==="

FPOD=$(kubectl get pods -n "$NAMESPACE" -l "app.kubernetes.io/component=frontend" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$FPOD" ]; then
    FHEALTH=$(kubectl exec -n "$NAMESPACE" "$FPOD" -- wget -qO- http://127.0.0.1:3000/ 2>/dev/null | head -1)
    check "Frontend serves HTML" "$(echo "$FHEALTH" | grep -q '<!DOCTYPE\|<html' && echo true || echo false)"
else
    check "Frontend pod found" "false"
fi

echo ""
echo "=== Ingress ==="

INGRESS_HOST=$(kubectl get ingress -n "$NAMESPACE" email-automation -o jsonpath='{.spec.rules[0].host}' 2>/dev/null)
check "Ingress configured" "$([ -n "$INGRESS_HOST" ] && echo true || echo false)"

echo ""
echo "=== Resource Usage ==="

kubectl top pods -n "$NAMESPACE" 2>/dev/null || echo "  (metrics-server not available, skipping resource usage)"

echo ""
echo "========================================="
echo "K8s Health: $PASS passed, $FAIL failed"
echo "========================================="

if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
