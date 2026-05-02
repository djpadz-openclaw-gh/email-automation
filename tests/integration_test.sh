#!/bin/bash
# API Integration Tests for email-automation
# Tests against the running K8s deployment via port-forward

set -e

API_URL="http://localhost:18080"
ADMIN_KEY="ea_45d384f375af77460856c9a50110463a2bb824ff8324b7ccc2594e6a48673a41"
TENANT_KEY=""
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

echo "=== Health Checks ==="
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$API_URL/health")
assert_status "GET /health" "200" "$STATUS"

STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$API_URL/ready")
assert_status "GET /ready" "200" "$STATUS"

BODY=$(curl -s "$API_URL/health")
assert_json "health status" ".status" "ok" "$BODY"

echo ""
echo "=== Auth Tests ==="
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$API_URL/api/v1/rules")
assert_status "GET /api/v1/rules without auth" "401" "$STATUS"

STATUS=$(curl -s -o /dev/null -w "%{http_code}" -H "X-API-Key: invalid" "$API_URL/api/v1/rules")
assert_status "GET /api/v1/rules with invalid key" "401" "$STATUS"

echo ""
echo "=== Admin: Create Tenant ==="
BODY=$(curl -s -w "\n%{http_code}" -X POST "$API_URL/admin/tenants" \
    -H "Content-Type: application/json" \
    -H "X-API-Key: $ADMIN_KEY" \
    -d '{"name":"Test Tenant","slug":"test-tenant"}')
STATUS=$(echo "$BODY" | tail -1)
RESPONSE=$(echo "$BODY" | head -1)
assert_status "POST /admin/tenants" "201" "$STATUS"

TENANT_KEY=$(echo "$RESPONSE" | jq -r '.api_key')
TENANT_ID=$(echo "$RESPONSE" | jq -r '.id')
echo "  Tenant ID: $TENANT_ID, API Key: ${TENANT_KEY:0:15}..."

echo ""
echo "=== Rules CRUD ==="

# Create rule
BODY=$(curl -s -w "\n%{http_code}" -X POST "$API_URL/api/v1/rules" \
    -H "Content-Type: application/json" \
    -H "X-API-Key: $TENANT_KEY" \
    -d '{
        "name": "Test Rule",
        "description": "Integration test rule",
        "lua_code": "if contains(email.subject, \"test\") then return move(\"@Test\") end\nreturn skip()",
        "priority": 50,
        "active": true
    }')
STATUS=$(echo "$BODY" | tail -1)
RESPONSE=$(echo "$BODY" | head -1)
assert_status "POST /api/v1/rules (create)" "201" "$STATUS"
assert_json "rule name" ".name" "Test Rule" "$RESPONSE"
assert_json "rule active" ".active" "true" "$RESPONSE"

RULE_ID=$(echo "$RESPONSE" | jq -r '.id')
echo "  Rule ID: $RULE_ID"

# List rules
BODY=$(curl -s -w "\n%{http_code}" "$API_URL/api/v1/rules" \
    -H "X-API-Key: $TENANT_KEY")
STATUS=$(echo "$BODY" | tail -1)
RESPONSE=$(echo "$BODY" | head -1)
assert_status "GET /api/v1/rules (list)" "200" "$STATUS"
COUNT=$(echo "$RESPONSE" | jq 'length')
assert_json "rules count" "length" "1" "$RESPONSE"

# Get single rule
BODY=$(curl -s -w "\n%{http_code}" "$API_URL/api/v1/rules/$RULE_ID" \
    -H "X-API-Key: $TENANT_KEY")
STATUS=$(echo "$BODY" | tail -1)
RESPONSE=$(echo "$BODY" | head -1)
assert_status "GET /api/v1/rules/$RULE_ID" "200" "$STATUS"
assert_json "rule id" ".id" "$RULE_ID" "$RESPONSE"

# Update rule
BODY=$(curl -s -w "\n%{http_code}" -X PUT "$API_URL/api/v1/rules/$RULE_ID" \
    -H "Content-Type: application/json" \
    -H "X-API-Key: $TENANT_KEY" \
    -d '{
        "name": "Updated Rule",
        "description": "Updated description",
        "lua_code": "return skip()",
        "priority": 10,
        "active": false
    }')
STATUS=$(echo "$BODY" | tail -1)
RESPONSE=$(echo "$BODY" | head -1)
assert_status "PUT /api/v1/rules/$RULE_ID (update)" "200" "$STATUS"
assert_json "updated name" ".name" "Updated Rule" "$RESPONSE"

echo ""
echo "=== Rule Validation ==="

# Valid Lua
BODY=$(curl -s -w "\n%{http_code}" -X POST "$API_URL/api/v1/rules/validate" \
    -H "Content-Type: application/json" \
    -H "X-API-Key: $TENANT_KEY" \
    -d '{"lua_code": "return skip()"}')
STATUS=$(echo "$BODY" | tail -1)
RESPONSE=$(echo "$BODY" | head -1)
assert_status "POST /api/v1/rules/validate (valid)" "200" "$STATUS"
assert_json "valid lua" ".valid" "true" "$RESPONSE"

# Invalid Lua
BODY=$(curl -s -w "\n%{http_code}" -X POST "$API_URL/api/v1/rules/validate" \
    -H "Content-Type: application/json" \
    -H "X-API-Key: $TENANT_KEY" \
    -d '{"lua_code": "if then end end garbage"}')
STATUS=$(echo "$BODY" | tail -1)
RESPONSE=$(echo "$BODY" | head -1)
assert_status "POST /api/v1/rules/validate (invalid)" "200" "$STATUS"
assert_json "invalid lua" ".valid" "false" "$RESPONSE"

echo ""
echo "=== Rule Testing ==="

# Test rule against sample email
BODY=$(curl -s -w "\n%{http_code}" -X POST "$API_URL/api/v1/rules/test" \
    -H "Content-Type: application/json" \
    -H "X-API-Key: $TENANT_KEY" \
    -d '{
        "lua_code": "if contains(email.subject, \"shipped\") then return move(\"@Orders\", \"order shipped\") end\nreturn skip()",
        "email": {
            "message_id": "test-001",
            "subject": "Your order has shipped",
            "sender_name": "Amazon",
            "sender_address": "ship@amazon.com",
            "recipients": ["user@example.com"],
            "date": "2025-01-01T00:00:00Z",
            "age_seconds": 90000,
            "body_preview": "Your package...",
            "has_attachments": false,
            "attachment_names": [],
            "attachment_types": [],
            "headers": {},
            "folder": "INBOX",
            "account_id": 1
        }
    }')
STATUS=$(echo "$BODY" | tail -1)
RESPONSE=$(echo "$BODY" | head -1)
assert_status "POST /api/v1/rules/test (match)" "200" "$STATUS"
assert_json "test action" ".action" "move" "$RESPONSE"
assert_json "test target" ".target" "@Orders" "$RESPONSE"

# Test rule that doesn't match
BODY=$(curl -s -w "\n%{http_code}" -X POST "$API_URL/api/v1/rules/test" \
    -H "Content-Type: application/json" \
    -H "X-API-Key: $TENANT_KEY" \
    -d '{
        "lua_code": "if contains(email.subject, \"shipped\") then return move(\"@Orders\") end\nreturn skip()",
        "email": {
            "message_id": "test-002",
            "subject": "Hello world",
            "sender_name": "Bob",
            "sender_address": "bob@example.com",
            "recipients": ["user@example.com"],
            "date": "2025-01-01T00:00:00Z",
            "age_seconds": 100,
            "body_preview": "Hi there",
            "has_attachments": false,
            "attachment_names": [],
            "attachment_types": [],
            "headers": {},
            "folder": "INBOX",
            "account_id": 1
        }
    }')
STATUS=$(echo "$BODY" | tail -1)
RESPONSE=$(echo "$BODY" | head -1)
assert_status "POST /api/v1/rules/test (no match)" "200" "$STATUS"
assert_json "test skip" ".action" "skip" "$RESPONSE"

echo ""
echo "=== Accounts CRUD ==="

# Create account
BODY=$(curl -s -w "\n%{http_code}" -X POST "$API_URL/api/v1/accounts" \
    -H "Content-Type: application/json" \
    -H "X-API-Key: $TENANT_KEY" \
    -d '{
        "name": "Test Account",
        "email": "test@example.com",
        "provider": "imap",
        "imap_host": "imap.example.com",
        "imap_port": 993,
        "imap_tls": true,
        "username": "test@example.com",
        "password": "testpass",
        "active": false
    }')
STATUS=$(echo "$BODY" | tail -1)
RESPONSE=$(echo "$BODY" | head -1)
assert_status "POST /api/v1/accounts (create)" "201" "$STATUS"
assert_json "account name" ".name" "Test Account" "$RESPONSE"
# Password is omitted from JSON (omitempty), so jq returns null
assert_json "password redacted" ".password" "null" "$RESPONSE"

ACCOUNT_ID=$(echo "$RESPONSE" | jq -r '.id')

# List accounts
BODY=$(curl -s -w "\n%{http_code}" "$API_URL/api/v1/accounts" \
    -H "X-API-Key: $TENANT_KEY")
STATUS=$(echo "$BODY" | tail -1)
RESPONSE=$(echo "$BODY" | head -1)
assert_status "GET /api/v1/accounts (list)" "200" "$STATUS"
assert_json "accounts count" "length" "1" "$RESPONSE"

# Get account
BODY=$(curl -s -w "\n%{http_code}" "$API_URL/api/v1/accounts/$ACCOUNT_ID" \
    -H "X-API-Key: $TENANT_KEY")
STATUS=$(echo "$BODY" | tail -1)
assert_status "GET /api/v1/accounts/$ACCOUNT_ID" "200" "$STATUS"

echo ""
echo "=== Multi-Tenant Isolation ==="

# Create second tenant
BODY=$(curl -s -w "\n%{http_code}" -X POST "$API_URL/admin/tenants" \
    -H "Content-Type: application/json" \
    -H "X-API-Key: $ADMIN_KEY" \
    -d '{"name":"Tenant 2","slug":"tenant-2"}')
RESPONSE=$(echo "$BODY" | head -1)
TENANT2_KEY=$(echo "$RESPONSE" | jq -r '.api_key')

# Tenant 2 should see no rules
BODY=$(curl -s -w "\n%{http_code}" "$API_URL/api/v1/rules" \
    -H "X-API-Key: $TENANT2_KEY")
STATUS=$(echo "$BODY" | tail -1)
RESPONSE=$(echo "$BODY" | head -1)
assert_status "Tenant 2 sees no rules" "200" "$STATUS"
assert_json "tenant 2 rules empty" "length" "0" "$RESPONSE"

# Tenant 2 should see no accounts
BODY=$(curl -s -w "\n%{http_code}" "$API_URL/api/v1/accounts" \
    -H "X-API-Key: $TENANT2_KEY")
STATUS=$(echo "$BODY" | tail -1)
RESPONSE=$(echo "$BODY" | head -1)
assert_status "Tenant 2 sees no accounts" "200" "$STATUS"
assert_json "tenant 2 accounts empty" "length" "0" "$RESPONSE"

# Tenant 2 can't access Tenant 1's rule
BODY=$(curl -s -w "\n%{http_code}" "$API_URL/api/v1/rules/$RULE_ID" \
    -H "X-API-Key: $TENANT2_KEY")
STATUS=$(echo "$BODY" | tail -1)
assert_status "Tenant 2 can't see Tenant 1 rule" "404" "$STATUS"

echo ""
echo "=== Error Handling ==="

# Create rule with invalid Lua
BODY=$(curl -s -w "\n%{http_code}" -X POST "$API_URL/api/v1/rules" \
    -H "Content-Type: application/json" \
    -H "X-API-Key: $TENANT_KEY" \
    -d '{"name":"Bad Rule","lua_code":"if then end end","priority":1,"active":true}')
STATUS=$(echo "$BODY" | tail -1)
assert_status "POST /api/v1/rules (invalid lua)" "400" "$STATUS"

# Invalid JSON body
BODY=$(curl -s -w "\n%{http_code}" -X POST "$API_URL/api/v1/rules" \
    -H "Content-Type: application/json" \
    -H "X-API-Key: $TENANT_KEY" \
    -d 'not json')
STATUS=$(echo "$BODY" | tail -1)
assert_status "POST /api/v1/rules (invalid json)" "400" "$STATUS"

# Non-existent rule
BODY=$(curl -s -w "\n%{http_code}" "$API_URL/api/v1/rules/99999" \
    -H "X-API-Key: $TENANT_KEY")
STATUS=$(echo "$BODY" | tail -1)
assert_status "GET /api/v1/rules/99999 (not found)" "404" "$STATUS"

echo ""
echo "=== Cleanup ==="

# Delete rule
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "$API_URL/api/v1/rules/$RULE_ID" \
    -H "X-API-Key: $TENANT_KEY")
assert_status "DELETE /api/v1/rules/$RULE_ID" "204" "$STATUS"

# Delete account
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "$API_URL/api/v1/accounts/$ACCOUNT_ID" \
    -H "X-API-Key: $TENANT_KEY")
assert_status "DELETE /api/v1/accounts/$ACCOUNT_ID" "204" "$STATUS"

# Verify deletion
BODY=$(curl -s -w "\n%{http_code}" "$API_URL/api/v1/rules" \
    -H "X-API-Key: $TENANT_KEY")
STATUS=$(echo "$BODY" | tail -1)
RESPONSE=$(echo "$BODY" | head -1)
assert_status "Rules empty after delete" "200" "$STATUS"
assert_json "rules count after delete" "length" "0" "$RESPONSE"

echo ""
echo "========================================="
echo "Results: $PASS passed, $FAIL failed"
echo "========================================="

if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
