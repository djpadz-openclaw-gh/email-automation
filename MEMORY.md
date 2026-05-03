# Email Automation v2 - Project Memory

## Project Overview
- **Purpose:** Email filtering and automation system with AI-powered rule generation
- **Tech Stack:** Go backend (Fiber), Next.js frontend, Kubernetes deployment, Lua rule engine
- **Deployment:** Kubernetes namespace `email-automation`, 2 replicas
- **Docker Registry:** `registry.container-registry.svc.cluster.local:32000`

## Kiro API Configuration
- **Endpoint:** `http://10.152.183.204:9000/v1/messages`
- **API Key:** `aing3Maiwiex0wie3loip9yee0iengo`
- **Auth Header:** `x-api-key` (not Bearer token)
- **Anthropic Version Header:** `2023-06-01`
- **Model:** `claude-sonnet-4-20250514`

## Key Features Implemented

### 1. User Registration & Authentication
- `POST /auth/register/passkey` — Register with username + password
- WebAuthn passkey enrollment and authentication endpoints
- Passwords hashed with bcrypt

### 2. Kiro Translation Endpoints
- `POST /api/kiro/translate/english-to-lua` — Convert English descriptions to Lua rules
- `POST /api/kiro/translate/lua-to-english` — Convert Lua rules to English descriptions
- `POST /api/kiro` — Generic proxy endpoint for frontend compatibility

### 3. Lua Rule Engine with Semantic Analysis
- **Simple pattern matching:** `contains_any()`, string operations for concrete criteria
- **Semantic functions (kiro namespace):**
  - `kiro.classify(email, question)` — Semantic classification with OCR support
  - `kiro.is_actionable(email)` — Determine if email requires action
  - `kiro.is_fake_invoice(email)` — Fraud detection using OCR text
- **Image support:** `email.has_images`, `email.ocr_text` for image-based rules

### 4. Error Handling
- Frontend shows error banner when rule translation fails
- Lua code editor doesn't update on error
- Backend returns `{"error": "...", "message": "..."}` on failure

## System Prompt Guidance (englishToLuaSystemPrompt)

**Key principle:** Prefer simple patterns for concrete criteria, use `kiro.classify()` for semantic/subjective criteria.

**Image-based rules:** When user mentions "picture of", "image contains", "looks like", etc., generate code using `kiro.classify()` with OCR analysis.

Example:
- Input: "If email has image of McAfee invoice, mark as Junk"
- Output: Uses `kiro.classify(email, "Does this image contain McAfee text?")` to analyze image content

## Recent Fixes (May 3, 2026)

### KiroProxy URL Bug (commit fe367e7)
- **Issue:** KiroProxy was appending `/v1/messages` to URL that already contained it
- **Result:** 404 errors on `/api/kiro` endpoint
- **Fix:** Removed duplicate path append

### Image-Based Rule Recognition (commit db28fc3)
- **Issue:** AI was generating rules that checked attachment filenames instead of using OCR
- **Fix:** Updated system prompt with explicit guidance and example for image-based rules
- **Result:** Now correctly generates `kiro.classify()` calls for image content analysis

## Database Schema
- **Users table:** id, username, password_hash, totp_secret, totp_enabled, tenant_id (nullable)
- **Passkeys table:** id, user_id, credential_id, public_key, backup_eligible, backup_state
- **Postgres:** User `emailauto`, password `87cb60e125b0a344f3b7ce41f25225c7`, service at `10.152.183.215:5432`

## Deployment Notes
- Docker image: `registry.container-registry.svc.cluster.local:32000/email-automation:latest`
- Build command: `docker build -t registry.container-registry.svc.cluster.local:32000/email-automation:latest .`
- Push command: `docker push registry.container-registry.svc.cluster.local:32000/email-automation:latest`
- Restart: `kubectl -n email-automation rollout restart deployment/api`

## Known Issues & TODOs
- None currently blocking

## Git Commits (This Session)
- `fe367e7` — Fix: remove duplicate /v1/messages path in KiroProxy handler
- `89e42dd` — Add error banner, Kiro namespace in Lua, updated prompts, and OCR support
- `db28fc3` — Improve system prompt to recognize and handle image-based rules with kiro.classify()
