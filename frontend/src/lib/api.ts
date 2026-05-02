const API_URL = process.env.NEXT_PUBLIC_API_URL || process.env.API_URL || 'http://localhost:8080';

export interface Rule {
  id: number;
  tenant_id: number;
  name: string;
  description: string;
  lua_code: string;
  priority: number;
  active: boolean;
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

class ApiClient {
  private apiKey: string;

  constructor(apiKey: string = '') {
    this.apiKey = apiKey;
  }

  setApiKey(key: string) {
    this.apiKey = key;
  }

  private async request<T>(path: string, options: RequestInit = {}): Promise<T> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...(options.headers as Record<string, string>),
    };

    if (this.apiKey) {
      headers['X-API-Key'] = this.apiKey;
    }

    const res = await fetch(`${API_URL}${path}`, {
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

  // Rules
  async listRules(): Promise<Rule[]> {
    return this.request<Rule[]>('/api/v1/rules');
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

  // Accounts
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

  // Logs
  async listLogs(limit: number = 50): Promise<ExecutionLog[]> {
    return this.request<ExecutionLog[]>(`/api/v1/logs?limit=${limit}`);
  }
}

export const api = new ApiClient();
export default api;
