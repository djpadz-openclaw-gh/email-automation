'use client';

import { useState, useEffect } from 'react';
import api, { PasskeyInfo, APIKeyInfo, TOTPSetupResponse, base64urlToBuffer } from '@/lib/api';

interface SettingsProps {
  onBack: () => void;
}

export default function Settings({ onBack }: SettingsProps) {
  const [activeSection, setActiveSection] = useState<'password' | 'totp' | 'passkeys' | 'apikeys' | 'exempt-folders'>('password');
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');

  // Profile
  const [profile, setProfile] = useState<{ username: string; totp_enabled: boolean; passkey_count: number } | null>(null);

  // Password
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmNewPassword, setConfirmNewPassword] = useState('');
  const [passwordLoading, setPasswordLoading] = useState(false);

  // TOTP
  const [totpData, setTotpData] = useState<TOTPSetupResponse | null>(null);
  const [totpToken, setTotpToken] = useState('');
  const [totpLoading, setTotpLoading] = useState(false);
  const [showManualEntry, setShowManualEntry] = useState(false);
  const [disablePassword, setDisablePassword] = useState('');

  // Passkeys
  const [passkeys, setPasskeys] = useState<PasskeyInfo[]>([]);
  const [passkeysLoading, setPasskeysLoading] = useState(false);
  const [newPasskeyName, setNewPasskeyName] = useState('');

  // API Keys
  const [apiKeys, setApiKeys] = useState<APIKeyInfo[]>([]);
  const [apiKeysLoading, setApiKeysLoading] = useState(false);
  const [newKeyName, setNewKeyName] = useState('');
  const [newKeyValue, setNewKeyValue] = useState('');

  // Exempt Folders
  const [exemptFolders, setExemptFolders] = useState<string[]>([]);
  const [exemptFoldersLoading, setExemptFoldersLoading] = useState(false);
  const [newExemptFolder, setNewExemptFolder] = useState('');

  useEffect(() => {
    loadProfile();
  }, []);

  useEffect(() => {
    if (activeSection === 'passkeys') loadPasskeys();
    if (activeSection === 'apikeys') loadAPIKeys();
    if (activeSection === 'exempt-folders') loadExemptFolders();
  }, [activeSection]);

  const loadProfile = async () => {
    try {
      const p = await api.getProfile();
      setProfile(p);
    } catch { /* ignore */ }
  };

  const loadPasskeys = async () => {
    try {
      setPasskeysLoading(true);
      const data = await api.passkeyList();
      setPasskeys(data || []);
    } catch { /* ignore */ } finally {
      setPasskeysLoading(false);
    }
  };

  const loadAPIKeys = async () => {
    try {
      setApiKeysLoading(true);
      const data = await api.apiKeyList();
      setApiKeys(data || []);
    } catch { /* ignore */ } finally {
      setApiKeysLoading(false);
    }
  };

  const loadExemptFolders = async () => {
    try {
      setExemptFoldersLoading(true);
      const data = await api.listExemptFolders();
      setExemptFolders(data.exempt_folders || []);
    } catch { /* ignore */ } finally {
      setExemptFoldersLoading(false);
    }
  };

  const handleAddExemptFolder = async () => {
    if (!newExemptFolder.trim()) {
      setError('Folder name is required');
      return;
    }
    try {
      setExemptFoldersLoading(true);
      setError('');
      const data = await api.addExemptFolder(newExemptFolder.trim());
      setExemptFolders(data.exempt_folders || []);
      setNewExemptFolder('');
      setSuccess('Folder added to exempt list');
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to add exempt folder');
    } finally {
      setExemptFoldersLoading(false);
    }
  };

  const handleRemoveExemptFolder = async (folder: string) => {
    try {
      setExemptFoldersLoading(true);
      setError('');
      const data = await api.removeExemptFolder(folder);
      setExemptFolders(data.exempt_folders || []);
      setSuccess(`Removed "${folder}" from exempt list`);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to remove exempt folder');
    } finally {
      setExemptFoldersLoading(false);
    }
  };

  const handleChangePassword = async (e: React.FormEvent) => {
    e.preventDefault();
    if (newPassword !== confirmNewPassword) {
      setError('New passwords do not match');
      return;
    }
    try {
      setPasswordLoading(true);
      setError('');
      setSuccess('');
      await api.changePassword(currentPassword, newPassword);
      setSuccess('Password changed successfully');
      setCurrentPassword('');
      setNewPassword('');
      setConfirmNewPassword('');
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to change password');
    } finally {
      setPasswordLoading(false);
    }
  };

  const handleTOTPSetup = async () => {
    try {
      setTotpLoading(true);
      setError('');
      const data = await api.totpSetup();
      setTotpData(data);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to setup TOTP');
    } finally {
      setTotpLoading(false);
    }
  };

  const handleTOTPVerify = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      setTotpLoading(true);
      setError('');
      await api.totpVerify(totpToken);
      setSuccess('Two-factor authentication enabled');
      setTotpData(null);
      setTotpToken('');
      loadProfile();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Invalid code');
    } finally {
      setTotpLoading(false);
    }
  };

  const handleTOTPDisable = async () => {
    if (!disablePassword) {
      setError('Password is required to disable TOTP');
      return;
    }
    try {
      setTotpLoading(true);
      setError('');
      await api.totpDisable(disablePassword);
      setSuccess('Two-factor authentication disabled');
      setDisablePassword('');
      loadProfile();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to disable TOTP');
    } finally {
      setTotpLoading(false);
    }
  };

  const handleAddPasskey = async () => {
    try {
      setPasskeysLoading(true);
      setError('');
      const options = await api.passkeyRegisterBegin();
      const publicKey = options as any;
      const pk = publicKey.publicKey || publicKey;

      const createOptions: PublicKeyCredentialCreationOptions = {
        challenge: base64urlToBuffer(pk.challenge),
        rp: pk.rp,
        user: { ...pk.user, id: base64urlToBuffer(pk.user.id) },
        pubKeyCredParams: pk.pubKeyCredParams,
        timeout: pk.timeout || 60000,
        attestation: pk.attestation || 'none',
        authenticatorSelection: pk.authenticatorSelection,
      };

      if (pk.excludeCredentials) {
        createOptions.excludeCredentials = pk.excludeCredentials.map((cred: any) => ({
          id: base64urlToBuffer(cred.id),
          type: cred.type,
          transports: cred.transports,
        }));
      }

      const credential = await navigator.credentials.create({ publicKey: createOptions }) as PublicKeyCredential;
      if (!credential) return;

      await api.passkeyRegisterComplete(credential, newPasskeyName || undefined);
      setSuccess('Passkey registered');
      setNewPasskeyName('');
      loadPasskeys();
      loadProfile();
    } catch (err: unknown) {
      if (err instanceof Error && err.name !== 'NotAllowedError') {
        setError(err instanceof Error ? err.message : 'Failed to register passkey');
      }
    } finally {
      setPasskeysLoading(false);
    }
  };

  const handleDeletePasskey = async (id: number) => {
    if (!confirm('Remove this passkey?')) return;
    try {
      await api.passkeyDelete(id);
      loadPasskeys();
      loadProfile();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to delete passkey');
    }
  };

  const handleCreateAPIKey = async () => {
    if (!newKeyName.trim()) {
      setError('API key name is required');
      return;
    }
    try {
      setApiKeysLoading(true);
      setError('');
      const result = await api.apiKeyCreate(newKeyName.trim());
      setNewKeyValue(result.key);
      setNewKeyName('');
      loadAPIKeys();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to create API key');
    } finally {
      setApiKeysLoading(false);
    }
  };

  const handleDeleteAPIKey = async (id: number) => {
    if (!confirm('Revoke this API key?')) return;
    try {
      await api.apiKeyDelete(id);
      loadAPIKeys();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to delete API key');
    }
  };

  const sections = [
    { id: 'password' as const, label: '🔒 Password', icon: '🔒' },
    { id: 'totp' as const, label: '📱 2FA', icon: '📱' },
    { id: 'passkeys' as const, label: '🔑 Passkeys', icon: '🔑' },
    { id: 'apikeys' as const, label: '🗝️ API Keys', icon: '🗝️' },
    { id: 'exempt-folders' as const, label: '📁 Exempt Folders', icon: '📁' },
  ];

  return (
    <div>
      <div className="flex items-center gap-3 mb-6">
        <button onClick={onBack} className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" aria-label="Back">
          ← Back
        </button>
        <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
          Settings {profile && <span className="text-sm font-normal text-gray-500">({profile.username})</span>}
        </h2>
      </div>

      {(error || success) && (
        <div className={`mb-4 p-3 rounded-lg text-sm ${
          error
            ? 'bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-700 dark:text-red-400'
            : 'bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 text-green-700 dark:text-green-400'
        }`} role="alert">
          {error || success}
        </div>
      )}

      {/* Section tabs */}
      <nav className="flex gap-1 bg-white dark:bg-gray-900 rounded-lg p-1 shadow-sm border border-gray-200 dark:border-gray-800 w-fit mb-6" aria-label="Settings sections">
        {sections.map((s) => (
          <button
            key={s.id}
            onClick={() => { setActiveSection(s.id); setError(''); setSuccess(''); }}
            className={`px-4 py-2 rounded-md text-sm font-medium transition-colors ${
              activeSection === s.id
                ? 'bg-blue-600 text-white'
                : 'text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800'
            }`}
          >
            {s.label}
          </button>
        ))}
      </nav>

      {/* Password Section */}
      {activeSection === 'password' && (
        <div className="bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 p-6 max-w-lg">
          <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-4">Change Password</h3>
          <form onSubmit={handleChangePassword} className="space-y-4">
            <div>
              <label htmlFor="current-pw" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Current Password</label>
              <input id="current-pw" type="password" value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)} className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 outline-none" autoComplete="current-password" />
            </div>
            <div>
              <label htmlFor="new-pw" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">New Password</label>
              <input id="new-pw" type="password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} placeholder="Min 12 chars, mixed case, digit, special" className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 outline-none" autoComplete="new-password" />
            </div>
            <div>
              <label htmlFor="confirm-new-pw" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Confirm New Password</label>
              <input id="confirm-new-pw" type="password" value={confirmNewPassword} onChange={(e) => setConfirmNewPassword(e.target.value)} className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 outline-none" autoComplete="new-password" />
            </div>
            <button type="submit" disabled={passwordLoading || !currentPassword || !newPassword || !confirmNewPassword} className="px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50 transition-colors">
              {passwordLoading ? 'Changing...' : 'Change Password'}
            </button>
          </form>
        </div>
      )}

      {/* TOTP Section */}
      {activeSection === 'totp' && (
        <div className="bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 p-6 max-w-lg">
          <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-4">Two-Factor Authentication</h3>

          {profile?.totp_enabled ? (
            <div className="space-y-4">
              <div className="flex items-center gap-2 p-3 bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg">
                <span className="text-green-600">✓</span>
                <span className="text-sm text-green-700 dark:text-green-400">Two-factor authentication is enabled</span>
              </div>
              <div>
                <label htmlFor="disable-pw" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Enter password to disable</label>
                <input id="disable-pw" type="password" value={disablePassword} onChange={(e) => setDisablePassword(e.target.value)} className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 outline-none" />
              </div>
              <button onClick={handleTOTPDisable} disabled={totpLoading || !disablePassword} className="px-4 py-2 bg-red-600 text-white rounded-lg text-sm font-medium hover:bg-red-700 disabled:opacity-50 transition-colors">
                {totpLoading ? 'Disabling...' : 'Disable 2FA'}
              </button>
            </div>
          ) : totpData ? (
            <form onSubmit={handleTOTPVerify} className="space-y-4">
              <div className="flex justify-center">
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img src={totpData.qr_code} alt="TOTP QR Code" className="w-48 h-48 rounded-lg border border-gray-200 dark:border-gray-700" />
              </div>
              <button type="button" onClick={() => setShowManualEntry(!showManualEntry)} className="w-full text-sm text-blue-600 dark:text-blue-400 hover:underline">
                {showManualEntry ? 'Hide key' : 'Enter key manually'}
              </button>
              {showManualEntry && (
                <div className="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
                  <code className="text-sm font-mono text-gray-900 dark:text-white break-all select-all">{totpData.secret}</code>
                </div>
              )}
              <div>
                <label htmlFor="settings-totp" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Verification Code</label>
                <input id="settings-totp" type="text" value={totpToken} onChange={(e) => setTotpToken(e.target.value.replace(/\D/g, '').slice(0, 6))} placeholder="000000" className="w-full px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm text-center text-lg tracking-widest font-mono focus:ring-2 focus:ring-blue-500 outline-none" inputMode="numeric" maxLength={6} autoFocus />
              </div>
              <button type="submit" disabled={totpLoading || totpToken.length !== 6} className="px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50 transition-colors">
                {totpLoading ? 'Verifying...' : 'Verify & Enable'}
              </button>
            </form>
          ) : (
            <div className="space-y-4">
              <p className="text-sm text-gray-500 dark:text-gray-400">Add an extra layer of security with an authenticator app.</p>
              <button onClick={handleTOTPSetup} disabled={totpLoading} className="px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50 transition-colors">
                {totpLoading ? 'Setting up...' : 'Setup 2FA'}
              </button>
            </div>
          )}
        </div>
      )}

      {/* Passkeys Section */}
      {activeSection === 'passkeys' && (
        <div className="bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 p-6">
          <div className="flex items-center justify-between mb-4">
            <h3 className="text-sm font-semibold text-gray-900 dark:text-white">Passkeys</h3>
            <div className="flex items-center gap-2">
              <input type="text" value={newPasskeyName} onChange={(e) => setNewPasskeyName(e.target.value)} placeholder="Passkey name (optional)" className="px-3 py-1.5 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 outline-none" />
              <button onClick={handleAddPasskey} disabled={passkeysLoading} className="px-3 py-1.5 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50 transition-colors">
                + Add
              </button>
            </div>
          </div>

          {passkeysLoading ? (
            <p className="text-sm text-gray-500">Loading...</p>
          ) : passkeys.length === 0 ? (
            <p className="text-sm text-gray-500 dark:text-gray-400">No passkeys registered. Add one for passwordless sign-in.</p>
          ) : (
            <div className="space-y-2">
              {passkeys.map((pk) => (
                <div key={pk.id} className="flex items-center justify-between p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
                  <div>
                    <p className="text-sm font-medium text-gray-900 dark:text-white">🔑 {pk.name}</p>
                    <p className="text-xs text-gray-500 dark:text-gray-400">
                      Added {new Date(pk.created_at).toLocaleDateString()}
                      {pk.last_used_at && ` • Last used ${new Date(pk.last_used_at).toLocaleDateString()}`}
                    </p>
                  </div>
                  <button onClick={() => handleDeletePasskey(pk.id)} className="p-1.5 text-gray-400 hover:text-red-600 dark:hover:text-red-400 transition-colors" title="Remove" aria-label="Remove passkey">
                    🗑
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* API Keys Section */}
      {activeSection === 'apikeys' && (
        <div className="bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 p-6">
          <div className="flex items-center justify-between mb-4">
            <h3 className="text-sm font-semibold text-gray-900 dark:text-white">API Keys</h3>
            <div className="flex items-center gap-2">
              <input type="text" value={newKeyName} onChange={(e) => setNewKeyName(e.target.value)} placeholder="Key name" className="px-3 py-1.5 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 outline-none" />
              <button onClick={handleCreateAPIKey} disabled={apiKeysLoading || !newKeyName.trim()} className="px-3 py-1.5 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50 transition-colors">
                + Generate
              </button>
            </div>
          </div>

          {newKeyValue && (
            <div className="mb-4 p-3 bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded-lg">
              <p className="text-xs text-yellow-700 dark:text-yellow-400 mb-1 font-medium">⚠️ Copy this key now — it won&apos;t be shown again</p>
              <code className="text-sm font-mono text-gray-900 dark:text-white break-all select-all">{newKeyValue}</code>
              <button onClick={() => { navigator.clipboard.writeText(newKeyValue); setNewKeyValue(''); setSuccess('API key copied to clipboard'); }} className="mt-2 px-3 py-1 bg-yellow-200 dark:bg-yellow-800 text-yellow-800 dark:text-yellow-200 rounded text-xs font-medium hover:bg-yellow-300 dark:hover:bg-yellow-700 transition-colors">
                📋 Copy & Dismiss
              </button>
            </div>
          )}

          {apiKeysLoading ? (
            <p className="text-sm text-gray-500">Loading...</p>
          ) : apiKeys.length === 0 ? (
            <p className="text-sm text-gray-500 dark:text-gray-400">No API keys. Generate one for programmatic access.</p>
          ) : (
            <div className="space-y-2">
              {apiKeys.map((key) => (
                <div key={key.id} className="flex items-center justify-between p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
                  <div>
                    <p className="text-sm font-medium text-gray-900 dark:text-white">🗝️ {key.name}</p>
                    <p className="text-xs text-gray-500 dark:text-gray-400">
                      {key.prefix} • Created {new Date(key.created_at).toLocaleDateString()}
                      {key.last_used_at && ` • Last used ${new Date(key.last_used_at).toLocaleDateString()}`}
                    </p>
                  </div>
                  <button onClick={() => handleDeleteAPIKey(key.id)} className="p-1.5 text-gray-400 hover:text-red-600 dark:hover:text-red-400 transition-colors" title="Revoke" aria-label="Revoke API key">
                    🗑
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Exempt Folders Section */}
      {activeSection === 'exempt-folders' && (
        <div className="bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 p-6">
          <div className="flex items-center justify-between mb-4">
            <div>
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white">Exempt Folders</h3>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">Messages moved to these folders won&apos;t trigger automatic rule creation.</p>
            </div>
            <div className="flex items-center gap-2">
              <input
                type="text"
                value={newExemptFolder}
                onChange={(e) => setNewExemptFolder(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter') handleAddExemptFolder(); }}
                placeholder="Folder name"
                className="px-3 py-1.5 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white text-sm focus:ring-2 focus:ring-blue-500 outline-none"
              />
              <button onClick={handleAddExemptFolder} disabled={exemptFoldersLoading || !newExemptFolder.trim()} className="px-3 py-1.5 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50 transition-colors">
                + Add
              </button>
            </div>
          </div>

          {exemptFoldersLoading ? (
            <p className="text-sm text-gray-500">Loading...</p>
          ) : exemptFolders.length === 0 ? (
            <p className="text-sm text-gray-500 dark:text-gray-400">No exempt folders configured. Moves to any folder will trigger rule creation.</p>
          ) : (
            <div className="space-y-2">
              {exemptFolders.map((folder) => (
                <div key={folder} className="flex items-center justify-between p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
                  <p className="text-sm font-medium text-gray-900 dark:text-white">📁 {folder}</p>
                  <button onClick={() => handleRemoveExemptFolder(folder)} className="p-1.5 text-gray-400 hover:text-red-600 dark:hover:text-red-400 transition-colors" title="Remove" aria-label={`Remove ${folder} from exempt list`}>
                    🗑
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
