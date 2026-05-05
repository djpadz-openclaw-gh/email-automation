'use client';

import { useState, useEffect, useCallback, useRef } from 'react';
import api, { Account, OAuth2Provider } from '@/lib/api';

type ProviderType = 'imap' | 'microsoft365' | 'gmail';

const PROVIDER_LABELS: Record<string, string> = {
  imap: 'IMAP',
  microsoft365: 'Microsoft 365',
  gmail: 'Gmail',
};

const PROVIDER_ICONS: Record<string, string> = {
  imap: '📧',
  microsoft365: '🏢',
  gmail: '✉️',
};

export default function AccountList() {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showForm, setShowForm] = useState(false);
  const [selectedProvider, setSelectedProvider] = useState<ProviderType>('imap');
  const [availableOAuthProviders, setAvailableOAuthProviders] = useState<OAuth2Provider[]>([]);
  const [oauthConnecting, setOauthConnecting] = useState<string | null>(null);

  // IMAP form state
  const [formName, setFormName] = useState('');
  const [formEmail, setFormEmail] = useState('');
  const [formHost, setFormHost] = useState('');
  const [formPort, setFormPort] = useState(993);
  const [formTLS, setFormTLS] = useState(true);
  const [formUsername, setFormUsername] = useState('');
  const [formPassword, setFormPassword] = useState('');
  const [formSaving, setFormSaving] = useState(false);

  // Ref for OAuth popup polling
  const oauthPopupRef = useRef<Window | null>(null);
  const pollIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const loadAccounts = useCallback(async () => {
    try {
      setLoading(true);
      setError('');
      const data = await api.listAccounts();
      setAccounts(data);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to load accounts');
    } finally {
      setLoading(false);
    }
  }, []);

  const loadOAuthProviders = useCallback(async () => {
    try {
      const providers = await api.listOAuth2Providers();
      setAvailableOAuthProviders(providers);
    } catch {
      // OAuth2 providers not available - that's fine, just show IMAP
    }
  }, []);

  useEffect(() => {
    loadAccounts();
    loadOAuthProviders();
  }, [loadAccounts, loadOAuthProviders]);

  // Listen for postMessage from OAuth popup and cleanup on unmount
  useEffect(() => {
    const handleMessage = (event: MessageEvent) => {
      if (event.data?.type === 'oauth2_complete') {
        if (pollIntervalRef.current) {
          clearInterval(pollIntervalRef.current);
          pollIntervalRef.current = null;
        }
        setOauthConnecting(null);
        if (event.data.success) {
          setShowForm(false);
          loadAccounts();
        } else if (event.data.error) {
          setError(event.data.error);
        }
      }
    };

    window.addEventListener('message', handleMessage);
    return () => {
      window.removeEventListener('message', handleMessage);
      if (pollIntervalRef.current) {
        clearInterval(pollIntervalRef.current);
      }
    };
  }, [loadAccounts]);

  const handleOAuthConnect = async (provider: string) => {
    try {
      setError('');
      setOauthConnecting(provider);

      const { auth_url } = await api.oauth2Connect(provider);

      // Open OAuth URL in a popup window
      const width = 600;
      const height = 700;
      const left = window.screenX + (window.outerWidth - width) / 2;
      const top = window.screenY + (window.outerHeight - height) / 2;

      const popup = window.open(
        auth_url,
        'oauth2_connect',
        `width=${width},height=${height},left=${left},top=${top},scrollbars=yes,resizable=yes`
      );

      if (!popup) {
        setError('Popup blocked. Please allow popups for this site and try again.');
        setOauthConnecting(null);
        return;
      }

      oauthPopupRef.current = popup;

      // Poll for popup close
      pollIntervalRef.current = setInterval(() => {
        if (!popup || popup.closed) {
          if (pollIntervalRef.current) {
            clearInterval(pollIntervalRef.current);
            pollIntervalRef.current = null;
          }
          oauthPopupRef.current = null;
          setOauthConnecting(null);
          // Refresh accounts - the OAuth callback should have created the account
          loadAccounts();
        }
      }, 500);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to start OAuth2 connection');
      setOauthConnecting(null);
    }
  };

  const handleDisconnect = async (account: Account) => {
    if (!confirm(`Disconnect OAuth2 account "${account.name}" (${account.email})?`)) return;
    try {
      await api.oauth2Disconnect(account.id);
      await loadAccounts();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to disconnect account');
    }
  };

  const handleCreate = async () => {
    if (!formName.trim() || !formEmail.trim() || !formHost.trim()) {
      setError('Name, email, and IMAP host are required');
      return;
    }

    try {
      setFormSaving(true);
      setError('');
      await api.createAccount({
        name: formName.trim(),
        email: formEmail.trim(),
        provider: 'imap',
        imap_host: formHost.trim(),
        imap_port: formPort,
        imap_tls: formTLS,
        username: formUsername.trim() || formEmail.trim(),
        active: true,
      });
      setShowForm(false);
      resetForm();
      await loadAccounts();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to create account');
    } finally {
      setFormSaving(false);
    }
  };

  const handleDelete = async (account: Account) => {
    if (!confirm(`Delete account "${account.name}" (${account.email})?`)) return;
    try {
      await api.deleteAccount(account.id);
      await loadAccounts();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to delete account');
    }
  };

  const handleToggle = async (account: Account) => {
    try {
      await api.updateAccount(account.id, { ...account, active: !account.active });
      await loadAccounts();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to update account');
    }
  };

  const resetForm = () => {
    setFormName('');
    setFormEmail('');
    setFormHost('');
    setFormPort(993);
    setFormTLS(true);
    setFormUsername('');
    setFormPassword('');
    setSelectedProvider('imap');
  };

  const isOAuthProvider = (provider: string): boolean => {
    return provider === 'microsoft365' || provider === 'gmail';
  };

  const getAccountProviderLabel = (account: Account): string => {
    if (account.oauth_provider) {
      return PROVIDER_LABELS[account.oauth_provider] || account.oauth_provider;
    }
    return 'IMAP';
  };

  const getAccountProviderIcon = (account: Account): string => {
    if (account.oauth_provider) {
      return PROVIDER_ICONS[account.oauth_provider] || '📧';
    }
    return '📧';
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="text-gray-500 dark:text-gray-400">Loading accounts...</div>
      </div>
    );
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
          Accounts ({accounts.length})
        </h2>
        <button
          onClick={() => setShowForm(!showForm)}
          className="px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 transition-colors"
        >
          + Add Account
        </button>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-red-700 dark:text-red-400 text-sm" role="alert">
          {error}
        </div>
      )}

      {/* Add account form */}
      {showForm && (
        <div className="mb-6 bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-xl p-6">
          <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-4">Add Account</h3>

          {/* Provider selection */}
          <div className="mb-5">
            <label htmlFor="provider-select" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Provider
            </label>
            <div className="grid grid-cols-3 gap-3">
              <button
                type="button"
                onClick={() => setSelectedProvider('imap')}
                className={`flex items-center justify-center gap-2 px-4 py-3 rounded-lg border text-sm font-medium transition-colors ${
                  selectedProvider === 'imap'
                    ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20 text-blue-700 dark:text-blue-400'
                    : 'border-gray-200 dark:border-gray-700 text-gray-600 dark:text-gray-400 hover:border-gray-300 dark:hover:border-gray-600'
                }`}
              >
                <span>📧</span> IMAP
              </button>
              {availableOAuthProviders.some(p => p.name === 'microsoft365') && (
                <button
                  type="button"
                  onClick={() => setSelectedProvider('microsoft365')}
                  className={`flex items-center justify-center gap-2 px-4 py-3 rounded-lg border text-sm font-medium transition-colors ${
                    selectedProvider === 'microsoft365'
                      ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20 text-blue-700 dark:text-blue-400'
                      : 'border-gray-200 dark:border-gray-700 text-gray-600 dark:text-gray-400 hover:border-gray-300 dark:hover:border-gray-600'
                  }`}
                >
                  <span>🏢</span> Microsoft 365
                </button>
              )}
              {availableOAuthProviders.some(p => p.name === 'gmail') && (
                <button
                  type="button"
                  onClick={() => setSelectedProvider('gmail')}
                  className={`flex items-center justify-center gap-2 px-4 py-3 rounded-lg border text-sm font-medium transition-colors ${
                    selectedProvider === 'gmail'
                      ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20 text-blue-700 dark:text-blue-400'
                      : 'border-gray-200 dark:border-gray-700 text-gray-600 dark:text-gray-400 hover:border-gray-300 dark:hover:border-gray-600'
                  }`}
                >
                  <span>✉️</span> Gmail
                </button>
              )}
            </div>
          </div>

          {/* IMAP form */}
          {selectedProvider === 'imap' && (
            <>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label htmlFor="acc-name" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Name</label>
                  <input id="acc-name" type="text" value={formName} onChange={(e) => setFormName(e.target.value)} placeholder="Personal" className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 outline-none" />
                </div>
                <div>
                  <label htmlFor="acc-email" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Email</label>
                  <input id="acc-email" type="email" value={formEmail} onChange={(e) => setFormEmail(e.target.value)} placeholder="user@example.com" className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 outline-none" />
                </div>
                <div>
                  <label htmlFor="acc-host" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">IMAP Host</label>
                  <input id="acc-host" type="text" value={formHost} onChange={(e) => setFormHost(e.target.value)} placeholder="imap.example.com" className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 outline-none" />
                </div>
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label htmlFor="acc-port" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Port</label>
                    <input id="acc-port" type="number" value={formPort} onChange={(e) => setFormPort(parseInt(e.target.value) || 993)} className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 outline-none" />
                  </div>
                  <div className="flex items-end pb-2">
                    <label className="flex items-center gap-2 cursor-pointer">
                      <input type="checkbox" checked={formTLS} onChange={(e) => setFormTLS(e.target.checked)} className="w-4 h-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500" />
                      <span className="text-sm text-gray-700 dark:text-gray-300">TLS</span>
                    </label>
                  </div>
                </div>
                <div>
                  <label htmlFor="acc-user" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Username (optional)</label>
                  <input id="acc-user" type="text" value={formUsername} onChange={(e) => setFormUsername(e.target.value)} placeholder="Defaults to email" className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 outline-none" />
                </div>
                <div>
                  <label htmlFor="acc-pass" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Password</label>
                  <input id="acc-pass" type="password" value={formPassword} onChange={(e) => setFormPassword(e.target.value)} className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 outline-none" />
                </div>
              </div>
              <div className="flex gap-3 mt-4">
                <button onClick={handleCreate} disabled={formSaving} className="px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50 transition-colors">
                  {formSaving ? 'Creating...' : 'Create Account'}
                </button>
                <button onClick={() => { setShowForm(false); resetForm(); }} className="px-4 py-2 bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400 rounded-lg text-sm font-medium hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors">
                  Cancel
                </button>
              </div>
            </>
          )}

          {/* Microsoft 365 OAuth */}
          {selectedProvider === 'microsoft365' && (
            <div className="text-center py-6">
              <div className="text-4xl mb-3">🏢</div>
              <p className="text-sm text-gray-600 dark:text-gray-400 mb-4">
                Connect your Microsoft 365 account using OAuth2. You&apos;ll be redirected to Microsoft to authorize access.
              </p>
              <div className="flex justify-center gap-3">
                <button
                  onClick={() => handleOAuthConnect('microsoft365')}
                  disabled={oauthConnecting === 'microsoft365'}
                  className="px-6 py-3 bg-[#0078d4] text-white rounded-lg text-sm font-medium hover:bg-[#106ebe] disabled:opacity-50 transition-colors flex items-center gap-2"
                >
                  {oauthConnecting === 'microsoft365' ? (
                    <>
                      <svg className="animate-spin h-4 w-4" viewBox="0 0 24 24" fill="none">
                        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                      </svg>
                      Connecting...
                    </>
                  ) : (
                    'Connect with Microsoft 365'
                  )}
                </button>
                <button onClick={() => { setShowForm(false); resetForm(); }} className="px-4 py-3 bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400 rounded-lg text-sm font-medium hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors">
                  Cancel
                </button>
              </div>
            </div>
          )}

          {/* Gmail OAuth */}
          {selectedProvider === 'gmail' && (
            <div className="text-center py-6">
              <div className="text-4xl mb-3">✉️</div>
              <p className="text-sm text-gray-600 dark:text-gray-400 mb-4">
                Connect your Gmail account using OAuth2. You&apos;ll be redirected to Google to authorize access.
              </p>
              <div className="flex justify-center gap-3">
                <button
                  onClick={() => handleOAuthConnect('gmail')}
                  disabled={oauthConnecting === 'gmail'}
                  className="px-6 py-3 bg-[#ea4335] text-white rounded-lg text-sm font-medium hover:bg-[#d33426] disabled:opacity-50 transition-colors flex items-center gap-2"
                >
                  {oauthConnecting === 'gmail' ? (
                    <>
                      <svg className="animate-spin h-4 w-4" viewBox="0 0 24 24" fill="none">
                        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                      </svg>
                      Connecting...
                    </>
                  ) : (
                    'Connect with Gmail'
                  )}
                </button>
                <button onClick={() => { setShowForm(false); resetForm(); }} className="px-4 py-3 bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400 rounded-lg text-sm font-medium hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors">
                  Cancel
                </button>
              </div>
            </div>
          )}
        </div>
      )}

      {/* Account list */}
      {accounts.length === 0 ? (
        <div className="text-center py-12 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800">
          <span className="text-4xl block mb-3" role="img" aria-label="mailbox">📬</span>
          <p className="text-gray-500 dark:text-gray-400">No accounts configured</p>
          <button onClick={() => setShowForm(true)} className="mt-4 px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 transition-colors">
            Add your first account
          </button>
        </div>
      ) : (
        <div className="space-y-3">
          {accounts.map((account) => (
            <div key={account.id} className="bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 p-4">
              <div className="flex items-start justify-between">
                <div>
                  <div className="flex items-center gap-2">
                    <span>{getAccountProviderIcon(account)}</span>
                    <h3 className="font-medium text-gray-900 dark:text-white">{account.name}</h3>
                    <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${
                      account.active
                        ? 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400'
                        : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400'
                    }`}>
                      {account.active ? 'Active' : 'Disabled'}
                    </span>
                    <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400">
                      {getAccountProviderLabel(account)}
                    </span>
                  </div>
                  <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{account.email}</p>
                  <p className="text-xs text-gray-400 dark:text-gray-500 mt-1">
                    {account.oauth_provider
                      ? `OAuth2 • ${PROVIDER_LABELS[account.oauth_provider] || account.oauth_provider}`
                      : `IMAP • ${account.imap_host}:${account.imap_port}`
                    }
                    {account.last_sync_at && ` • Last sync: ${new Date(account.last_sync_at).toLocaleString()}`}
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <button onClick={() => handleToggle(account)} className="p-2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors" title={account.active ? 'Disable' : 'Enable'} aria-label={account.active ? 'Disable account' : 'Enable account'}>
                    {account.active ? '⏸' : '▶️'}
                  </button>
                  {account.oauth_provider ? (
                    <button onClick={() => handleDisconnect(account)} className="p-2 text-gray-400 hover:text-red-600 dark:hover:text-red-400 transition-colors" title="Disconnect" aria-label="Disconnect OAuth2 account">
                      🔌
                    </button>
                  ) : (
                    <button onClick={() => handleDelete(account)} className="p-2 text-gray-400 hover:text-red-600 dark:hover:text-red-400 transition-colors" title="Delete" aria-label="Delete account">
                      🗑
                    </button>
                  )}
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
