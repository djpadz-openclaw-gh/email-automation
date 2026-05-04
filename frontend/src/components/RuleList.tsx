'use client';

import { useState, useEffect, useRef, useCallback } from 'react';
import api, { Rule } from '@/lib/api';

interface RuleListProps {
  onEdit: (rule: Rule) => void;
  onCreate: () => void;
}

export default function RuleList({ onEdit, onCreate }: RuleListProps) {
  const [rules, setRules] = useState<Rule[]>([]);
  const [suggestedRules, setSuggestedRules] = useState<Rule[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [reordering, setReordering] = useState(false);

  // Drag state
  const [dragIndex, setDragIndex] = useState<number | null>(null);
  const [insertIndex, setInsertIndex] = useState<number | null>(null);
  const dragCounter = useRef(0);

  const loadRules = async () => {
    try {
      setLoading(true);
      setError('');
      const [data, suggested] = await Promise.all([
        api.listRules(),
        api.listSuggestedRules().catch(() => [] as Rule[]),
      ]);
      setRules(data);
      setSuggestedRules(suggested);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to load rules');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadRules();
  }, []);

  const handleToggle = async (rule: Rule) => {
    try {
      await api.updateRule(rule.id, { ...rule, active: !rule.active });
      await loadRules();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to update rule');
    }
  };

  const handleDelete = async (rule: Rule) => {
    if (!confirm(`Delete rule "${rule.name}"?`)) return;
    try {
      await api.deleteRule(rule.id);
      await loadRules();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to delete rule');
    }
  };

  const handleApprove = async (rule: Rule) => {
    try {
      await api.approveRule(rule.id);
      await loadRules();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to approve rule');
    }
  };

  const handleDismiss = async (rule: Rule) => {
    try {
      await api.deleteRule(rule.id);
      await loadRules();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to dismiss rule');
    }
  };

  // --- Drag and drop ---
  const handleDragStart = useCallback((e: React.DragEvent, index: number) => {
    setDragIndex(index);
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', String(index));
    // Make the drag image slightly transparent
    if (e.currentTarget instanceof HTMLElement) {
      e.currentTarget.style.opacity = '0.5';
    }
  }, []);

  const handleDragEnd = useCallback((e: React.DragEvent) => {
    if (e.currentTarget instanceof HTMLElement) {
      e.currentTarget.style.opacity = '1';
    }
    setDragIndex(null);
    setInsertIndex(null);
    dragCounter.current = 0;
  }, []);

  const handleDragOver = useCallback((e: React.DragEvent, index: number) => {
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    // Determine if cursor is in the top or bottom half of the card
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
    const midY = rect.top + rect.height / 2;
    const slot = e.clientY < midY ? index : index + 1;
    setInsertIndex(slot);
  }, []);

  const handleDragLeaveList = useCallback(() => {
    dragCounter.current--;
    if (dragCounter.current <= 0) {
      setInsertIndex(null);
      dragCounter.current = 0;
    }
  }, []);

  const handleDragEnterList = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    dragCounter.current++;
  }, []);

  const handleDrop = useCallback(async (e: React.DragEvent) => {
    e.preventDefault();
    dragCounter.current = 0;

    if (dragIndex === null || insertIndex === null) {
      setDragIndex(null);
      setInsertIndex(null);
      return;
    }

    // Calculate the effective target index after removal
    let targetIndex = insertIndex;
    if (targetIndex > dragIndex) {
      targetIndex--; // account for the removed item shifting indices down
    }

    if (targetIndex === dragIndex) {
      setDragIndex(null);
      setInsertIndex(null);
      return;
    }

    // Reorder locally first for instant feedback
    const newRules = [...rules];
    const [moved] = newRules.splice(dragIndex, 1);
    newRules.splice(targetIndex, 0, moved);
    setRules(newRules);
    setDragIndex(null);
    setInsertIndex(null);

    // Persist to backend
    try {
      setReordering(true);
      await api.reorderRules(newRules.map(r => r.id));
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to reorder rules');
      // Reload to get the actual order
      await loadRules();
    } finally {
      setReordering(false);
    }
  }, [dragIndex, insertIndex, rules]);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="text-gray-500 dark:text-gray-400">Loading rules...</div>
      </div>
    );
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <div className="flex items-center gap-3">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
            Rules ({rules.length})
          </h2>
          {reordering && (
            <span className="text-xs text-blue-500 animate-pulse">Saving order...</span>
          )}
        </div>
        <button
          onClick={onCreate}
          className="px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 transition-colors"
        >
          + New Rule
        </button>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-red-700 dark:text-red-400 text-sm" role="alert">
          {error}
        </div>
      )}

      {/* Suggested rules banner */}
      {suggestedRules.length > 0 && (
        <div className="mb-6">
          <h3 className="text-sm font-medium text-amber-700 dark:text-amber-400 mb-3 flex items-center gap-2">
            <span>💡</span>
            Suggested Rules ({suggestedRules.length})
          </h3>
          <div className="space-y-3">
            {suggestedRules.map((rule) => (
              <div
                key={rule.id}
                className="bg-amber-50 dark:bg-amber-900/10 rounded-xl border border-amber-200 dark:border-amber-800/50 p-4"
              >
                <div className="flex items-start justify-between">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <h3 className="font-medium text-gray-900 dark:text-white truncate">
                        {rule.name}
                      </h3>
                      <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-400">
                        Auto-learned
                      </span>
                    </div>
                    {rule.description && (
                      <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
                        {rule.description}
                      </p>
                    )}
                    <pre className="mt-2 text-xs text-gray-600 dark:text-gray-400 bg-white dark:bg-gray-900 rounded-lg p-2 overflow-x-auto border border-gray-200 dark:border-gray-700">
                      {rule.lua_code}
                    </pre>
                  </div>
                  <div className="flex items-center gap-2 ml-4 shrink-0">
                    <button
                      onClick={() => onEdit(rule)}
                      className="px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 transition-colors"
                      title="Edit before approving"
                    >
                      ✏️ Edit
                    </button>
                    <button
                      onClick={() => handleApprove(rule)}
                      className="px-3 py-1.5 bg-green-600 text-white rounded-lg text-sm font-medium hover:bg-green-700 transition-colors"
                    >
                      ✓ Approve
                    </button>
                    <button
                      onClick={() => handleDismiss(rule)}
                      className="px-3 py-1.5 bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400 rounded-lg text-sm font-medium hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors"
                    >
                      ✗ Dismiss
                    </button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {rules.length === 0 && suggestedRules.length === 0 ? (
        <div className="text-center py-12 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800">
          <span className="text-4xl block mb-3" role="img" aria-label="empty">📭</span>
          <p className="text-gray-500 dark:text-gray-400">No rules yet</p>
          <button
            onClick={onCreate}
            className="mt-4 px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 transition-colors"
          >
            Create your first rule
          </button>
        </div>
      ) : (
        <div
          className="space-y-1"
          onDrop={handleDrop}
          onDragEnter={handleDragEnterList}
          onDragLeave={handleDragLeaveList}
        >
          {rules.length > 1 && (
            <p className="text-xs text-gray-400 dark:text-gray-500 mb-2">
              Drag rules to reorder. Rules are evaluated top to bottom — first match wins.
            </p>
          )}
          {rules.map((rule, index) => {
            // Show insertion line if this slot is active and it's not a no-op position
            const showInsertBefore = dragIndex !== null && insertIndex === index
              && insertIndex !== dragIndex && insertIndex !== dragIndex + 1;
            const showInsertAfter = dragIndex !== null && index === rules.length - 1
              && insertIndex === rules.length
              && insertIndex !== dragIndex && insertIndex !== dragIndex + 1;

            return (
            <div key={rule.id}>
              {/* Insertion indicator before this card */}
              <div
                className={`transition-all duration-150 ${
                  showInsertBefore
                    ? 'h-1 my-1 mx-2 rounded-full bg-blue-500 dark:bg-blue-400'
                    : 'h-0'
                }`}
                aria-hidden="true"
              />
              <div
                draggable
                onDragStart={(e) => handleDragStart(e, index)}
                onDragEnd={handleDragEnd}
                onDragOver={(e) => handleDragOver(e, index)}
                className={`bg-white dark:bg-gray-900 rounded-xl border p-4 transition-all ${
                  dragIndex === index
                    ? 'opacity-50 border-blue-400 dark:border-blue-600'
                    : 'border-gray-200 dark:border-gray-800 hover:border-gray-300 dark:hover:border-gray-700'
                }`}
              >
              <div className="flex items-start">
                {/* Drag handle */}
                <div
                  className="flex items-center justify-center w-6 h-6 mr-3 mt-0.5 cursor-grab active:cursor-grabbing text-gray-300 dark:text-gray-600 hover:text-gray-500 dark:hover:text-gray-400 select-none shrink-0"
                  title="Drag to reorder"
                  aria-label="Drag to reorder"
                >
                  <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
                    <circle cx="5" cy="3" r="1.5" />
                    <circle cx="11" cy="3" r="1.5" />
                    <circle cx="5" cy="8" r="1.5" />
                    <circle cx="11" cy="8" r="1.5" />
                    <circle cx="5" cy="13" r="1.5" />
                    <circle cx="11" cy="13" r="1.5" />
                  </svg>
                </div>

                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-gray-300 dark:text-gray-600 font-mono w-5 shrink-0">
                      {index + 1}
                    </span>
                    <h3 className="font-medium text-gray-900 dark:text-white truncate">
                      {rule.name}
                    </h3>
                    <span
                      className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${
                        rule.active
                          ? 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400'
                          : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400'
                      }`}
                    >
                      {rule.active ? 'Active' : 'Disabled'}
                    </span>
                    {rule.source === 'auto-learned' && (
                      <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-purple-100 text-purple-800 dark:bg-purple-900/30 dark:text-purple-400">
                        Auto-learned
                      </span>
                    )}
                    {rule.uses_ai && (
                      <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-indigo-100 text-indigo-800 dark:bg-indigo-900/30 dark:text-indigo-400" title="This rule uses AI (kiro.*) calls">
                        🤖 AI
                      </span>
                    )}
                  </div>
                  {rule.description && (
                    <p className="text-sm text-gray-500 dark:text-gray-400 mt-1 truncate ml-5">
                      {rule.description}
                    </p>
                  )}
                </div>

                <div className="flex items-center gap-1 ml-4 shrink-0">
                  <button
                    onClick={() => handleToggle(rule)}
                    className="p-2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors"
                    title={rule.active ? 'Disable' : 'Enable'}
                    aria-label={rule.active ? 'Disable rule' : 'Enable rule'}
                  >
                    {rule.active ? '⏸' : '▶️'}
                  </button>
                  <button
                    onClick={() => onEdit(rule)}
                    className="p-2 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 transition-colors"
                    title="Edit"
                    aria-label="Edit rule"
                  >
                    ✏️
                  </button>
                  <button
                    onClick={() => handleDelete(rule)}
                    className="p-2 text-gray-400 hover:text-red-600 dark:hover:text-red-400 transition-colors"
                    title="Delete"
                    aria-label="Delete rule"
                  >
                    🗑
                  </button>
                </div>
              </div>
              </div>
              {/* Insertion indicator after the last card */}
              <div
                className={`transition-all duration-150 ${
                  showInsertAfter
                    ? 'h-1 my-1 mx-2 rounded-full bg-blue-500 dark:bg-blue-400'
                    : 'h-0'
                }`}
                aria-hidden="true"
              />
            </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
