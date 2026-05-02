'use client';

import { useState } from 'react';
import api, { AuthResponse, base64urlToBuffer } from '@/lib/api';

interface LoginFormProps {
  onLogin: (response: AuthResponse) => void;
  onSwitchToRegister: () => void;
}

export default function LoginForm({ onLogin, onSwitchToRegister }: LoginFormProps) {
  const [activeTab, setActiveTab] = useState<'passkey' | 'password'>('passkey');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [totpToken, setTotpToken] = useState('');
  const [showTotp, setShowTotp] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const handlePasskeyLogin = async () => {
    try {
      setLoading(true);
      setError('');

      // Begin authentication
      const beginResp = await api.passkeyAuthenticateBegin();

      // Convert challenge and allowCredentials from base64url to ArrayBuffer
      const publicKey = beginResp.publicKey || (beginResp as any).Response || beginResp;
      const options: PublicKeyCredentialRequestOptions = {
        challenge: base64urlToBuffer(publicKey.challenge as unknown as string),
        timeout: publicKey.timeout || 60000,
        rpId: publicKey.rpId,
        userVerification: publicKey.userVerification as UserVerificationRequirement || 'preferred',
      };

      if (publicKey.allowCredentials) {
        options.allowCredentials = (publicKey.allowCredentials as any[]).map((cred: any) => ({
          id: base64urlToBuffer(cred.id),
          type: cred.type,
          transports: cred.transports,
        }));
      }

      // Prompt user for passkey
      const credential = await navigator.credentials.get({ publicKey: options }) as PublicKeyCredential;
      if (!credential) {
        setError('Passkey authentication was cancelled');
        return;
      }

      // Complete authentication
      const response = await api.passkeyAuthenticateComplete(credential);
      onLogin(response);
    } catch (err: unknown) {
      if (err instanceof Error && err.name === 'NotAllowedError') {
        setError('Passkey authentication was cancelled');
      } else {
        setError(err instanceof Error ? err.message : 'Passkey authentication failed');
      }
    } finally {
      setLoading(false);
    }
  };

  const handlePasswordLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!username.trim() || !password) return;

    try {
      setLoading(true);
      setError('');

      const response = await api.login(username.trim(), password, totpToken || undefined);
      onLogin(response);
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Login failed';
      if (message.includes('TOTP token required') || message.includes('totp_required')) {
        setShowTotp(true);
        setError('Please enter your authenticator code');
      } else {
        setError(message);
      }
    } finally {
      setLoading(false);
    }
  };

  const supportsWebAuthn = typeof window !== 'undefined' && !!window.PublicKeyCredential;

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-950">
      <div className="bg-white dark:bg-gray-900 rounded-xl shadow-lg p-8 max-w-md w-full border border-gray-200 dark:border-gray-800">
        <div className="text-center mb-6">
          <span className="text-4xl mb-4 block" role="img" aria-label="email">📧</span>
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Email Automation</h1>
          <p className="text-gray-500 dark:text-gray-400 mt-2">Sign in to your account</p>
        </div>

        {error && (
          <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-red-700 dark:text-red-400 text-sm" role="alert">
            {error}
          </div>
        )}

        {/* Tabs */}
        <div className="flex mb-6 bg-gray-100 dark:bg-gray-800 rounded-lg p-1">
          {supportsWebAuthn && (
            <button
              onClick={() => { setActiveTab('passkey'); setError(''); }}
              className={`flex-1 py-2 px-3 rounded-md text-sm font-medium transition-colors ${
                activeTab === 'passkey'
                  ? 'bg-white dark:bg-gray-700 text-gray-900 dark:text-white shadow-sm'
                  : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
              }`}
            >
              🔑 Passkey
            </button>
          )}
          <button
            onClick={() => { setActiveTab('password'); setError(''); }}
            className={`flex-1 py-2 px-3 rounded-md text-sm font-medium transition-colors ${
              activeTab === 'password'
                ? 'bg-white dark:bg-gray-700 text-gray-900 dark:text-white shadow-sm'
                : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
            }`}
          >
            🔒 Password
          </button>
        </div>

        {/* Passkey Tab */}
        {activeTab === 'passkey' && supportsWebAuthn && (
          <div>
            <button
              onClick={handlePasskeyLogin}
              disabled={loading}
              className="w-full py-3 bg-blue-600 text-white rounded-lg font-medium hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors flex items-center justify-center gap-2"
            >
              {loading ? (
                <span className="animate-pulse">Waiting for passkey...</span>
              ) : (
                <>
                  <span>🔑</span>
                  <span>Sign in with passkey</span>
                </>
              )}
            </button>
            <p className="text-xs text-gray-400 dark:text-gray-500 text-center mt-3">
              Use your fingerprint, face, or security key
            </p>
          </div>
        )}

        {/* Password Tab */}
        {activeTab === 'password' && (
          <form onSubmit={handlePasswordLogin}>
            <div className="space-y-4">
              <div>
                <label htmlFor="login-username" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Username
                </label>
                <input
                  id="login-username"
                  type="text"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder="Enter your username"
                  className="w-full px-4 py-3 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent outline-none"
                  autoFocus
                  autoComplete="username"
                />
              </div>
              <div>
                <label htmlFor="login-password" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Password
                </label>
                <input
                  id="login-password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="Enter your password"
                  className="w-full px-4 py-3 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent outline-none"
                  autoComplete="current-password"
                />
              </div>

              {showTotp && (
                <div>
                  <label htmlFor="login-totp" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Authenticator Code
                  </label>
                  <input
                    id="login-totp"
                    type="text"
                    value={totpToken}
                    onChange={(e) => setTotpToken(e.target.value.replace(/\D/g, '').slice(0, 6))}
                    placeholder="000000"
                    className="w-full px-4 py-3 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent outline-none text-center text-lg tracking-widest font-mono"
                    autoComplete="one-time-code"
                    inputMode="numeric"
                    maxLength={6}
                    autoFocus
                  />
                </div>
              )}

              <button
                type="submit"
                disabled={loading || !username.trim() || !password}
                className="w-full py-3 bg-blue-600 text-white rounded-lg font-medium hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
              >
                {loading ? 'Signing in...' : 'Sign In'}
              </button>
            </div>
          </form>
        )}

        <div className="mt-6 text-center">
          <button
            onClick={onSwitchToRegister}
            className="text-sm text-blue-600 dark:text-blue-400 hover:underline"
          >
            Don&apos;t have an account? Register
          </button>
        </div>
      </div>
    </div>
  );
}
