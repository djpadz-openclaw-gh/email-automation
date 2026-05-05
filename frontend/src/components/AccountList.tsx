'use client';

import { useState, useEffect, useCallback } from 'react';
import api, { Account } from '@/lib/api';

export default function AccountList() {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [showForm, setShowForm] = useState(false);
  const [editingAccount, setEditingAccount] = useState<Account | null>(null);

  // OAuth state
  const [oauthProviders, setOauthProviders] = useState<string[]>([]);
  const [oauthLoading, setOauthLoading] = useState<string | null>(null);

  // Form state
  const [formName, setFormName] = useState('');
  const [formEmail, setFormEmail] = useState('');
  const [formHost, setFormHost] = useState('');
  const [formPort, setFormPort] = useState(993);
  const [formTLS, setFormTLS] = useState(true);
  const [formUsername, setFormUsername] = useState('');
  const [formPassword, setFormPassword] = useState('');
  const [formSaving, setFormSaving] = useState(false);

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
      const data = await api.listOAuthProviders();
      if (data.providers) {
        setOauthProviders(data.providers.map((p) => p.name));
      }
    } catch {
      // OAuth providers not available - that's fine, just don't show buttons
    }
  }, []);

  useEffect(() => {
    loadAccounts();
    loadOAuthProviders();
  }, [loadAccounts, loadOAuthProviders]);

  // OAuth connect handler - opens popup and listens for postMessage
  const handleOAuthConnect = async (provider: string) => {
    try {
      setOauthLoading(provider);
      setError('');
      setSuccess('');
      const data = await api.oauthConnect(provider);
      
      // Open OAuth in a popup window
      const popup = window.open(data.auth_url, 'oauth_popup', 'width=500,height=600');
      if (!popup) {
        setError('Failed to open OAuth popup. Please check your browser popup settings.');
        setOauthLoading(null);
        return;
      }
      
      // Listen for postMessage from the OAuth callback
      const handleMessage = (event: MessageEvent) => {
        if (event.data?.type === 'oauth_callback') {
          window.removeEventListener('message', handleMessage);
          setOauthLoading(null);
          
          if (event.data.success) {
            setSuccess('Account connected successfully!');
            loadAccounts();
          } else {
            setError(`OAuth failed: ${event.data.message}`);
          }
        }
      };
      
      window.addEventListener('message', handleMessage);
      
      // Clean up listener if popup is closed manually
      const checkPopupClosed = setInterval(() => {
        if (popup.closed) {
          clearInterval(checkPopupClosed);
          window.removeEventListener('message', handleMessage);
          setOauthLoading(null);
        }
      }, 500);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : `Failed to connect ${provider}`);
      setOauthLoading(null);
    }
  };

  const handleSave = async () => {
    if (!formName.trim() || !formEmail.trim() || !formHost.trim()) {
      setError('Name, email, and IMAP host are required');
      return;
    }

    try {
      setFormSaving(true);
      setError('');

      const data: Partial<Account> & { password?: string } = {
        name: formName.trim(),
        email: formEmail.trim(),
        provider: 'imap',
        imap_host: formHost.trim(),
        imap_port: formPort,
        imap_tls: formTLS,
        username: formUsername.trim() || formEmail.trim(),
        active: editingAccount?.active ?? true,
      };

      // Only include password if it was entered (for edits, empty means "don't change")
      if (formPassword) {
        (data as Record<string, unknown>).password = formPassword;
      }

      if (editingAccount) {
        await api.updateAccount(editingAccount.id, data);
      } else {
        await api.createAccount(data);
      }

      setShowForm(false);
      setEditingAccount(null);
      resetForm();
      await loadAccounts();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : `Failed to ${editingAccount ? 'update' : 'create'} account`);
    } finally {
      setFormSaving(false);
    }
  };

  const handleEdit = (account: Account) => {
    setEditingAccount(account);
    setFormName(account.name);
    setFormEmail(account.email);
    if (account.provider === 'imap') {
      setFormHost(account.imap_host);
      setFormPort(account.imap_port);
      setFormTLS(account.imap_tls);
      setFormUsername(account.username || '');
    }
    setFormPassword('');
    setShowForm(true);
    setError('');
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
    setEditingAccount(null);
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
          onClick={() => { setShowForm(!showForm); if (showForm) resetForm(); }}
          className="px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 transition-colors"
        >
          {showForm ? 'Cancel' : '+ Add Account'}
        </button>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-red-700 dark:text-red-400 text-sm" role="alert">
          {error}
        </div>
      )}

      {success && (
        <div className="mb-4 p-3 bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg text-green-700 dark:text-green-400 text-sm" role="status">
          {success}
        </div>
      )}

      {/* OAuth connect buttons */}
      {oauthProviders.length > 0 && !showForm && (
        <div className="mb-6 bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-xl p-4">
          <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">Connect via OAuth</h3>
          <div className="flex flex-wrap gap-3">
            {oauthProviders.includes('gmail') && (
              <button
                onClick={() => handleOAuthConnect('gmail')}
                disabled={oauthLoading !== null}
                className="inline-flex items-center gap-2 px-4 py-2.5 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-lg text-sm font-medium text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors shadow-sm"
              >
                <svg className="w-5 h-5" viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z" fill="#4285F4"/>
                  <path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853"/>
                  <path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z" fill="#FBBC05"/>
                  <path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335"/>
                </svg>
                {oauthLoading === 'gmail' ? 'Connecting...' : 'Connect Gmail'}
              </button>
            )}
            {oauthProviders.includes('microsoft365') && (
              <button
                onClick={() => handleOAuthConnect('microsoft365')}
                disabled={oauthLoading !== null}
                className="inline-flex items-center gap-2 px-4 py-2.5 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-lg text-sm font-medium text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors shadow-sm"
              >
                <svg className="w-5 h-5" viewBox="0 0 23 23" aria-hidden="true">
                  <path fill="#f35325" d="M1 1h10v10H1z"/>
                  <path fill="#81bc06" d="M12 1h10v10H12z"/>
                  <path fill="#05a6f0" d="M1 12h10v10H1z"/>
                  <path fill="#ffba08" d="M12 12h10v10H12z"/>
                </svg>
                {oauthLoading === 'microsoft365' ? 'Connecting...' : 'Connect Microsoft 365'}
              </button>
            )}
          </div>
        </div>
      )}

      {/* Add account form */}
      {showForm && (
        <div className="mb-6 bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-xl p-6">
          {/* OAuth Account Edit Form */}
          {editingAccount && editingAccount.provider !== 'imap' ? (
            <>
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-4">
                Edit: {editingAccount.name}
              </h3>
              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                    Account Type
                  </label>
                  <p className="text-sm text-gray-600 dark:text-gray-400">
                    {editingAccount.provider === 'gmail' ? 'Gmail (OAuth)' : 'Microsoft 365 (OAuth)'}
                  </p>
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                    Email
                  </label>
                  <p className="text-sm text-gray-600 dark:text-gray-400">{editingAccount.email}</p>
                </div>
                <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-3 text-sm text-blue-700 dark:text-blue-400">
                  To update your authentication, click the reauthenticate button below.
                </div>
              </div>
              <div className="flex gap-3 mt-6">
                <button
                  onClick={() => handleOAuthConnect(editingAccount.provider)}
                  disabled={oauthLoading !== null}
                  className="px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50 transition-colors"
                >
                  {oauthLoading === editingAccount.provider ? 'Reauthenticating...' : '🔄 Reauthenticate'}
                </button>
                <button
                  onClick={() => { setShowForm(false); resetForm(); }}
                  className="px-4 py-2 bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400 rounded-lg text-sm font-medium hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors"
                >
                  Close
                </button>
              </div>
            </>
          ) : (
            <>
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-4">
                {editingAccount ? `Edit: ${editingAccount.name}` : 'New IMAP Account'}
              </h3>
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
              <label htmlFor="acc-pass" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Password{editingAccount && <span className="text-gray-400 font-normal"> (leave blank to keep current)</span>}
              </label>
              <input id="acc-pass" type="password" value={formPassword} onChange={(e) => setFormPassword(e.target.value)} placeholder={editingAccount ? '••••••••' : ''} className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 outline-none" />
            </div>
              </div>
              <div className="flex gap-3 mt-4">
                <button onClick={handleSave} disabled={formSaving} className="px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50 transition-colors">
                  {formSaving ? (editingAccount ? 'Updating...' : 'Creating...') : (editingAccount ? 'Update Account' : 'Create Account')}
                </button>
                <button onClick={() => { setShowForm(false); resetForm(); }} className="px-4 py-2 bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400 rounded-lg text-sm font-medium hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors">
                  Cancel
                </button>
              </div>
            </>
          )}
        </div>
      )}}

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
                    <h3 className="font-medium text-gray-900 dark:text-white">{account.name}</h3>
                    <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${
                      account.active
                        ? 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400'
                        : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400'
                    }`}>
                      {account.active ? 'Active' : 'Disabled'}
                    </span>
                  </div>
                  <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{account.email}</p>
                  <p className="text-xs text-gray-400 dark:text-gray-500 mt-1">
                    {account.provider.toUpperCase()} • {account.imap_host}:{account.imap_port}
                    {account.last_sync_at && ` • Last sync: ${new Date(account.last_sync_at).toLocaleString()}`}
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <button onClick={() => handleEdit(account)} className="p-2 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 transition-colors" title="Edit" aria-label="Edit account">
                    ✏️
                  </button>
                  <button onClick={() => handleToggle(account)} className="p-2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors" title={account.active ? 'Disable' : 'Enable'} aria-label={account.active ? 'Disable account' : 'Enable account'}>
                    {account.active ? '⏸' : '▶️'}
                  </button>
                  <button onClick={() => handleDelete(account)} className="p-2 text-gray-400 hover:text-red-600 dark:hover:text-red-400 transition-colors" title="Delete" aria-label="Delete account">
                    🗑
                  </button>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
