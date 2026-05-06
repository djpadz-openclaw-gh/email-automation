#!/bin/bash
# RBAC Integration Tests for email-automation
# Tests role-based access control against the running deployment
#
# Prerequisites:
#   - API running at localhost:18080 (port-forward or local)
#   - User #1 exists and is admin (migration 014 applied)
#   - A second non-admin user exists or will be created

set -e

API_URL="${API_URL:-http://localhost:18080}"
PASS=0
FAIL=0

green() { echo -e "\033[32m$1\033[0m"; }
red() { echo -e "\033[31m$1\033[0m"; }

assert_status() {
    local test_name="$1"
    local expected="$2"
    local actual="$3"
    if [ "$actual" = "$expected" ]; then
        green "  ✓ $test_name (HTTP $actual)"
        PASS=$((PASS + 1))
    else
        red "  ✗ $test_name (expected HTTP $expected, got $actual)"
        FAIL=$((FAIL + 1))
    fi
}

assert_json() {
    local test_name="$1"
    local jq_expr="$2"
    local expected="$3"
    local body="$4"
    local actual
    actual=$(echo "$body" | jq -r "$jq_expr" 2>/dev/null)
    if [ "$actual" = "$expected" ]; then
        green "  ✓ $test_name ($jq_expr = $expected)"
        PASS=$((PASS + 1))
    else
        red "  ✗ $test_name ($jq_expr: expected '$expected', got '$actual')"
        FAIL=$((FAIL + 1))
    fi
}

echo "=== RBAC Integration Tests ==="
echo "API URL: $API_URL"
echo ""

# --- Setup: Register/login as admin (user #1) ---
echo "=== Setup: Login as admin user ==="

# Login as the first user (admin)
BODY=$(curl -s -X POST "$API_URL/auth/login" \
    -H "Content-Type: application/json" \
    -d '{"username":"dj","password":"testpass123"}')
ADMIN_TOKEN=$(echo "$BODY" | jq -r '.token // empty')

if [ -z "$ADMIN_TOKEN" ]; then
    red "  ✗ Could not login as admin user. Make sure user 'dj' exists with password 'testpass123'"
    echo "  Attempting to register..."
    BODY=$(curl -s -X POST "$API_URL/auth/register" \
        -H "Content-Type: application/json" \
        -d '{"username":"dj","password":"testpass123"}')
    ADMIN_TOKEN=$(echo "$BODY" | jq -r '.token // empty')
    if [ -z "$ADMIN_TOKEN" ]; then
        red "  ✗ Could not register admin user either. Aborting."
        exit 1
    fi
fi
green "  ✓ Admin user authenticated"

# --- Setup: Register a non-admin user ---
echo ""
echo "=== Setup: Create non-admin user ==="

RAND_USER="testuser_$(date +%s)"
BODY=$(curl -s -X POST "$API_URL/auth/register" \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"$RAND_USER\",\"password\":\"testpass456\"}")
USER_TOKEN=$(echo "$BODY" | jq -r '.token // empty')

if [ -z "$USER_TOKEN" ]; then
    # Try login if registration is disabled
    BODY=$(curl -s -X POST "$API_URL/auth/login" \
        -H "Content-Type: application/json" \
        -d "{\"username\":\"$RAND_USER\",\"password\":\"testpass456\"}")
    USER_TOKEN=$(echo "$BODY" | jq -r '.token // empty')
fi

if [ -z "$USER_TOKEN" ]; then
    red "  ✗ Could not create/login non-admin user. Some tests will be skipped."
else
    green "  ✓ Non-admin user '$RAND_USER' authenticated"
fi

# --- Test 1: Unauthenticated requests to admin endpoints ---
echo ""
echo "=== Test: Unauthenticated requests rejected ==="

STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$API_URL/admin/users")
assert_status "GET /admin/users without auth" "401" "$STATUS"

STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$API_URL/admin/tenants")
assert_status "GET /admin/tenants without auth" "401" "$STATUS"

STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$API_URL/admin/rotate-encryption-keys")
assert_status "POST /admin/rotate-encryption-keys without auth" "401" "$STATUS"

# --- Test 2: Non-admin users get 403 on admin endpoints ---
echo ""
echo "=== Test: Non-admin users get 403 Forbidden ==="

if [ -n "$USER_TOKEN" ]; then
    STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
        -H "Authorization: Bearer $USER_TOKEN" "$API_URL/admin/users")
    assert_status "GET /admin/users as non-admin" "403" "$STATUS"

    STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
        -H "Authorization: Bearer $USER_TOKEN" "$API_URL/admin/tenants")
    assert_status "GET /admin/tenants as non-admin" "403" "$STATUS"

    STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
        -H "Authorization: Bearer $USER_TOKEN" \
        -X POST "$API_URL/admin/rotate-encryption-keys")
    assert_status "POST /admin/rotate-encryption-keys as non-admin" "403" "$STATUS"

    STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
        -H "Authorization: Bearer $USER_TOKEN" \
        -X POST "$API_URL/admin/users/1/promote")
    assert_status "POST /admin/users/1/promote as non-admin" "403" "$STATUS"

    STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
        -H "Authorization: Bearer $USER_TOKEN" \
        -X POST "$API_URL/admin/users/1/demote")
    assert_status "POST /admin/users/1/demote as non-admin" "403" "$STATUS"
else
    red "  ⚠ Skipped non-admin tests (no user token)"
fi

# --- Test 3: Admin user can access admin endpoints ---
echo ""
echo "=== Test: Admin user can access admin endpoints ==="

RESULT=$(curl -s -w "\n%{http_code}" \
    -H "Authorization: Bearer $ADMIN_TOKEN" "$API_URL/admin/users")
STATUS=$(echo "$RESULT" | tail -1)
BODY=$(echo "$RESULT" | sed '$d')
assert_status "GET /admin/users as admin" "200" "$STATUS"
assert_json "users list has users array" ".users | type" "array" "$BODY"

# Check that users have role field
FIRST_ROLE=$(echo "$BODY" | jq -r '.users[0].role // empty')
if [ -n "$FIRST_ROLE" ]; then
    green "  ✓ Users include role field (first user role: $FIRST_ROLE)"
    PASS=$((PASS + 1))
else
    red "  ✗ Users missing role field"
    FAIL=$((FAIL + 1))
fi

STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
    -H "Authorization: Bearer $ADMIN_TOKEN" "$API_URL/admin/tenants")
assert_status "GET /admin/tenants as admin" "200" "$STATUS"

# --- Test 4: Role promotion/demotion ---
echo ""
echo "=== Test: Role promotion and demotion ==="

if [ -n "$USER_TOKEN" ]; then
    # Get the non-admin user's ID
    BODY=$(curl -s -H "Authorization: Bearer $USER_TOKEN" "$API_URL/auth/profile")
    NON_ADMIN_ID=$(echo "$BODY" | jq -r '.id // .user_id // empty')

    if [ -n "$NON_ADMIN_ID" ]; then
        # Promote the non-admin user
        RESULT=$(curl -s -w "\n%{http_code}" -X POST \
            -H "Authorization: Bearer $ADMIN_TOKEN" \
            "$API_URL/admin/users/$NON_ADMIN_ID/promote")
        STATUS=$(echo "$RESULT" | tail -1)
        BODY=$(echo "$RESULT" | sed '$d')
        assert_status "POST /admin/users/$NON_ADMIN_ID/promote" "200" "$STATUS"
        assert_json "promote response role" ".role" "admin" "$BODY"

        # Verify the promoted user can now access admin endpoints
        STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
            -H "Authorization: Bearer $USER_TOKEN" "$API_URL/admin/users")
        assert_status "GET /admin/users as newly promoted admin" "200" "$STATUS"

        # Demote the user back
        RESULT=$(curl -s -w "\n%{http_code}" -X POST \
            -H "Authorization: Bearer $ADMIN_TOKEN" \
            "$API_URL/admin/users/$NON_ADMIN_ID/demote")
        STATUS=$(echo "$RESULT" | tail -1)
        BODY=$(echo "$RESULT" | sed '$d')
        assert_status "POST /admin/users/$NON_ADMIN_ID/demote" "200" "$STATUS"
        assert_json "demote response role" ".role" "user" "$BODY"

        # Verify the demoted user can no longer access admin endpoints
        STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
            -H "Authorization: Bearer $USER_TOKEN" "$API_URL/admin/users")
        assert_status "GET /admin/users as demoted user" "403" "$STATUS"
    else
        red "  ⚠ Could not determine non-admin user ID"
    fi
fi

# --- Test 5: Cannot demote user #1 ---
echo ""
echo "=== Test: Cannot demote primary admin (user #1) ==="

RESULT=$(curl -s -w "\n%{http_code}" -X POST \
    -H "Authorization: Bearer $ADMIN_TOKEN" \
    "$API_URL/admin/users/1/demote")
STATUS=$(echo "$RESULT" | tail -1)
BODY=$(echo "$RESULT" | sed '$d')
assert_status "POST /admin/users/1/demote (should be forbidden)" "403" "$STATUS"
assert_json "demote user #1 error" ".error" "cannot demote the primary admin (user #1)" "$BODY"

# --- Test 6: Invalid user ID handling ---
echo ""
echo "=== Test: Invalid user ID handling ==="

STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST \
    -H "Authorization: Bearer $ADMIN_TOKEN" \
    "$API_URL/admin/users/99999/promote")
assert_status "POST /admin/users/99999/promote (not found)" "404" "$STATUS"

STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST \
    -H "Authorization: Bearer $ADMIN_TOKEN" \
    "$API_URL/admin/users/invalid/promote")
assert_status "POST /admin/users/invalid/promote (bad request)" "400" "$STATUS"

# --- Summary ---
echo ""
echo "================================"
echo "Results: $PASS passed, $FAIL failed"
echo "================================"

if [ $FAIL -gt 0 ]; then
    exit 1
fi
