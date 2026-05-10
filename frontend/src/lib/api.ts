// Use relative paths - Next.js rewrites will proxy to backend
const API_URL = '';

export interface Rule {
  id: number;
  tenant_id: number;
  name: string;
  description: string;
  lua_code: string;
  priority: number;
  active: boolean;
  source: string;    // 'manual' or 'auto-learned'
  approved: boolean;
  uses_ai: boolean;  // true if lua_code contains kiro.* calls
  created_at: string;
  updated_at: string;
}

export interface Account {
  id: number;
  tenant_id: number;
  name: string;
  email: string;
  provider: string;
  imap_host: string;
  imap_port: number;
  imap_tls: boolean;
  username: string;
  active: boolean;
  last_sync_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface RuleResult {
  action: string;
  target: string;
  delay: number;
  reason: string;
}

export interface ExecutionLog {
  id: number;
  rule_id: number;
  account_id: number;
  message_id: string;
  subject: string;
  sender: string;
  action: string;
  target: string;
  success: boolean;
  error: string;
  executed_at: string;
}

export interface DryRunMatch {
  message_id: string;
  subject: string;
  sender_address: string;
  action: string;
  target: string;
  reason: string;
}

export interface DryRunResult {
  total_scanned: number;
  total_matched: number;
  matches: DryRunMatch[];
  cancelled?: boolean;
}

export interface ExecuteResult {
  total_scanned: number;
  total_executed: number;
  total_failed: number;
  results: DryRunMatch[];
  errors?: string[];
  cancelled?: boolean;
}

export interface DeferredAction {
  id: number;
  rule_id: number;
  account_id: number;
  message_id: string;
  action: string;
  target: string;
  execute_at: string;
  executed: boolean;
  error: string;
  created_at: string;
  rule_name: string;
}

export interface DeferredActionsResponse {
  deferred_actions: DeferredAction[];
}

export interface EmailContext {
  message_id: string;
  subject: string;
  sender_name: string;
  sender_address: string;
  recipients: string[];
  date: string;
  age_seconds: number;
  body_preview: string;
  has_attachments: boolean;
  attachment_names: string[];
  attachment_types: string[];
  headers: Record<string, string>;
  folder: string;
  account_id: number;
}

export interface AuthUser {
  id: number;
  username: string;
  totp_enabled: boolean;
}

export interface ConfigResponse {
  registration_enabled: boolean;
}

export interface AuthResponse {
  token: string;
  expires_at: string;
  user: AuthUser;
}

export interface AdminUser {
  id: number;
  username: string;
  ai_enabled: boolean;
  totp_enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface PasskeyInfo {
  id: number;
  name: string;
  created_at: string;
  last_used_at: string | null;
}

export interface APIKeyInfo {
  id: number;
  name: string;
  prefix: string;
  key?: string;
  created_at: string;
  last_used_at: string | null;
}

export interface TOTPSetupResponse {
  secret: string;
  url: string;
  qr_code: string;
}

class ApiClient {
  private token: string;
  private onUnauthorized: (() => void) | null = null;

  constructor(token: string = '') {
    this.token = token;
  }

  setToken(token: string) {
    this.token = token;
  }

  // Legacy support
  setApiKey(key: string) {
    this.token = key;
  }

  setOnUnauthorized(callback: () => void) {
    this.onUnauthorized = callback;
  }

  private async request<T>(path: string, options: RequestInit = {}): Promise<T> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...(options.headers as Record<string, string>),
    };

    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`;
    }

    const url = API_URL ? `${API_URL}${path}` : path;
    const res = await fetch(url, {
      ...options,
      headers,
    });

    if (!res.ok) {
      const body = await res.json().catch(() => ({ error: res.statusText }));
      
      // For 401, check if it's TOTP required before treating as session expired
      if (res.status === 401) {
        const errorMsg = body.error || res.statusText;
        // If it's TOTP required, throw that error so LoginForm can handle it
        if (errorMsg.includes('TOTP token required') || body.totp_required) {
          throw new Error(errorMsg);
        }
        // Otherwise it's a real session expiration
        if (this.onUnauthorized) {
          this.onUnauthorized();
        }
      }
      
      throw new Error(body.error || `API error: ${res.status}`);
    }

    if (res.status === 204) return {} as T;
    return res.json();
  }

  // Send raw request (for WebAuthn where body is not JSON)
  private async requestRaw<T>(path: string, body: string, method: string = 'POST'): Promise<T> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    };

    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`;
    }

    const url = API_URL ? `${API_URL}${path}` : path;
    const res = await fetch(url, {
      method,
      headers,
      body,
    });

    if (!res.ok) {
      const respBody = await res.json().catch(() => ({ error: res.statusText }));
      
      // For 401, check if it's TOTP required before treating as session expired
      if (res.status === 401) {
        const errorMsg = respBody.error || res.statusText;
        // If it's TOTP required, throw that error so LoginForm can handle it
        if (errorMsg.includes('TOTP token required') || respBody.totp_required) {
          throw new Error(errorMsg);
        }
        // Otherwise it's a real session expiration
        if (this.onUnauthorized) {
          this.onUnauthorized();
        }
      }
      
      throw new Error(respBody.error || `API error: ${res.status}`);
    }

    return res.json();
  }

  // --- Config ---
  async getConfig(): Promise<ConfigResponse> {
    return this.request<ConfigResponse>('/config', { method: 'GET' });
  }

  // --- Auth ---
  async register(username: string, password: string): Promise<AuthResponse> {
    return this.request<AuthResponse>('/auth/register', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    });
  }

  async login(username: string, password: string, totpToken?: string): Promise<AuthResponse> {
    return this.request<AuthResponse>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password, totp_token: totpToken }),
    });
  }

  async logout(): Promise<void> {
    await this.request('/auth/logout', { method: 'POST' });
  }

  async getProfile(): Promise<{ id: number; username: string; totp_enabled: boolean; passkey_count: number; created_at: string }> {
    return this.request('/auth/profile');
  }

  async changePassword(currentPassword: string, newPassword: string): Promise<{ message: string }> {
    return this.request('/auth/password/change', {
      method: 'POST',
      body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
    });
  }

  // --- TOTP ---
  async totpSetup(): Promise<TOTPSetupResponse> {
    return this.request<TOTPSetupResponse>('/auth/totp/setup', { method: 'POST' });
  }

  async totpVerify(totpToken: string): Promise<{ message: string }> {
    return this.request('/auth/totp/verify', {
      method: 'POST',
      body: JSON.stringify({ totp_token: totpToken }),
    });
  }

  async totpDisable(password: string): Promise<{ message: string }> {
    return this.request('/auth/totp/disable', {
      method: 'POST',
      body: JSON.stringify({ password }),
    });
  }

  // --- Passkeys ---
  async passkeyRegisterBegin(): Promise<PublicKeyCredentialCreationOptions> {
    return this.request('/auth/passkey/register/begin', { method: 'POST' });
  }

  async passkeyRegisterComplete(credential: PublicKeyCredential, name?: string): Promise<{ message: string; passkey: { id: number; name: string } }> {
    const attestationResponse = credential.response as AuthenticatorAttestationResponse;
    const body = JSON.stringify({
      id: credential.id,
      rawId: bufferToBase64url(credential.rawId),
      type: credential.type,
      response: {
        attestationObject: bufferToBase64url(attestationResponse.attestationObject),
        clientDataJSON: bufferToBase64url(attestationResponse.clientDataJSON),
      },
    });
    const queryParam = name ? `?name=${encodeURIComponent(name)}` : '';
    return this.requestRaw(`/auth/passkey/register/complete${queryParam}`, body);
  }

  async passkeyAuthenticateBegin(username?: string): Promise<{ publicKey: PublicKeyCredentialRequestOptions; user_id?: number }> {
    return this.request('/auth/passkey/authenticate/begin', {
      method: 'POST',
      body: JSON.stringify({ username: username || '' }),
    });
  }

  async passkeyAuthenticateComplete(credential: PublicKeyCredential): Promise<AuthResponse> {
    const assertionResponse = credential.response as AuthenticatorAssertionResponse;
    const body = JSON.stringify({
      id: credential.id,
      rawId: bufferToBase64url(credential.rawId),
      type: credential.type,
      response: {
        authenticatorData: bufferToBase64url(assertionResponse.authenticatorData),
        clientDataJSON: bufferToBase64url(assertionResponse.clientDataJSON),
        signature: bufferToBase64url(assertionResponse.signature),
        userHandle: assertionResponse.userHandle ? bufferToBase64url(assertionResponse.userHandle) : null,
      },
    });
    return this.requestRaw('/auth/passkey/authenticate/complete', body);
  }

  async passkeyList(): Promise<PasskeyInfo[]> {
    return this.request<PasskeyInfo[]>('/auth/passkey/list', { method: 'POST' });
  }

  async passkeyDelete(id: number): Promise<void> {
    await this.request(`/auth/passkey/${id}`, { method: 'DELETE' });
  }

  // --- API Keys ---
  async apiKeyCreate(name: string): Promise<APIKeyInfo & { key: string; message: string }> {
    return this.request('/auth/apikeys', {
      method: 'POST',
      body: JSON.stringify({ name }),
    });
  }

  async apiKeyList(): Promise<APIKeyInfo[]> {
    return this.request<APIKeyInfo[]>('/auth/apikeys');
  }

  async apiKeyDelete(id: number): Promise<void> {
    await this.request(`/auth/apikeys/${id}`, { method: 'DELETE' });
  }

  // --- Rules ---
  async listRules(): Promise<Rule[]> {
    return this.request<Rule[]>('/api/v1/rules');
  }

  async listSuggestedRules(): Promise<Rule[]> {
    return this.request<Rule[]>('/api/v1/rules/suggested');
  }

  async getRule(id: number): Promise<Rule> {
    return this.request<Rule>(`/api/v1/rules/${id}`);
  }

  async createRule(rule: Partial<Rule>): Promise<Rule> {
    return this.request<Rule>('/api/v1/rules', {
      method: 'POST',
      body: JSON.stringify(rule),
    });
  }

  async updateRule(id: number, rule: Partial<Rule>): Promise<Rule> {
    return this.request<Rule>(`/api/v1/rules/${id}`, {
      method: 'PUT',
      body: JSON.stringify(rule),
    });
  }

  async deleteRule(id: number): Promise<void> {
    await this.request(`/api/v1/rules/${id}`, { method: 'DELETE' });
  }

  async approveRule(id: number): Promise<{ message: string }> {
    return this.request(`/api/v1/rules/${id}/approve`, { method: 'POST' });
  }

  async testRule(luaCode: string, email: EmailContext): Promise<RuleResult> {
    return this.request<RuleResult>('/api/v1/rules/test', {
      method: 'POST',
      body: JSON.stringify({ lua_code: luaCode, email }),
    });
  }

  async validateRule(luaCode: string): Promise<{ valid: boolean; error?: string }> {
    return this.request('/api/v1/rules/validate', {
      method: 'POST',
      body: JSON.stringify({ lua_code: luaCode }),
    });
  }

  // --- OAuth2 ---
  async listOAuthProviders(): Promise<{ providers: { name: string }[] }> {
    return this.request<{ providers: { name: string }[] }>('/api/v1/oauth2/providers');
  }

  async oauthConnect(provider: string): Promise<{ auth_url: string; state: string }> {
    return this.request<{ auth_url: string; state: string }>(`/api/v1/oauth2/connect/${encodeURIComponent(provider)}`);
  }

  async oauthRefreshToken(accountId: number): Promise<{ message: string; expires_at: string; expires_in: number }> {
    return this.request(`/api/v1/oauth2/refresh/${accountId}`, { method: 'POST' });
  }

  async oauthDisconnect(accountId: number): Promise<{ message: string }> {
    return this.request(`/api/v1/oauth2/disconnect/${accountId}`, { method: 'POST' });
  }

  // --- Accounts ---
  async listAccounts(): Promise<Account[]> {
    return this.request<Account[]>('/api/v1/accounts');
  }

  async createAccount(account: Partial<Account>): Promise<Account> {
    return this.request<Account>('/api/v1/accounts', {
      method: 'POST',
      body: JSON.stringify(account),
    });
  }

  async updateAccount(id: number, account: Partial<Account>): Promise<Account> {
    return this.request<Account>(`/api/v1/accounts/${id}`, {
      method: 'PUT',
      body: JSON.stringify(account),
    });
  }

  async deleteAccount(id: number): Promise<void> {
    await this.request(`/api/v1/accounts/${id}`, { method: 'DELETE' });
  }

  // --- Logs ---
  async listLogs(limit: number = 50): Promise<ExecutionLog[]> {
    return this.request<ExecutionLog[]>(`/api/v1/logs?limit=${limit}`);
  }

  // --- Dry Run & Execute ---
  async dryRunRule(id: number, luaCode?: string, limit?: number, signal?: AbortSignal): Promise<DryRunResult> {
    const body: Record<string, unknown> = {};
    if (luaCode) body.lua_code = luaCode;
    if (limit && limit > 0) body.limit = limit;
    return this.request<DryRunResult>(`/api/v1/rules/${id}/dry-run`, {
      method: 'POST',
      body: JSON.stringify(body),
      signal,
    });
  }

  async dryRunAdHoc(luaCode: string, limit?: number, signal?: AbortSignal): Promise<DryRunResult> {
    const body: Record<string, unknown> = { lua_code: luaCode };
    if (limit && limit > 0) body.limit = limit;
    return this.request<DryRunResult>('/api/v1/rules/dry-run', {
      method: 'POST',
      body: JSON.stringify(body),
      signal,
    });
  }

  async cancelAdHocOperation(): Promise<{ message: string }> {
    return this.request('/api/v1/rules/cancel-adhoc', { method: 'POST' });
  }

  async executeRule(id: number, luaCode?: string, limit?: number, signal?: AbortSignal): Promise<ExecuteResult> {
    const body: Record<string, unknown> = {};
    if (luaCode) body.lua_code = luaCode;
    if (limit && limit > 0) body.limit = limit;
    return this.request<ExecuteResult>(`/api/v1/rules/${id}/execute`, {
      method: 'POST',
      body: JSON.stringify(body),
      signal,
    });
  }

  async cancelRuleOperation(id: number): Promise<{ message: string }> {
    return this.request(`/api/v1/rules/${id}/cancel`, { method: 'POST' });
  }

  // --- Deferred Actions ---
  async listDeferredActions(): Promise<DeferredActionsResponse> {
    return this.request<DeferredActionsResponse>('/api/v1/deferred-actions');
  }

  async cancelDeferredAction(id: number): Promise<{ message: string }> {
    return this.request(`/api/v1/deferred-actions/${id}`, { method: 'DELETE' });
  }

  // --- Reorder ---
  async reorderRules(ruleIds: number[]): Promise<{ message: string }> {
    return this.request('/api/v1/rules/reorder', {
      method: 'PATCH',
      body: JSON.stringify({ rule_ids: ruleIds }),
    });
  }

  // --- Bulk Operations ---
  async bulkDeleteRules(ruleIds: number[]): Promise<{ message: string; deleted: number }> {
    return this.request('/api/v1/rules/bulk-delete', {
      method: 'POST',
      body: JSON.stringify({ rule_ids: ruleIds }),
    });
  }

  // --- Settings: Exempt Folders ---
  async listExemptFolders(): Promise<{ exempt_folders: string[] }> {
    return this.request<{ exempt_folders: string[] }>('/api/v1/settings/exempt-folders');
  }

  async addExemptFolder(folder: string): Promise<{ exempt_folders: string[] }> {
    return this.request<{ exempt_folders: string[] }>('/api/v1/settings/exempt-folders', {
      method: 'POST',
      body: JSON.stringify({ folder }),
    });
  }

  async removeExemptFolder(folder: string): Promise<{ exempt_folders: string[] }> {
    return this.request<{ exempt_folders: string[] }>(`/api/v1/settings/exempt-folders/${encodeURIComponent(folder)}`, {
      method: 'DELETE',
    });
  }

  // --- Admin (requires system API key) ---
  private adminKey: string = '';

  setAdminKey(key: string) {
    this.adminKey = key;
  }

  private async adminRequest<T>(path: string, options: RequestInit = {}): Promise<T> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...(options.headers as Record<string, string>),
    };

    if (this.adminKey) {
      headers['X-API-Key'] = this.adminKey;
    }

    const url = API_URL ? `${API_URL}${path}` : path;
    const res = await fetch(url, {
      ...options,
      headers,
    });

    if (!res.ok) {
      const body = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(body.error || `API error: ${res.status}`);
    }

    if (res.status === 204) return {} as T;
    return res.json();
  }

  async adminListUsers(): Promise<{ users: AdminUser[] }> {
    return this.adminRequest<{ users: AdminUser[] }>('/admin/users');
  }

  async adminGetUser(userId: number): Promise<AdminUser> {
    return this.adminRequest<AdminUser>(`/admin/users/${userId}`);
  }

  async adminUpdateUserAI(userId: number, aiEnabled: boolean): Promise<{ message: string; user_id: number; ai_enabled: boolean }> {
    return this.adminRequest(`/admin/users/${userId}/ai-enabled`, {
      method: 'PATCH',
      body: JSON.stringify({ ai_enabled: aiEnabled }),
    });
  }
}

// --- WebAuthn helpers ---

function bufferToBase64url(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let str = '';
  for (let i = 0; i < bytes.length; i++) {
    str += String.fromCharCode(bytes[i]);
  }
  return btoa(str).replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '');
}

export function base64urlToBuffer(base64url: string): ArrayBuffer {
  const base64 = base64url.replace(/-/g, '+').replace(/_/g, '/');
  const padLen = (4 - (base64.length % 4)) % 4;
  const padded = base64 + '='.repeat(padLen);
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes.buffer;
}

export const api = new ApiClient();
export default api;
