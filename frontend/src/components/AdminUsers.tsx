'use client';

import { useState, useEffect } from 'react';
import api, { AdminUser } from '@/lib/api';

interface AdminUsersProps {
  onBack?: () => void;
}

export default function AdminUsers({ onBack }: AdminUsersProps) {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [adminKey, setAdminKey] = useState('');
  const [authenticated, setAuthenticated] = useState(false);
  const [updating, setUpdating] = useState<number | null>(null);

  const loadUsers = async () => {
    setLoading(true);
    setError('');
    try {
      const response = await api.adminListUsers();
      setUsers(response.users);
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Failed to load users';
      setError(message);
    } finally {
      setLoading(false);
    }
  };

  const handleAuth = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!adminKey.trim()) return;
    api.setAdminKey(adminKey.trim());
    setAuthenticated(true);
    localStorage.setItem('ea_admin_key', adminKey.trim());
    await loadUsers();
  };

  const handleToggleAI = async (userId: number, currentValue: boolean) => {
    setUpdating(userId);
    setError('');
    try {
      await api.adminUpdateUserAI(userId, !currentValue);
      setUsers(users.map(u =>
        u.id === userId ? { ...u, ai_enabled: !currentValue } : u
      ));
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Failed to update user';
      setError(message);
    } finally {
      setUpdating(null);
    }
  };

  useEffect(() => {
    const stored = localStorage.getItem('ea_admin_key');
    if (stored) {
      setAdminKey(stored);
      api.setAdminKey(stored);
      setAuthenticated(true);
      loadUsers();
    } else {
      setLoading(false);
    }
  }, []);

  if (!authenticated) {
    return (
      <div className="max-w-md mx-auto mt-8">
        <h2 className="text-xl font-semibold mb-4 text-gray-900 dark:text-gray-100">
          Admin Authentication
        </h2>
        <p className="text-sm text-gray-600 dark:text-gray-400 mb-4">
          Enter the system API key to access admin controls.
        </p>
        <form onSubmit={handleAuth} className="space-y-4">
          <div>
            <label htmlFor="admin-key" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              System API Key
            </label>
            <input
              id="admin-key"
              type="password"
              value={adminKey}
              onChange={(e) => setAdminKey(e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 focus:ring-2 focus:ring-blue-500 focus:border-transparent"
              placeholder="Enter admin API key"
              autoComplete="off"
            />
          </div>
          <button
            type="submit"
            className="w-full px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 disabled:opacity-50"
            disabled={!adminKey.trim()}
          >
            Authenticate
          </button>
        </form>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">
          User AI Permissions
        </h2>
        <div className="flex gap-2">
          <button
            onClick={loadUsers}
            className="px-3 py-1.5 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 rounded-md hover:bg-gray-200 dark:hover:bg-gray-600"
            disabled={loading}
          >
            {loading ? 'Loading...' : 'Refresh'}
          </button>
          <button
            onClick={() => {
              localStorage.removeItem('ea_admin_key');
              api.setAdminKey('');
              setAuthenticated(false);
              setUsers([]);
              setAdminKey('');
            }}
            className="px-3 py-1.5 text-sm bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-400 rounded-md hover:bg-red-200 dark:hover:bg-red-900/50"
          >
            Logout Admin
          </button>
        </div>
      </div>

      <p className="text-sm text-gray-600 dark:text-gray-400">
        Control which users can use AI-powered rules (kiro.* functions). When disabled, rules with AI calls will still run but AI functions will return false.
      </p>

      {error && (
        <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-red-700 dark:text-red-400 text-sm">
          {error}
        </div>
      )}

      {loading ? (
        <div className="text-center py-8 text-gray-500 dark:text-gray-400">
          Loading users...
        </div>
      ) : users.length === 0 ? (
        <div className="text-center py-8 text-gray-500 dark:text-gray-400">
          No users found.
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm" role="grid" aria-label="User AI permissions">
            <thead>
              <tr className="border-b border-gray-200 dark:border-gray-700">
                <th className="text-left py-3 px-4 font-medium text-gray-700 dark:text-gray-300">User</th>
                <th className="text-left py-3 px-4 font-medium text-gray-700 dark:text-gray-300">Created</th>
                <th className="text-center py-3 px-4 font-medium text-gray-700 dark:text-gray-300">2FA</th>
                <th className="text-center py-3 px-4 font-medium text-gray-700 dark:text-gray-300">AI Enabled</th>
              </tr>
            </thead>
            <tbody>
              {users.map((user) => (
                <tr
                  key={user.id}
                  className="border-b border-gray-100 dark:border-gray-800 hover:bg-gray-50 dark:hover:bg-gray-800/50"
                >
                  <td className="py-3 px-4">
                    <div className="font-medium text-gray-900 dark:text-gray-100">
                      {user.username}
                    </div>
                    <div className="text-xs text-gray-500 dark:text-gray-400">
                      ID: {user.id}
                    </div>
                  </td>
                  <td className="py-3 px-4 text-gray-600 dark:text-gray-400">
                    {new Date(user.created_at).toLocaleDateString()}
                  </td>
                  <td className="py-3 px-4 text-center">
                    {user.totp_enabled ? (
                      <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-400">
                        Enabled
                      </span>
                    ) : (
                      <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400">
                        Off
                      </span>
                    )}
                  </td>
                  <td className="py-3 px-4 text-center">
                    <button
                      onClick={() => handleToggleAI(user.id, user.ai_enabled)}
                      disabled={updating === user.id}
                      className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 disabled:opacity-50 ${
                        user.ai_enabled
                          ? 'bg-blue-600'
                          : 'bg-gray-300 dark:bg-gray-600'
                      }`}
                      role="switch"
                      aria-checked={user.ai_enabled}
                      aria-label={`Toggle AI for ${user.username}`}
                    >
                      <span
                        className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                          user.ai_enabled ? 'translate-x-6' : 'translate-x-1'
                        }`}
                      />
                    </button>
                    {updating === user.id && (
                      <span className="ml-2 text-xs text-gray-500">Saving...</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
