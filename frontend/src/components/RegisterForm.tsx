'use client';

import { useState } from 'react';
import api, { AuthResponse, TOTPSetupResponse, base64urlToBuffer } from '@/lib/api';

interface RegisterFormProps {
  onComplete: (response: AuthResponse) => void;
  onSwitchToLogin: () => void;
}

type Step = 'register' | 'totp-setup' | 'totp-verify' | 'passkey-setup' | 'done';

export default function RegisterForm({ onComplete, onSwitchToLogin }: RegisterFormProps) {
  const [step, setStep] = useState<Step>('register');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  // TOTP state
  const [totpData, setTotpData] = useState<TOTPSetupResponse | null>(null);
  const [totpToken, setTotpToken] = useState('');
  const [showManualEntry, setShowManualEntry] = useState(false);

  // Auth response stored after registration
  const [authResponse, setAuthResponse] = useState<AuthResponse | null>(null);

  const handleRegister = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!username.trim() || !password || !confirmPassword) return;

    if (password !== confirmPassword) {
      setError('Passwords do not match');
      return;
    }

    try {
      setLoading(true);
      setError('');

      const response = await api.register(username.trim(), password);
      setAuthResponse(response);
      api.setToken(response.token);

      // Move to TOTP setup
      setStep('totp-setup');
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Registration failed');
    } finally {
      setLoading(false);
    }
  };

  const handleTOTPSetup = async () => {
    try {
      setLoading(true);
      setError('');

      const data = await api.totpSetup();
      setTotpData(data);
      setStep('totp-verify');
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to setup TOTP');
    } finally {
      setLoading(false);
    }
  };

  const handleTOTPVerify = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!totpToken || totpToken.length !== 6) return;

    try {
      setLoading(true);
      setError('');

      await api.totpVerify(totpToken);
      setStep('passkey-setup');
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Invalid code, please try again');
    } finally {
      setLoading(false);
    }
  };

  const handlePasskeySetup = async () => {
    try {
      setLoading(true);
      setError('');

      const options = await api.passkeyRegisterBegin();
      const publicKey = options as any;

      const createOptions: PublicKeyCredentialCreationOptions = {
        challenge: base64urlToBuffer(publicKey.publicKey?.challenge || publicKey.challenge),
        rp: publicKey.publicKey?.rp || publicKey.rp,
        user: {
          ...(publicKey.publicKey?.user || publicKey.user),
          id: base64urlToBuffer((publicKey.publicKey?.user || publicKey.user).id),
        },
        pubKeyCredParams: publicKey.publicKey?.pubKeyCredParams || publicKey.pubKeyCredParams,
        timeout: publicKey.publicKey?.timeout || publicKey.timeout || 60000,
        attestation: publicKey.publicKey?.attestation || publicKey.attestation || 'none',
        authenticatorSelection: publicKey.publicKey?.authenticatorSelection || publicKey.authenticatorSelection,
      };

      if (publicKey.publicKey?.excludeCredentials || publicKey.excludeCredentials) {
        const excl = publicKey.publicKey?.excludeCredentials || publicKey.excludeCredentials;
        createOptions.excludeCredentials = excl.map((cred: any) => ({
          id: base64urlToBuffer(cred.id),
          type: cred.type,
          transports: cred.transports,
        }));
      }

      const credential = await navigator.credentials.create({ publicKey: createOptions }) as PublicKeyCredential;
      if (!credential) {
        setError('Passkey registration was cancelled');
        return;
      }

      await api.passkeyRegisterComplete(credential, 'Primary passkey');
      if (authResponse) onComplete(authResponse);
    } catch (err: unknown) {
      if (err instanceof Error && err.name === 'NotAllowedError') {
        setError('Passkey registration was cancelled');
      } else {
        setError(err instanceof Error ? err.message : 'Passkey registration failed');
      }
    } finally {
      setLoading(false);
    }
  };

  const handleSkipPasskey = () => {
    if (authResponse) onComplete(authResponse);
  };

  const handleSkipTOTP = () => {
    setStep('passkey-setup');
  };

  const supportsWebAuthn = typeof window !== 'undefined' && !!window.PublicKeyCredential;

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-950">
      <div className="bg-white dark:bg-gray-900 rounded-xl shadow-lg p-8 max-w-md w-full border border-gray-200 dark:border-gray-800">
        <div className="text-center mb-6">
          <span className="text-4xl mb-4 block" role="img" aria-label="email">📧</span>
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white">
            {step === 'register' && 'Create Account'}
            {step === 'totp-setup' && 'Setup Authenticator'}
            {step === 'totp-verify' && 'Verify Authenticator'}
            {step === 'passkey-setup' && 'Add Passkey'}
          </h1>
          <p className="text-gray-500 dark:text-gray-400 mt-2">
            {step === 'register' && 'Set up your account'}
            {step === 'totp-setup' && 'Add two-factor authentication for extra security'}
            {step === 'totp-verify' && 'Scan the QR code and enter the verification code'}
            {step === 'passkey-setup' && 'Register a passkey for passwordless sign-in'}
          </p>
        </div>

        {/* Progress indicator */}
        <div className="flex items-center justify-center gap-2 mb-6">
          {['register', 'totp-setup', 'passkey-setup'].map((s, i) => (
            <div key={s} className="flex items-center gap-2">
              <div className={`w-2.5 h-2.5 rounded-full ${
                step === s || (s === 'totp-setup' && step === 'totp-verify')
                  ? 'bg-blue-600'
                  : i < ['register', 'totp-setup', 'passkey-setup'].indexOf(step === 'totp-verify' ? 'totp-setup' : step)
                    ? 'bg-green-500'
                    : 'bg-gray-300 dark:bg-gray-700'
              }`} />
              {i < 2 && <div className="w-8 h-0.5 bg-gray-200 dark:bg-gray-700" />}
            </div>
          ))}
        </div>

        {error && (
          <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-red-700 dark:text-red-400 text-sm" role="alert">
            {error}
          </div>
        )}

        {/* Step 1: Register */}
        {step === 'register' && (
          <form onSubmit={handleRegister}>
            <div className="space-y-4">
              <div>
                <label htmlFor="reg-username" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Username
                </label>
                <input
                  id="reg-username"
                  type="text"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder="Choose a username"
                  className="w-full px-4 py-3 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent outline-none"
                  autoFocus
                  autoComplete="username"
                  minLength={3}
                  maxLength={64}
                />
              </div>
              <div>
                <label htmlFor="reg-password" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Password
                </label>
                <input
                  id="reg-password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="Min 12 chars, mixed case, digit, special"
                  className="w-full px-4 py-3 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent outline-none"
                  autoComplete="new-password"
                  minLength={12}
                />
              </div>
              <div>
                <label htmlFor="reg-confirm" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Confirm Password
                </label>
                <input
                  id="reg-confirm"
                  type="password"
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  placeholder="Repeat your password"
                  className="w-full px-4 py-3 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent outline-none"
                  autoComplete="new-password"
                />
              </div>
              <button
                type="submit"
                disabled={loading || !username.trim() || !password || !confirmPassword}
                className="w-full py-3 bg-blue-600 text-white rounded-lg font-medium hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
              >
                {loading ? 'Creating account...' : 'Create Account'}
              </button>
            </div>
          </form>
        )}

        {/* Step 2a: TOTP Setup prompt */}
        {step === 'totp-setup' && (
          <div className="space-y-4">
            <div className="p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
              <p className="text-sm text-blue-700 dark:text-blue-400">
                Two-factor authentication adds an extra layer of security. You&apos;ll need an authenticator app like Google Authenticator, Authy, or 1Password.
              </p>
            </div>
            <button
              onClick={handleTOTPSetup}
              disabled={loading}
              className="w-full py-3 bg-blue-600 text-white rounded-lg font-medium hover:bg-blue-700 disabled:opacity-50 transition-colors"
            >
              {loading ? 'Setting up...' : 'Setup Authenticator'}
            </button>
            <button
              onClick={handleSkipTOTP}
              className="w-full py-3 bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400 rounded-lg font-medium hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors"
            >
              Skip for now
            </button>
          </div>
        )}

        {/* Step 2b: TOTP Verify */}
        {step === 'totp-verify' && totpData && (
          <form onSubmit={handleTOTPVerify}>
            <div className="space-y-4">
              <div className="flex justify-center">
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img src={totpData.qr_code} alt="TOTP QR Code" className="w-48 h-48 rounded-lg border border-gray-200 dark:border-gray-700" />
              </div>

              <button
                type="button"
                onClick={() => setShowManualEntry(!showManualEntry)}
                className="w-full text-sm text-blue-600 dark:text-blue-400 hover:underline"
              >
                {showManualEntry ? 'Hide manual entry key' : 'Can\'t scan? Enter key manually'}
              </button>

              {showManualEntry && (
                <div className="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
                  <p className="text-xs text-gray-500 dark:text-gray-400 mb-1">Manual entry key:</p>
                  <code className="text-sm font-mono text-gray-900 dark:text-white break-all select-all">
                    {totpData.secret}
                  </code>
                </div>
              )}

              <div>
                <label htmlFor="totp-code" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Verification Code
                </label>
                <input
                  id="totp-code"
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

              <button
                type="submit"
                disabled={loading || totpToken.length !== 6}
                className="w-full py-3 bg-blue-600 text-white rounded-lg font-medium hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
              >
                {loading ? 'Verifying...' : 'Verify & Enable'}
              </button>
            </div>
          </form>
        )}

        {/* Step 3: Passkey Setup */}
        {step === 'passkey-setup' && (
          <div className="space-y-4">
            {supportsWebAuthn ? (
              <>
                <div className="p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
                  <p className="text-sm text-blue-700 dark:text-blue-400">
                    Passkeys let you sign in with your fingerprint, face, or security key. No password needed.
                  </p>
                </div>
                <button
                  onClick={handlePasskeySetup}
                  disabled={loading}
                  className="w-full py-3 bg-blue-600 text-white rounded-lg font-medium hover:bg-blue-700 disabled:opacity-50 transition-colors flex items-center justify-center gap-2"
                >
                  {loading ? (
                    <span className="animate-pulse">Waiting for passkey...</span>
                  ) : (
                    <>
                      <span>🔑</span>
                      <span>Register Passkey</span>
                    </>
                  )}
                </button>
              </>
            ) : (
              <div className="p-4 bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded-lg">
                <p className="text-sm text-yellow-700 dark:text-yellow-400">
                  Your browser doesn&apos;t support passkeys. You can add one later from a supported browser.
                </p>
              </div>
            )}
            <button
              onClick={handleSkipPasskey}
              className="w-full py-3 bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400 rounded-lg font-medium hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors"
            >
              {supportsWebAuthn ? 'Skip for now' : 'Continue to dashboard'}
            </button>
          </div>
        )}

        {/* Back to login link (only on register step) */}
        {step === 'register' && (
          <div className="mt-6 text-center">
            <button
              onClick={onSwitchToLogin}
              className="text-sm text-blue-600 dark:text-blue-400 hover:underline"
            >
              Already have an account? Sign in
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
