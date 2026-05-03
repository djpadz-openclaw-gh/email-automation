'use client';

import { useState, useRef, useCallback, useEffect } from 'react';
import dynamic from 'next/dynamic';
import api, { Rule, EmailContext, RuleResult } from '@/lib/api';
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
if not sender:find("example.com", 1, true) then
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
  const [priority, setPriority] = useState(rule?.priority ?? 100);
  const [active, setActive] = useState(rule?.active ?? true);

  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [validationResult, setValidationResult] = useState<{ valid: boolean; error?: string } | null>(null);
  const [testResult, setTestResult] = useState<RuleResult | null>(null);
  const [testEmail, setTestEmail] = useState(JSON.stringify(DEFAULT_TEST_EMAIL, null, 2));
  const [showReference, setShowReference] = useState(false);
  const [showTest, setShowTest] = useState(false);

  // Bidirectional editor state
  const [naturalLanguage, setNaturalLanguage] = useState('');
  const [translating, setTranslating] = useState<'nl2lua' | 'lua2nl' | null>(null);
  const [translationError, setTranslationError] = useState('');
  const nlDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const luaDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [lastEditSource, setLastEditSource] = useState<'nl' | 'lua' | null>(null);

  // Error banner state for non-Lua AI responses
  const [errorBanner, setErrorBanner] = useState<{ error: string; message: string } | null>(null);

  // Translate natural language → Lua (debounced)
  const translateNlToLua = useCallback((text: string) => {
    if (nlDebounceRef.current) clearTimeout(nlDebounceRef.current);
    if (!text.trim()) return;

    nlDebounceRef.current = setTimeout(async () => {
      try {
        setTranslating('nl2lua');
        setTranslationError('');
        setErrorBanner(null);
        const code = await naturalLanguageToLua(text);
        setLuaCode(code);
      } catch (err) {
        if (err instanceof KiroTranslationError) {
          // Show error banner, don't update the Lua code
          setErrorBanner({ error: err.message, message: err.aiMessage });
        } else {
          setTranslationError(err instanceof Error ? err.message : 'Translation failed');
        }
      } finally {
        setTranslating(null);
      }
    }, 500);
  }, []);

  // Translate Lua → natural language (debounced)
  const translateLuaToNl = useCallback((code: string) => {
    if (luaDebounceRef.current) clearTimeout(luaDebounceRef.current);
    if (!code.trim()) return;

    luaDebounceRef.current = setTimeout(async () => {
      try {
        setTranslating('lua2nl');
        setTranslationError('');
        const desc = await luaToNaturalLanguage(code);
        setNaturalLanguage(desc);
      } catch (err) {
        setTranslationError(err instanceof Error ? err.message : 'Translation failed');
      } finally {
        setTranslating(null);
      }
    }, 500);
  }, []);

  // Handle natural language changes
  const handleNlChange = (text: string) => {
    setNaturalLanguage(text);
    setLastEditSource('nl');
    translateNlToLua(text);
  };

  // Handle Lua code changes
  const handleLuaChange = (code: string) => {
    setLuaCode(code);
    setLastEditSource('lua');
    translateLuaToNl(code);
  };

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
                Description
              </label>
              <input
                id="rule-description"
                type="text"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="What does this rule do?"
                className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 focus:border-transparent outline-none"
              />
            </div>
          </div>

          <div className="grid grid-cols-3 gap-4 mb-4">
            <div>
              <label htmlFor="rule-priority" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Priority (lower = first)
              </label>
              <input
                id="rule-priority"
                type="number"
                value={priority}
                onChange={(e) => setPriority(parseInt(e.target.value) || 100)}
                className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 focus:border-transparent outline-none"
              />
            </div>
            <div className="flex items-end">
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  checked={active}
                  onChange={(e) => setActive(e.target.checked)}
                  className="w-4 h-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                />
                <span className="text-sm text-gray-700 dark:text-gray-300">Active</span>
              </label>
            </div>
          </div>

          {/* Natural Language Input */}
          <div className="border border-gray-200 dark:border-gray-800 rounded-xl overflow-hidden mb-4">
            <div className="bg-emerald-50 dark:bg-emerald-900/20 px-4 py-2 flex items-center justify-between border-b border-gray-200 dark:border-gray-700">
              <span className="text-sm font-medium text-emerald-700 dark:text-emerald-400">
                💬 Natural Language
              </span>
              {translating === 'nl2lua' && (
                <span className="text-xs text-emerald-600 dark:text-emerald-400 animate-pulse">
                  Translating to Lua...
                </span>
              )}
            </div>
            <textarea
              value={naturalLanguage}
              onChange={(e) => handleNlChange(e.target.value)}
              placeholder="Describe your rule in plain English, e.g.: &quot;Delete emails from noreply@example.com that are older than 24 hours and contain 'verification code' in the subject&quot;"
              rows={3}
              className="w-full px-4 py-3 bg-white dark:bg-gray-900 text-gray-900 dark:text-white text-sm focus:outline-none resize-none"
            />
          </div>

          {/* Translation sync indicator */}
          <div className="flex items-center justify-center mb-4">
            <div className="flex items-center gap-2 text-xs text-gray-400">
              <span>💬 Natural Language</span>
              <svg className={`w-4 h-4 ${translating ? 'animate-spin text-blue-500' : 'text-gray-300'}`} fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M7 16V4m0 0L3 8m4-4l4 4m6 0v12m0 0l4-4m-4 4l-4-4" />
              </svg>
              <span>🔧 Lua Code</span>
              {lastEditSource && (
                <span className="ml-2 text-gray-300">
                  (last edited: {lastEditSource === 'nl' ? 'natural language' : 'Lua'})
                </span>
              )}
            </div>
          </div>

          {/* Translation error */}
          {translationError && (
            <div className="mb-4 p-3 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg text-amber-700 dark:text-amber-400 text-sm" role="alert">
              ⚠️ Translation: {translationError}
            </div>
          )}

          {/* Error banner for non-Lua AI responses */}
          {errorBanner && (
            <div className="mb-4 p-4 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg" role="alert">
              <div className="flex items-start justify-between">
                <div>
                  <div className="text-sm font-medium text-red-700 dark:text-red-400">
                    ⚠️ {errorBanner.error}
                  </div>
                  {errorBanner.message && (
                    <div className="mt-2 text-sm text-red-600 dark:text-red-300 whitespace-pre-wrap">
                      {errorBanner.message}
                    </div>
                  )}
                </div>
                <button
                  onClick={() => setErrorBanner(null)}
                  className="ml-3 text-red-400 hover:text-red-600 dark:hover:text-red-300"
                  aria-label="Dismiss error"
                >
                  ✕
                </button>
              </div>
            </div>
          )}

          {/* Monaco Editor */}
          <div className="border border-gray-200 dark:border-gray-800 rounded-xl overflow-hidden mb-4">
            <div className="bg-gray-100 dark:bg-gray-800 px-4 py-2 flex items-center justify-between border-b border-gray-200 dark:border-gray-700">
              <span className="text-sm font-medium text-gray-600 dark:text-gray-400">
                🔧 Lua Rule Code
              </span>
              <div className="flex gap-2 items-center">
                {translating === 'lua2nl' && (
                  <span className="text-xs text-blue-600 dark:text-blue-400 animate-pulse">
                    Translating to English...
                  </span>
                )}
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

          {/* Save button */}
          <div className="flex gap-3">
            <button
              onClick={handleSave}
              disabled={saving}
              className="px-6 py-2.5 bg-blue-600 text-white rounded-lg font-medium hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {saving ? 'Saving...' : rule ? 'Update Rule' : 'Create Rule'}
            </button>
            <button
              onClick={onCancel}
              className="px-6 py-2.5 bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400 rounded-lg font-medium hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors"
            >
              Cancel
            </button>
          </div>
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
