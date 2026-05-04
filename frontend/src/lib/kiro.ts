// Translation endpoints on the Go backend API server.
// The ingress routes /api/* to the Go backend, so these hit the dedicated
// translation handlers (not the generic Anthropic proxy).
const TRANSLATE_BASE =
  process.env.NEXT_PUBLIC_TRANSLATE_API_URL || '/api/kiro/translate';

// Custom error class for non-Lua AI responses
export class KiroTranslationError extends Error {
  public aiMessage: string;
  constructor(error: string, aiMessage: string) {
    super(error);
    this.name = 'KiroTranslationError';
    this.aiMessage = aiMessage;
  }
}

// Translation cache to minimize API calls
const translationCache = new Map<string, { result: string; timestamp: number }>();
const CACHE_TTL_MS = 5 * 60 * 1000; // 5 minutes

function getCacheKey(direction: 'nl2lua' | 'lua2nl', input: string): string {
  return `${direction}:${input}`;
}

function getCached(key: string): string | null {
  const entry = translationCache.get(key);
  if (entry && Date.now() - entry.timestamp < CACHE_TTL_MS) {
    return entry.result;
  }
  if (entry) {
    translationCache.delete(key);
  }
  return null;
}

function setCache(key: string, result: string): void {
  // Limit cache size
  if (translationCache.size > 200) {
    const oldest = translationCache.keys().next().value;
    if (oldest) translationCache.delete(oldest);
  }
  translationCache.set(key, { result, timestamp: Date.now() });
}

/**
 * Translate natural language description to Lua rule code.
 * Uses the dedicated /api/kiro/translate/english-to-lua endpoint on the Go backend,
 * which has proper system prompts and response validation built in.
 */
export async function naturalLanguageToLua(description: string): Promise<string> {
  const cacheKey = getCacheKey('nl2lua', description);
  const cached = getCached(cacheKey);
  if (cached) return cached;

  const res = await fetch(`${TRANSLATE_BASE}/english-to-lua`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ description }),
  });

  if (!res.ok) {
    const body = await res.text().catch(() => res.statusText);
    throw new Error(`Translation API error ${res.status}: ${body}`);
  }

  const data = await res.json();

  // The backend returns { error, message } when the AI response wasn't valid Lua
  if (data.error) {
    throw new KiroTranslationError(data.error, data.message || '');
  }

  const code = (data.lua_code || '').trim();
  if (!code) {
    throw new Error('Empty response from translation API');
  }

  setCache(cacheKey, code);
  return code;
}

/**
 * Translate Lua rule code to natural language description.
 * Uses the dedicated /api/kiro/translate/lua-to-english endpoint on the Go backend,
 * which returns a plain English description without any looksLikeLua validation.
 */
export async function luaToNaturalLanguage(luaCode: string): Promise<string> {
  const cacheKey = getCacheKey('lua2nl', luaCode);
  const cached = getCached(cacheKey);
  if (cached) return cached;

  const res = await fetch(`${TRANSLATE_BASE}/lua-to-english`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ lua_code: luaCode }),
  });

  if (!res.ok) {
    const body = await res.text().catch(() => res.statusText);
    throw new Error(`Translation API error ${res.status}: ${body}`);
  }

  const data = await res.json();

  if (data.error) {
    throw new Error(data.error);
  }

  const description = (data.description || '').trim();
  if (!description) {
    throw new Error('Empty response from translation API');
  }

  setCache(cacheKey, description);
  return description;
}
