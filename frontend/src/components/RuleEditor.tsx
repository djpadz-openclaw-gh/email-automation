'use client';

import { useState, useRef, useCallback, useEffect } from 'react';
import dynamic from 'next/dynamic';
import api, { Rule, EmailContext, RuleResult, DryRunResult, ExecuteResult } from '@/lib/api';
import { naturalLanguageToLua, luaToNaturalLanguage, KiroTranslationError } from '@/lib/kiro';
import ReferencePanel from '@/components/ReferencePanel';

const MonacoEditor = dynamic(() => import('@monaco-editor/react'), { ssr: false });

interface RuleEditorProps {
  rule: Rule | null;
  onSave: () => void;
  onCancel: () => void;
}

const DEFAULT_CODE = `-- New rule
-- Access email data via the "email" table
-- Call an action function to set the result

local sender = email.sender_address:lower()
local subject = email.subject:lower()

-- Example: match by sender domain
if not sender:match("example%.com") then
    return skip()
end

return move("@Example", "Matched example.com sender")
`;

const DEFAULT_TEST_EMAIL: EmailContext = {
  message_id: 'test-001',
  subject: 'Your order has shipped',
  sender_name: 'Amazon',
  sender_address: 'ship-confirm@amazon.com',
  recipients: ['user@example.com'],
  date: new Date().toISOString(),
  age_seconds: 90000,
  body_preview: 'Your package is on its way...',
  has_attachments: false,
  attachment_names: [],
  attachment_types: [],
  headers: {},
  folder: 'INBOX',
  account_id: 1,
};

export default function RuleEditor({ rule, onSave, onCancel }: RuleEditorProps) {
  const [name, setName] = useState(rule?.name || '');
  const [description, setDescription] = useState(rule?.description || '');
  const [luaCode, setLuaCode] = useState(rule?.lua_code || DEFAULT_CODE);
  const [priority] = useState(rule?.priority ?? 1000);
  const [active, setActive] = useState(rule?.active ?? true);

  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [validationResult, setValidationResult] = useState<{ valid: boolean; error?: string } | null>(null);
  const [testResult, setTestResult] = useState<RuleResult | null>(null);
  const [testEmail, setTestEmail] = useState(JSON.stringify(DEFAULT_TEST_EMAIL, null, 2));
  const [showReference, setShowReference] = useState(false);
  const [showTest, setShowTest] = useState(false);

  // Dry-run / execute state
  const [dryRunning, setDryRunning] = useState(false);
  const [dryRunResult, setDryRunResult] = useState<DryRunResult | null>(null);
  const [executing, setExecuting] = useState(false);
  const [executeResult, setExecuteResult] = useState<ExecuteResult | null>(null);
  const [scanLimit, setScanLimit] = useState<number>(0); // 0 = all
  const abortControllerRef = useRef<AbortController | null>(null);

  // User-controlled AI toggle (initialized from rule or auto-detected from code)
  const [usesAi, setUsesAi] = useState(() => {
    if (rule) return rule.uses_ai;
    return /\bkiro\.\w+\s*\(/.test(luaCode);
  });

  // Detect kiro.classify usage specifically (for the performance warning)
  const usesAiClassify = /kiro\.classify\s*\(/.test(luaCode);

  // Bidirectional editor state
  const [naturalLanguage, setNaturalLanguage] = useState('');
  const [translating, setTranslating] = useState<'nl2lua' | 'lua2nl' | null>(null);
  const [translationError, setTranslationError] = useState('');
  const [translationAiMessage, setTranslationAiMessage] = useState('');
  const nlDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const luaDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [lastEditSource, setLastEditSource] = useState<'nl' | 'lua' | null>(null);

  // Track whether the user is actively typing in the NL textarea
  const nlFocusedRef = useRef(false);
  const nlLastTypedRef = useRef(0);

  // Translate natural language → Lua (debounced)
  const translateNlToLua = useCallback((text: string, aiEnabled?: boolean) => {
    if (nlDebounceRef.current) clearTimeout(nlDebounceRef.current);
    if (!text.trim()) return;

    nlDebounceRef.current = setTimeout(async () => {
      try {
        setTranslating('nl2lua');
        setTranslationError('');
        setTranslationAiMessage('');
        const code = await naturalLanguageToLua(text, aiEnabled);
        setLuaCode(code);
      } catch (err) {
        if (err instanceof KiroTranslationError) {
          setTranslationError(err.message);
          setTranslationAiMessage(err.aiMessage);
        } else {
          setTranslationError(err instanceof Error ? err.message : 'Translation failed');
        }
      } finally {
        setTranslating(null);
      }
    }, 800);
  }, []);

  // Translate Lua → natural language (debounced)
  const translateLuaToNl = useCallback((code: string, aiEnabled?: boolean) => {
    if (luaDebounceRef.current) clearTimeout(luaDebounceRef.current);
    if (!code.trim()) {
      setNaturalLanguage('');
      return;
    }

    luaDebounceRef.current = setTimeout(async () => {
      try {
        setTranslating('lua2nl');
        setTranslationError('');
        setTranslationAiMessage('');
        const desc = await luaToNaturalLanguage(code, aiEnabled);
        // Only update if the user is NOT actively typing in the NL box
        const recentlyTyped = Date.now() - nlLastTypedRef.current < 500;
        if (!nlFocusedRef.current || !recentlyTyped) {
          setNaturalLanguage(desc);
        }
      } catch (err) {
        setTranslationError(err instanceof Error ? err.message : 'Translation failed');
      } finally {
        setTranslating(null);
      }
    }, 1000);
  }, []);

  // Handle natural language changes
  const handleNlChange = (text: string) => {
    setNaturalLanguage(text);
    setLastEditSource('nl');
    nlLastTypedRef.current = Date.now();
    translateNlToLua(text, usesAi);
  };

  // Handle Lua code changes
  const handleLuaChange = (code: string) => {
    setLuaCode(code);
    setLastEditSource('lua');
    translateLuaToNl(code, usesAi);
  };

  // Handle Uses AI checkbox toggle
  const handleUsesAiChange = (checked: boolean) => {
    setUsesAi(checked);
    // Trigger NL interpretation update with the new AI setting
    translateLuaToNl(luaCode, checked);
  };

  // Generate NL interpretation on initial load
  useEffect(() => {
    translateLuaToNl(luaCode, usesAi);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Cleanup debounce timers
  useEffect(() => {
    return () => {
      if (nlDebounceRef.current) clearTimeout(nlDebounceRef.current);
      if (luaDebounceRef.current) clearTimeout(luaDebounceRef.current);
    };
  }, []);

  const handleValidate = async () => {
    try {
      const result = await api.validateRule(luaCode);
      setValidationResult(result);
    } catch (err: unknown) {
      setValidationResult({ valid: false, error: err instanceof Error ? err.message : 'Validation failed' });
    }
  };

  const handleTest = async () => {
    try {
      setTestResult(null);
      const emailCtx = JSON.parse(testEmail) as EmailContext;
      const result = await api.testRule(luaCode, emailCtx);
      setTestResult(result);
    } catch (err: unknown) {
      setTestResult({ action: 'error', target: '', delay: 0, reason: err instanceof Error ? err.message : 'Test failed' });
    }
  };

  const handleSave = async () => {
    if (!name.trim()) {
      setError('Rule name is required');
      return;
    }

    try {
      setSaving(true);
      setError('');

      const data = {
        name: name.trim(),
        description: description.trim(),
        lua_code: luaCode,
        priority,
        active,
        uses_ai: usesAi,
      };

      if (rule) {
        await api.updateRule(rule.id, data);
      } else {
        await api.createRule(data);
      }

      onSave();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to save rule');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
          {rule ? `Edit: ${rule.name}` : 'New Rule'}
        </h2>
        <div className="flex gap-2">
          <button
            onClick={() => setShowReference(!showReference)}
            className={`px-3 py-1.5 rounded-lg text-sm font-medium transition-colors ${
              showReference
                ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-700'
            }`}
          >
            📖 Reference
          </button>
          <button
            onClick={onCancel}
            className="px-3 py-1.5 bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400 rounded-lg text-sm font-medium hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors"
          >
            Cancel
          </button>
        </div>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-red-700 dark:text-red-400 text-sm" role="alert">
          {error}
        </div>
      )}

      <div className={`grid gap-6 ${showReference ? 'grid-cols-3' : 'grid-cols-1'}`}>
        <div className={showReference ? 'col-span-2' : ''}>
          {/* Rule metadata */}
          <div className="grid grid-cols-2 gap-4 mb-4">
            <div>
              <label htmlFor="rule-name" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Name
              </label>
              <input
                id="rule-name"
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Rule name"
                className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 focus:border-transparent outline-none"
              />
            </div>
            <div>
              <label htmlFor="rule-description" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Description <span className="text-gray-400 font-normal">(optional, for organizing)</span>
              </label>
              <input
                id="rule-description"
                type="text"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="e.g. Move Amazon emails, Delete old newsletters"
                className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 focus:border-transparent outline-none"
              />
            </div>
          </div>

          <div className="flex items-end mb-4">
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  checked={active}
                  onChange={(e) => setActive(e.target.checked)}
                  className="w-4 h-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                />
                <span className="text-sm text-gray-700 dark:text-gray-300">Active</span>
              </label>
              <label className="flex items-center gap-2 ml-6 cursor-pointer" title="Enable AI (kiro.*) function calls in this rule">
                <input
                  type="checkbox"
                  checked={usesAi}
                  onChange={(e) => handleUsesAiChange(e.target.checked)}
                  className="w-4 h-4 rounded border-gray-300 text-purple-600 focus:ring-purple-500"
                />
                <span className={`text-sm ${usesAi ? 'text-purple-700 dark:text-purple-400 font-medium' : 'text-gray-700 dark:text-gray-300'}`}>
                  🤖 Uses AI
                </span>
              </label>
          </div>

          {/* Natural Language Input (bidirectional) */}
          <div className="border border-gray-200 dark:border-gray-800 rounded-xl overflow-hidden mb-4">
            <div className="bg-emerald-50 dark:bg-emerald-900/20 px-4 py-2 flex items-center justify-between border-b border-gray-200 dark:border-gray-700">
              <span className="text-sm font-medium text-emerald-700 dark:text-emerald-400">
                💬 Natural Language
              </span>
              <div className="flex items-center gap-2">
                {translating === 'nl2lua' && (
                  <span className="text-xs text-emerald-600 dark:text-emerald-400 animate-pulse">
                    Translating to Lua...
                  </span>
                )}
                {translating === 'lua2nl' && (
                  <span className="text-xs text-emerald-600 dark:text-emerald-400 animate-pulse">
                    Interpreting...
                  </span>
                )}
                {lastEditSource && (
                  <span className="text-xs text-gray-400">
                    {lastEditSource === 'nl' ? '↓ editing NL' : '↑ synced from Lua'}
                  </span>
                )}
              </div>
            </div>
            <textarea
              value={naturalLanguage}
              onChange={(e) => handleNlChange(e.target.value)}
              onFocus={() => { nlFocusedRef.current = true; }}
              onBlur={() => { nlFocusedRef.current = false; }}
              placeholder={translating === 'lua2nl' && !naturalLanguage ? '💭 Interpreting...' : 'Describe your rule in plain English, e.g.: "Delete emails from noreply@example.com that are older than 24 hours"'}
              rows={3}
              className={`w-full px-4 py-3 bg-white dark:bg-gray-900 text-gray-900 dark:text-white text-sm focus:outline-none resize-none ${translating === 'lua2nl' && !naturalLanguage ? 'placeholder:animate-pulse' : ''}`}
            />
            {translationError && (
              <div className="px-4 py-2 bg-amber-50 dark:bg-amber-900/20 border-t border-gray-200 dark:border-gray-700">
                <p className="text-sm text-amber-600 dark:text-amber-400">
                  ⚠️ {translationError}
                </p>
                {translationAiMessage && (
                  <p className="text-xs text-amber-500 dark:text-amber-500 mt-1">
                    {translationAiMessage}
                  </p>
                )}
              </div>
            )}
          </div>

          {/* Monaco Editor — Lua code (source of truth) */}
          <div className="border border-gray-200 dark:border-gray-800 rounded-xl overflow-hidden mb-4">
            <div className="bg-gray-100 dark:bg-gray-800 px-4 py-2 flex items-center justify-between border-b border-gray-200 dark:border-gray-700">
              <span className="text-sm font-medium text-gray-600 dark:text-gray-400">
                🔧 Lua Rule Code
              </span>
              <div className="flex gap-2 items-center">
                <button
                  onClick={handleValidate}
                  className="px-3 py-1 bg-gray-200 dark:bg-gray-700 text-gray-700 dark:text-gray-300 rounded text-xs font-medium hover:bg-gray-300 dark:hover:bg-gray-600 transition-colors"
                >
                  ✓ Validate
                </button>
                <button
                  onClick={() => setShowTest(!showTest)}
                  className={`px-3 py-1 rounded text-xs font-medium transition-colors ${
                    showTest
                      ? 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400'
                      : 'bg-gray-200 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-300 dark:hover:bg-gray-600'
                  }`}
                >
                  🧪 Test
                </button>
              </div>
            </div>
            <MonacoEditor
              height="400px"
              language="lua"
              theme="vs-dark"
              value={luaCode}
              onChange={(value) => handleLuaChange(value || '')}
              options={{
                minimap: { enabled: false },
                fontSize: 14,
                lineNumbers: 'on',
                scrollBeyondLastLine: false,
                wordWrap: 'on',
                tabSize: 4,
                insertSpaces: true,
                automaticLayout: true,
              }}
            />
          </div>

          {/* Validation result */}
          {validationResult && (
            <div
              className={`mb-4 p-3 rounded-lg text-sm ${
                validationResult.valid
                  ? 'bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 text-green-700 dark:text-green-400'
                  : 'bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-400'
              }`}
              role="status"
            >
              {validationResult.valid ? '✓ Lua syntax is valid' : `✗ ${validationResult.error}`}
            </div>
          )}

          {/* Test panel */}
          {showTest && (
            <div className="mb-4 bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-xl p-4">
              <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">Test Email Context (JSON)</h3>
              <textarea
                value={testEmail}
                onChange={(e) => setTestEmail(e.target.value)}
                rows={8}
                className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-gray-50 dark:bg-gray-800 text-gray-900 dark:text-white text-xs font-mono focus:ring-2 focus:ring-blue-500 focus:border-transparent outline-none"
              />
              <button
                onClick={handleTest}
                className="mt-2 px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 transition-colors"
              >
                Run Test
              </button>

              {testResult && (
                <div className="mt-3 p-3 bg-gray-50 dark:bg-gray-800 rounded-lg" role="status">
                  <div className="text-sm font-medium text-gray-700 dark:text-gray-300">Result:</div>
                  <div className="mt-1 text-sm font-mono">
                    <span className={`font-bold ${
                      testResult.action === 'skip' ? 'text-gray-500' :
                      testResult.action === 'delete' ? 'text-red-600 dark:text-red-400' :
                      testResult.action === 'move' ? 'text-blue-600 dark:text-blue-400' :
                      testResult.action === 'archive' ? 'text-yellow-600 dark:text-yellow-400' :
                      testResult.action === 'keep' ? 'text-green-600 dark:text-green-400' :
                      testResult.action === 'flag' ? 'text-orange-600 dark:text-orange-400' :
                      testResult.action === 'error' ? 'text-red-600 dark:text-red-400' :
                      'text-purple-600 dark:text-purple-400'
                    }`}>
                      {testResult.action}
                    </span>
                    {testResult.target && <span className="text-gray-500"> → {testResult.target}</span>}
                    {testResult.reason && <span className="text-gray-400 ml-2">({testResult.reason})</span>}
                  </div>
                </div>
              )}
            </div>
          )}

          {/* Save / Dry Run / Execute buttons */}
          <div className="flex gap-3 flex-wrap items-center">
            <button
              onClick={handleSave}
              disabled={saving}
              className="px-6 py-2.5 bg-blue-600 text-white rounded-lg font-medium hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {saving ? 'Saving...' : rule ? 'Update Rule' : 'Create Rule'}
            </button>

            {/* Scan limit selector */}
            <select
              value={scanLimit}
              onChange={(e) => setScanLimit(Number(e.target.value))}
              disabled={dryRunning || executing}
              className="px-3 py-2.5 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 focus:border-transparent outline-none disabled:opacity-50"
              aria-label="Scan limit"
            >
              <option value={0}>All messages</option>
              <option value={100}>Last 100</option>
              <option value={500}>Last 500</option>
              <option value={1000}>Last 1,000</option>
              <option value={5000}>Last 5,000</option>
            </select>

            {/* Dry Run / Stop button */}
            {dryRunning ? (
              <button
                onClick={() => {
                  abortControllerRef.current?.abort();
                  abortControllerRef.current = null;
                  if (rule) {
                    api.cancelRuleOperation(rule.id).catch(() => {});
                  } else {
                    api.cancelAdHocOperation().catch(() => {});
                  }
                }}
                className="px-5 py-2.5 bg-red-600 text-white rounded-lg font-medium hover:bg-red-700 transition-colors"
              >
                ⏹ Stop
              </button>
            ) : (
              <button
                onClick={async () => {
                  try {
                    const controller = new AbortController();
                    abortControllerRef.current = controller;
                    setDryRunning(true);
                    setDryRunResult(null);
                    setExecuteResult(null);
                    setError('');
                    let result;
                    if (rule) {
                      result = await api.dryRunRule(rule.id, luaCode, scanLimit || undefined, controller.signal);
                    } else {
                      result = await api.dryRunAdHoc(luaCode, scanLimit || undefined, controller.signal);
                    }
                    setDryRunResult(result);
                  } catch (err: unknown) {
                    if (err instanceof DOMException && err.name === 'AbortError') {
                      // User cancelled — don't show error
                    } else {
                      setError(err instanceof Error ? err.message : 'Dry run failed');
                    }
                  } finally {
                    setDryRunning(false);
                    abortControllerRef.current = null;
                  }
                }}
                disabled={false}
                className="px-5 py-2.5 bg-amber-500 text-white rounded-lg font-medium hover:bg-amber-600 transition-colors"
              >
                🔍 Dry Run
              </button>
            )}

            {/* Execute / Stop button */}
            {executing ? (
              <button
                onClick={() => {
                  abortControllerRef.current?.abort();
                  abortControllerRef.current = null;
                  if (rule) {
                    api.cancelRuleOperation(rule.id).catch(() => {});
                  } else {
                    api.cancelAdHocOperation().catch(() => {});
                  }
                }}
                className="px-5 py-2.5 bg-red-600 text-white rounded-lg font-medium hover:bg-red-700 transition-colors"
              >
                ⏹ Stop
              </button>
            ) : (
              <button
                onClick={async () => {
                  if (!confirm('Execute this rule against INBOX messages? Actions will be performed immediately.')) return;
                  if (!name.trim()) {
                    setError('Rule name is required to execute');
                    return;
                  }
                  try {
                    const controller = new AbortController();
                    abortControllerRef.current = controller;
                    setExecuting(true);
                    setExecuteResult(null);
                    setDryRunResult(null);
                    setError('');

                    let ruleId: number;
                    if (rule) {
                      ruleId = rule.id;
                    } else {
                      // Save the rule first, then execute
                      const created = await api.createRule({
                        name: name.trim(),
                        description: description.trim(),
                        lua_code: luaCode,
                        priority,
                        active,
                        uses_ai: usesAi,
                      });
                      ruleId = created.id;
                    }

                    const result = await api.executeRule(ruleId, luaCode, scanLimit || undefined, controller.signal);
                    setExecuteResult(result);

                    if (!rule) {
                      // Rule was created — notify parent to refresh
                      onSave();
                    }
                  } catch (err: unknown) {
                    if (err instanceof DOMException && err.name === 'AbortError') {
                      // User cancelled
                    } else {
                      setError(err instanceof Error ? err.message : 'Execution failed');
                    }
                  } finally {
                    setExecuting(false);
                    abortControllerRef.current = null;
                  }
                }}
                disabled={dryRunning}
                className="px-5 py-2.5 bg-red-600 text-white rounded-lg font-medium hover:bg-red-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
              >
                ⚡ Execute Now
              </button>
            )}

            <button
              onClick={onCancel}
              className="px-6 py-2.5 bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400 rounded-lg font-medium hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors"
            >
              Cancel
            </button>
          </div>

          {/* AI classification warning */}
          {usesAiClassify && (dryRunning || executing || (!dryRunResult && !executeResult)) && (
            <div className="mt-3 p-3 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg text-amber-700 dark:text-amber-400 text-sm" role="alert">
              ⚠️ This rule uses AI classification. Scanning many messages will take a long time and consume API quota.
            </div>
          )}

          {/* Dry run results */}
          {dryRunResult && (
            <div className="mt-4 bg-white dark:bg-gray-900 border border-amber-200 dark:border-amber-800 rounded-xl p-4">
              <h3 className="text-sm font-medium text-amber-700 dark:text-amber-400 mb-2">
                🔍 Dry Run Results: {dryRunResult.total_matched} of {dryRunResult.total_scanned} messages matched
                {dryRunResult.cancelled && <span className="ml-2 text-amber-500">(stopped early)</span>}
              </h3>
              {dryRunResult.matches.length === 0 ? (
                <p className="text-sm text-gray-500 dark:text-gray-400">No messages matched this rule.</p>
              ) : (
                <div className="max-h-64 overflow-y-auto space-y-2">
                  {dryRunResult.matches.map((m, i) => (
                    <div key={i} className="text-sm p-2 bg-gray-50 dark:bg-gray-800 rounded-lg">
                      <div className="flex items-center gap-2">
                        <span className={`font-medium ${
                          m.action === 'delete' ? 'text-red-600 dark:text-red-400' :
                          m.action === 'move' ? 'text-blue-600 dark:text-blue-400' :
                          m.action === 'archive' ? 'text-yellow-600 dark:text-yellow-400' :
                          m.action === 'flag' ? 'text-orange-600 dark:text-orange-400' :
                          'text-gray-600 dark:text-gray-400'
                        }`}>
                          {m.action}
                        </span>
                        {m.target && <span className="text-gray-400">→ {m.target}</span>}
                      </div>
                      <p className="text-gray-600 dark:text-gray-400 truncate">
                        <span className="text-gray-400">{m.sender_address}:</span> {m.subject}
                      </p>
                      {m.reason && <p className="text-xs text-gray-400">{m.reason}</p>}
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* Execute results */}
          {executeResult && (
            <div className="mt-4 bg-white dark:bg-gray-900 border border-green-200 dark:border-green-800 rounded-xl p-4">
              <h3 className="text-sm font-medium text-green-700 dark:text-green-400 mb-2">
                ⚡ Execution Results: {executeResult.total_executed} executed, {executeResult.total_failed} failed, {executeResult.total_scanned} scanned
                {executeResult.cancelled && <span className="ml-2 text-amber-500">(stopped early)</span>}
              </h3>
              {executeResult.errors && executeResult.errors.length > 0 && (
                <div className="mb-2 p-2 bg-red-50 dark:bg-red-900/20 rounded-lg">
                  {executeResult.errors.map((err, i) => (
                    <p key={i} className="text-xs text-red-600 dark:text-red-400">{err}</p>
                  ))}
                </div>
              )}
              {executeResult.results.length > 0 && (
                <div className="max-h-64 overflow-y-auto space-y-2">
                  {executeResult.results.map((m, i) => (
                    <div key={i} className="text-sm p-2 bg-gray-50 dark:bg-gray-800 rounded-lg">
                      <div className="flex items-center gap-2">
                        <span className={`font-medium ${
                          m.action === 'delete' ? 'text-red-600 dark:text-red-400' :
                          m.action === 'move' ? 'text-blue-600 dark:text-blue-400' :
                          m.action === 'archive' ? 'text-yellow-600 dark:text-yellow-400' :
                          m.action === 'flag' ? 'text-orange-600 dark:text-orange-400' :
                          'text-gray-600 dark:text-gray-400'
                        }`}>
                          {m.action}
                        </span>
                        {m.target && <span className="text-gray-400">→ {m.target}</span>}
                      </div>
                      <p className="text-gray-600 dark:text-gray-400 truncate">
                        <span className="text-gray-400">{m.sender_address}:</span> {m.subject}
                      </p>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>

        {/* Reference panel */}
        {showReference && (
          <div className="col-span-1">
            <ReferencePanel />
          </div>
        )}
      </div>
    </div>
  );
}
