import * as https from 'https';
import * as http from 'http';

interface ApiResponse<T> {
    ok: boolean;
    data?: T;
    error?: string;
}

interface ValidationResult {
    valid: boolean;
    error?: string;
    line?: number;
    column?: number;
}

interface TestResult {
    action: string;
    target: string;
    delay: number;
    reason: string;
    error?: string;
}

interface ServerRule {
    id: number;
    name: string;
    description: string;
    lua_code: string;
    priority: number;
    active: boolean;
    created_at: string;
    updated_at: string;
}

export class ApiClient {
    private url: string;
    private apiKey: string;

    constructor(url: string, apiKey: string) {
        this.url = url.replace(/\/$/, '');
        this.apiKey = apiKey;
    }

    setUrl(url: string): void {
        this.url = url.replace(/\/$/, '');
    }

    setApiKey(key: string): void {
        this.apiKey = key;
    }

    async validate(code: string): Promise<ApiResponse<ValidationResult>> {
        return this.post<ValidationResult>('/api/v1/rules/validate', { lua_code: code });
    }

    async testRule(code: string, emailContext: Record<string, unknown>): Promise<ApiResponse<TestResult>> {
        return this.post<TestResult>('/api/v1/rules/test', {
            lua_code: code,
            email: emailContext,
        });
    }

    async listRules(): Promise<ApiResponse<ServerRule[]>> {
        return this.get<ServerRule[]>('/api/v1/rules');
    }

    async getRule(id: number): Promise<ApiResponse<ServerRule>> {
        return this.get<ServerRule>(`/api/v1/rules/${id}`);
    }

    async createRule(rule: { name: string; description: string; lua_code: string; priority: number; active: boolean }): Promise<ApiResponse<ServerRule>> {
        return this.post<ServerRule>('/api/v1/rules', rule);
    }

    async updateRule(id: number, rule: { name: string; description: string; lua_code: string; priority: number; active: boolean }): Promise<ApiResponse<ServerRule>> {
        return this.put<ServerRule>(`/api/v1/rules/${id}`, rule);
    }

    private async get<T>(path: string): Promise<ApiResponse<T>> {
        return this.request<T>('GET', path);
    }

    private async post<T>(path: string, body: unknown): Promise<ApiResponse<T>> {
        return this.request<T>('POST', path, body);
    }

    private async put<T>(path: string, body: unknown): Promise<ApiResponse<T>> {
        return this.request<T>('PUT', path, body);
    }

    private request<T>(method: string, path: string, body?: unknown): Promise<ApiResponse<T>> {
        return new Promise((resolve) => {
            const url = new URL(this.url + path);
            const isHttps = url.protocol === 'https:';
            const lib = isHttps ? https : http;

            const headers: Record<string, string> = {
                'Content-Type': 'application/json',
                'Accept': 'application/json',
            };
            if (this.apiKey) {
                headers['X-API-Key'] = this.apiKey;
            }

            const payload = body ? JSON.stringify(body) : undefined;
            if (payload) {
                headers['Content-Length'] = Buffer.byteLength(payload).toString();
            }

            const req = lib.request({
                hostname: url.hostname,
                port: url.port || (isHttps ? 443 : 80),
                path: url.pathname + url.search,
                method,
                headers,
                timeout: 10000,
            }, (res) => {
                let data = '';
                res.on('data', (chunk: Buffer) => { data += chunk.toString(); });
                res.on('end', () => {
                    try {
                        const parsed = JSON.parse(data);
                        if (res.statusCode && res.statusCode >= 200 && res.statusCode < 300) {
                            resolve({ ok: true, data: parsed as T });
                        } else {
                            resolve({ ok: false, error: parsed.error || parsed.message || `HTTP ${res.statusCode}` });
                        }
                    } catch {
                        resolve({ ok: false, error: `Invalid response: ${data.substring(0, 200)}` });
                    }
                });
            });

            req.on('error', (err: Error) => {
                resolve({ ok: false, error: `Connection failed: ${err.message}` });
            });

            req.on('timeout', () => {
                req.destroy();
                resolve({ ok: false, error: 'Request timed out' });
            });

            if (payload) {
                req.write(payload);
            }
            req.end();
        });
    }
}
