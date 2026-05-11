'use client';

import { useState, useEffect } from 'react';
import api, { DeferredAction } from '@/lib/api';

export default function DeferredActions() {
  const [actions, setActions] = useState<DeferredAction[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const loadActions = async () => {
    try {
      setLoading(true);
      setError('');
      const data = await api.listDeferredActions();
      setActions(data.deferred_actions);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to load deferred actions');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadActions();
    // Auto-refresh every 30 seconds
    const interval = setInterval(loadActions, 30000);
    return () => clearInterval(interval);
  }, []);

  const handleCancel = async (action: DeferredAction) => {
    if (!confirm(`Cancel deferred "${action.action}" for message ${action.message_id}?`)) return;
    try {
      await api.cancelDeferredAction(action.id);
      await loadActions();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to cancel action');
    }
  };

  const formatTimeUntil = (dateStr: string) => {
    const target = new Date(dateStr);
    const now = new Date();
    const diffMs = target.getTime() - now.getTime();

    if (diffMs <= 0) return 'executing soon';

    const diffMin = Math.floor(diffMs / 60000);
    const diffHr = Math.floor(diffMin / 60);
    const diffDay = Math.floor(diffHr / 24);

    if (diffDay > 0) return `in ${diffDay}d ${diffHr % 24}h`;
    if (diffHr > 0) return `in ${diffHr}h ${diffMin % 60}m`;
    if (diffMin > 0) return `in ${diffMin}m`;
    return `in ${Math.floor(diffMs / 1000)}s`;
  };

  const actionEmoji = (action: string) => {
    switch (action) {
      case 'delete': return '🗑';
      case 'move': return '📁';
      case 'archive': return '📦';
      case 'flag': return '🏷';
      default: return '⏳';
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="text-gray-500 dark:text-gray-400">Loading pending operations...</div>
      </div>
    );
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
          Pending Operations ({actions.length})
        </h2>
        <button
          onClick={loadActions}
          className="px-3 py-1.5 bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400 rounded-lg text-sm font-medium hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors"
        >
          ↻ Refresh
        </button>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-red-700 dark:text-red-400 text-sm" role="alert">
          {error}
        </div>
      )}

      {actions.length === 0 ? (
        <div className="text-center py-12 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800">
          <span className="text-4xl block mb-3" role="img" aria-label="empty">✅</span>
          <p className="text-gray-500 dark:text-gray-400">No pending operations</p>
          <p className="text-sm text-gray-400 dark:text-gray-500 mt-1">
            Deferred actions from rules will appear here
          </p>
        </div>
      ) : (
        <div className="space-y-3">
          {actions.map((action) => (
            <div
              key={action.id}
              className="bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 p-4"
            >
              <div className="flex items-start justify-between">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-lg">{actionEmoji(action.action)}</span>
                    <span className="font-medium text-gray-900 dark:text-white capitalize">
                      {action.action}
                    </span>
                    {action.target && (
                      <span className="text-sm text-gray-500 dark:text-gray-400">
                        → {action.target}
                      </span>
                    )}
                    <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-400">
                      {formatTimeUntil(action.execute_at)}
                    </span>
                  </div>
                  <div className="mt-1 text-sm text-gray-500 dark:text-gray-400 space-y-0.5">
                    <p>
                      <span className="text-gray-400 dark:text-gray-500">Rule:</span>{' '}
                      {action.rule_name}
                    </p>
                    <p className="truncate">
                      <span className="text-gray-400 dark:text-gray-500">Message:</span>{' '}
                      {action.message_id}
                    </p>
                    <p className="text-xs text-gray-400 dark:text-gray-500">
                      Scheduled: {new Date(action.execute_at).toLocaleString()} · Created: {new Date(action.created_at).toLocaleString()}
                    </p>
                  </div>
                </div>
                <button
                  onClick={() => handleCancel(action)}
                  className="ml-4 px-3 py-1.5 bg-red-50 dark:bg-red-900/20 text-red-600 dark:text-red-400 rounded-lg text-sm font-medium hover:bg-red-100 dark:hover:bg-red-900/40 transition-colors shrink-0"
                >
                  Cancel
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
