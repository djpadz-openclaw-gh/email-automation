// Use the Next.js API route proxy to avoid exposing the Kiro API key client-side
const KIRO_API_URL =
  process.env.NEXT_PUBLIC_KIRO_API_URL || '/api/kiro';

interface KiroMessage {
  role: 'user' | 'assistant';
  content: string;
}

interface KiroResponse {
  content?: Array<{ type: string; text: string }>;
  error?: string;
  message?: string;
}

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

async function callKiro(messages: KiroMessage[]): Promise<string> {
  const res = await fetch(KIRO_API_URL, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      model: 'claude-haiku-4.5',
      max_tokens: 2048,
      messages,
    }),
  });

  if (!res.ok) {
    const body = await res.text().catch(() => res.statusText);
    throw new Error(`Kiro API error ${res.status}: ${body}`);
  }

  const data: KiroResponse = await res.json();

  // Check if the response contains an error field (non-Lua response)
  if (data.error) {
    throw new KiroTranslationError(data.error, data.message || '');
  }

  if (data.content && data.content.length > 0) {
    return data.content[0].text;
  }
  throw new Error('Empty response from Kiro API');
}

const SYSTEM_CONTEXT = `You are an expert at the email automation Lua rule DSL. Rules have access to:

Email context (via "email" table):
- email.subject, email.sender_address, email.sender_name, email.sender (alias)
- email.recipients (table), email.date, email.age_seconds
- email.body_preview, email.has_attachments, email.attachment_names, email.attachment_types
- email.headers (table), email.folder, email.message_id

Action functions (call one to set result):
- skip() - rule doesn't apply
- delete(reason?) - delete message
- archive(reason?) - archive message
- move(folder, reason?) - move to folder
- keep(reason?) - keep in inbox, stop chain
- notify(message, reason?) - send notification
- defer_action(action, target, delay_secs, reason?)
- move_after(folder, delay_secs, reason?)
- delete_after(delay_secs, reason?)

Helper functions:
- contains(haystack, needle) - case-insensitive
- contains_any(haystack, {needles}) - case-insensitive
- starts_with(text, prefix), ends_with(text, suffix)
- domain_of(email_addr), older_than(secs), older_than_hours(h), older_than_days(d)
- has_ics(), has_attachment_type(mime), is_reply(), now_hour()

Kiro AI functions (for SEMANTIC evaluation only):
- kiro.classify(email, question) - ask AI a yes/no question about the email
- kiro.is_actionable(email) - ask AI if the email requires action from the recipient

Guidelines:
- PREFER simple string matching (contains, domain_of, etc.) for concrete criteria
- Use kiro.classify() ONLY for inherently semantic/subjective questions
- Use kiro.is_actionable() for action/triage questions

Lua standard library: string, table, math (safe subset only).`;

/**
 * Translate natural language description to Lua rule code.
 */
export async function naturalLanguageToLua(description: string): Promise<string> {
  const cacheKey = getCacheKey('nl2lua', description);
  const cached = getCached(cacheKey);
  if (cached) return cached;

  const result = await callKiro([
    {
      role: 'user',
      content: `${SYSTEM_CONTEXT}

Convert this natural language rule description into Lua code for the email automation engine. Return ONLY the Lua code, no markdown fences, no explanation.

Description: ${description}`,
    },
  ]);

  // Strip markdown code fences if present
  let code = result.trim();
  if (code.startsWith('```lua')) {
    code = code.slice(6);
  } else if (code.startsWith('```')) {
    code = code.slice(3);
  }
  if (code.endsWith('```')) {
    code = code.slice(0, -3);
  }
  code = code.trim();

  setCache(cacheKey, code);
  return code;
}

/**
 * Translate Lua rule code to natural language description.
 */
export async function luaToNaturalLanguage(luaCode: string): Promise<string> {
  const cacheKey = getCacheKey('lua2nl', luaCode);
  const cached = getCached(cacheKey);
  if (cached) return cached;

  const result = await callKiro([
    {
      role: 'user',
      content: `${SYSTEM_CONTEXT}

Describe this Lua email automation rule in plain English. Be concise but complete. Describe what emails it matches and what action it takes. Return ONLY the description, no code.

Lua code:
${luaCode}`,
    },
  ]);

  const description = result.trim();
  setCache(cacheKey, description);
  return description;
}
