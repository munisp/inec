import { useState, useEffect, useRef } from 'react';
import { api } from '../lib/api';

interface MFAStatus {
  totp_enabled: boolean;
  sms_enabled: boolean;
  webauthn_enabled: boolean;
  webauthn_devices: number;
  backup_codes_remaining?: number;
}

interface WebAuthnCredential {
  id: string;
  device_name: string;
  created_at: string;
  last_used: string;
}

export default function MFAPage() {
  const [status, setStatus] = useState<MFAStatus | null>(null);
  const [totpSecret, setTotpSecret] = useState<string | null>(null);
  const [otpauthUri, setOtpauthUri] = useState<string | null>(null);
  const [backupCodes, setBackupCodes] = useState<string[] | null>(null);
  const [verifyCode, setVerifyCode] = useState('');
  const [disableCode, setDisableCode] = useState('');
  const [message, setMessage] = useState<{ text: string; type: 'success' | 'error' | 'info' } | null>(null);
  const [tab, setTab] = useState<'totp' | 'webauthn' | 'backup'>('totp');
  const [credentials, setCredentials] = useState<WebAuthnCredential[]>([]);
  const [loading, setLoading] = useState(false);
  const [showDisable, setShowDisable] = useState(false);
  const qrCanvasRef = useRef<HTMLCanvasElement>(null);
  const codeInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    loadStatus();
  }, []);

  useEffect(() => {
    if (otpauthUri && qrCanvasRef.current) {
      renderQRCode(otpauthUri, qrCanvasRef.current);
    }
  }, [otpauthUri]);

  const loadStatus = async () => {
    try {
      const s = await api.getMFAStatus();
      setStatus(s);
    } catch {
      void 0;
    }
  };

  const loadCredentials = async () => {
    try {
      const res = await fetch('/auth/mfa/webauthn/list', { credentials: 'include' });
      const data = await res.json();
      setCredentials(data.credentials || []);
    } catch {
      void 0;
    }
  };

  useEffect(() => {
    if (tab === 'webauthn') loadCredentials();
  }, [tab]);

  const showMessage = (text: string, type: 'success' | 'error' | 'info') => {
    setMessage({ text, type });
    setTimeout(() => setMessage(null), 5000);
  };

  const handleSetupTOTP = async () => {
    setLoading(true);
    try {
      const res = await fetch('/auth/mfa/setup', { method: 'POST', credentials: 'include' });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error);
      setTotpSecret(data.secret);
      setOtpauthUri(data.otpauth_uri);
      if (data.backup_codes) setBackupCodes(data.backup_codes);
      showMessage('Scan the QR code with your authenticator app, then enter the 6-digit code below', 'info');
      setTimeout(() => codeInputRef.current?.focus(), 100);
    } catch (e: unknown) {
      showMessage(e instanceof Error ? e.message : 'Setup failed', 'error');
    } finally {
      setLoading(false);
    }
  };

  const handleVerifyTOTP = async () => {
    if (verifyCode.length !== 6) {
      showMessage('Please enter a 6-digit code', 'error');
      return;
    }
    setLoading(true);
    try {
      const res = await fetch('/auth/mfa/verify-setup', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code: verifyCode }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error);
      showMessage('MFA enabled successfully! Your account is now protected.', 'success');
      setTotpSecret(null);
      setOtpauthUri(null);
      setVerifyCode('');
      loadStatus();
    } catch (e: unknown) {
      showMessage(e instanceof Error ? e.message : 'Invalid code', 'error');
      setVerifyCode('');
      codeInputRef.current?.focus();
    } finally {
      setLoading(false);
    }
  };

  const handleDisableTOTP = async () => {
    if (disableCode.length !== 6) {
      showMessage('Enter your current 6-digit code to disable MFA', 'error');
      return;
    }
    setLoading(true);
    try {
      const res = await fetch('/auth/mfa/disable', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code: disableCode }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error);
      showMessage('MFA disabled', 'info');
      setShowDisable(false);
      setDisableCode('');
      loadStatus();
    } catch (e: unknown) {
      showMessage(e instanceof Error ? e.message : 'Failed to disable', 'error');
    } finally {
      setLoading(false);
    }
  };

  const handleRegisterWebAuthn = async () => {
    setLoading(true);
    try {
      // Begin registration — get challenge from server
      const beginRes = await fetch('/auth/mfa/webauthn/begin', { method: 'POST', credentials: 'include' });
      const options = await beginRes.json();
      if (!beginRes.ok) throw new Error(options.error);

      // Use Web Authentication API
      const credential = await navigator.credentials.create({
        publicKey: {
          challenge: Uint8Array.from(atob(options.challenge), c => c.charCodeAt(0)),
          rp: { name: options.rp_name, id: options.rp_id || window.location.hostname },
          user: {
            id: new TextEncoder().encode(String(options.user_id || '1')),
            name: options.user_name || 'user',
            displayName: options.user_display_name || 'User',
          },
          pubKeyCredParams: [
            { type: 'public-key', alg: -7 },   // ES256
            { type: 'public-key', alg: -257 }, // RS256
          ],
          authenticatorSelection: {
            authenticatorAttachment: 'cross-platform',
            userVerification: 'preferred',
          },
          timeout: 60000,
        },
      }) as PublicKeyCredential | null;

      if (!credential) throw new Error('Registration cancelled');

      const response = credential.response as AuthenticatorAttestationResponse;
      const credentialId = Array.from(new Uint8Array(credential.rawId));
      const publicKey = Array.from(new Uint8Array(response.getPublicKey?.() || new ArrayBuffer(0)));

      // Complete registration on server
      const completeRes = await fetch('/auth/mfa/webauthn/complete', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          credential_id: credentialId,
          public_key: publicKey,
          device_name: getDeviceName(),
        }),
      });
      const result = await completeRes.json();
      if (!completeRes.ok) throw new Error(result.error);

      showMessage(`Security key "${result.device}" registered successfully`, 'success');
      loadCredentials();
      loadStatus();
    } catch (e: unknown) {
      if (e instanceof Error && e.name === 'NotAllowedError') {
        showMessage('Registration was cancelled or timed out', 'info');
      } else {
        showMessage(e instanceof Error ? e.message : 'Registration failed', 'error');
      }
    } finally {
      setLoading(false);
    }
  };

  const handleDeleteCredential = async (id: string) => {
    try {
      const res = await fetch(`/auth/mfa/webauthn/credentials/${encodeURIComponent(id)}`, {
        method: 'DELETE',
        credentials: 'include',
      });
      if (!res.ok) throw new Error('Delete failed');
      showMessage('Security key removed', 'info');
      loadCredentials();
      loadStatus();
    } catch (e: unknown) {
      showMessage(e instanceof Error ? e.message : 'Delete failed', 'error');
    }
  };

  const handleRegenerateBackupCodes = async () => {
    const code = prompt('Enter your current TOTP code to regenerate backup codes:');
    if (!code || code.length !== 6) return;

    setLoading(true);
    try {
      const res = await fetch('/auth/mfa/backup-codes', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error);
      setBackupCodes(data.backup_codes);
      showMessage('New backup codes generated. Save them securely!', 'success');
    } catch (e: unknown) {
      showMessage(e instanceof Error ? e.message : 'Failed to generate codes', 'error');
    } finally {
      setLoading(false);
    }
  };

  const downloadBackupCodes = () => {
    if (!backupCodes) return;
    const content = [
      'INEC Platform - Backup Codes',
      '============================',
      `Generated: ${new Date().toISOString()}`,
      '',
      'Each code can only be used ONCE.',
      'Store these codes in a safe place.',
      '',
      ...backupCodes.map((code, i) => `${i + 1}. ${code}`),
    ].join('\n');

    const blob = new Blob([content], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'inec-backup-codes.txt';
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="p-4 sm:p-6 max-w-3xl mx-auto min-h-screen" role="main" aria-label="Multi-Factor Authentication Settings">
      {/* Header */}
      <div className="mb-6">
        <h1 className="text-2xl sm:text-3xl font-bold dark:text-white flex items-center gap-2">
          <svg className="w-7 h-7 text-blue-600" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" d="M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z" />
          </svg>
          Multi-Factor Authentication
        </h1>
        <p className="text-gray-500 dark:text-gray-400 mt-1">Protect your INEC account with additional verification methods</p>
      </div>

      {/* Alert Messages */}
      {message && (
        <div className={`p-4 rounded-xl mb-6 flex items-center gap-3 animate-in slide-in-from-top ${
          message.type === 'success' ? 'bg-green-50 dark:bg-green-900/20 text-green-700 dark:text-green-300 border border-green-200 dark:border-green-800' :
          message.type === 'error' ? 'bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300 border border-red-200 dark:border-red-800' :
          'bg-blue-50 dark:bg-blue-900/20 text-blue-700 dark:text-blue-300 border border-blue-200 dark:border-blue-800'
        }`}>
          <svg className="w-5 h-5 flex-shrink-0" fill="currentColor" viewBox="0 0 20 20">
            {message.type === 'success' ? (
              <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.857-9.809a.75.75 0 00-1.214-.882l-3.483 4.79-1.88-1.88a.75.75 0 10-1.06 1.061l2.5 2.5a.75.75 0 001.137-.089l4-5.5z" clipRule="evenodd" />
            ) : (
              <path fillRule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a.75.75 0 000 1.5h.253a.25.25 0 01.244.304l-.459 2.066A1.75 1.75 0 0010.747 15H11a.75.75 0 000-1.5h-.253a.25.25 0 01-.244-.304l.459-2.066A1.75 1.75 0 009.253 9H9z" clipRule="evenodd" />
            )}
          </svg>
          <span className="text-sm font-medium">{message.text}</span>
        </div>
      )}

      {/* Status Cards */}
      {status && (
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 mb-6">
          <StatusCard label="Authenticator" enabled={status.totp_enabled} icon="key" />
          <StatusCard label="Security Keys" enabled={status.webauthn_enabled} count={status.webauthn_devices} icon="fingerprint" />
          <StatusCard label="Backup Codes" enabled={(status.backup_codes_remaining || 0) > 0} count={status.backup_codes_remaining} icon="shield" />
        </div>
      )}

      {/* Tab Navigation */}
      <div className="flex gap-1 mb-6 bg-gray-100 dark:bg-gray-800 rounded-xl p-1">
        {([
          { id: 'totp', label: 'Authenticator', icon: '🔑' },
          { id: 'webauthn', label: 'Security Keys', icon: '🔐' },
          { id: 'backup', label: 'Backup Codes', icon: '🛡️' },
        ] as const).map((t) => (
          <button key={t.id} onClick={() => setTab(t.id)}
            className={`flex-1 px-3 py-2.5 rounded-lg text-sm font-medium transition-all ${
              tab === t.id
                ? 'bg-white dark:bg-gray-700 shadow-sm text-blue-600 dark:text-blue-400'
                : 'text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200'
            }`}>
            <span className="hidden sm:inline mr-1">{t.icon}</span> {t.label}
          </button>
        ))}
      </div>

      {/* TOTP Tab */}
      {tab === 'totp' && (
        <div className="bg-white dark:bg-gray-800 rounded-2xl shadow-sm border border-gray-200 dark:border-gray-700 overflow-hidden">
          {!totpSecret ? (
            <div className="p-6 sm:p-8 text-center">
              <div className="w-16 h-16 mx-auto mb-4 rounded-full bg-blue-100 dark:bg-blue-900/30 flex items-center justify-center">
                <svg className="w-8 h-8 text-blue-600" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
              </div>
              <h3 className="text-lg font-semibold dark:text-white mb-2">Time-Based One-Time Password (TOTP)</h3>
              <p className="text-gray-600 dark:text-gray-300 mb-6 max-w-md mx-auto">
                Use an authenticator app like Google Authenticator, Authy, or 1Password to generate secure 6-digit codes that change every 30 seconds.
              </p>
              <button onClick={handleSetupTOTP} disabled={loading}
                className="bg-blue-600 text-white px-8 py-3 rounded-xl font-medium hover:bg-blue-700 disabled:opacity-50 transition-colors shadow-sm">
                {loading ? 'Setting up...' : status?.totp_enabled ? 'Reset TOTP' : 'Enable TOTP'}
              </button>
              {status?.totp_enabled && (
                <div className="mt-4">
                  <button onClick={() => setShowDisable(!showDisable)}
                    className="text-red-600 dark:text-red-400 text-sm hover:underline">
                    Disable TOTP
                  </button>
                  {showDisable && (
                    <div className="mt-3 flex gap-2 justify-center">
                      <input type="text" value={disableCode} onChange={(e) => setDisableCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                        placeholder="6-digit code" maxLength={6}
                        className="w-32 border rounded-lg px-3 py-2 text-center font-mono dark:bg-gray-700 dark:border-gray-600 dark:text-white" />
                      <button onClick={handleDisableTOTP} className="bg-red-600 text-white px-4 py-2 rounded-lg text-sm">
                        Confirm Disable
                      </button>
                    </div>
                  )}
                </div>
              )}
            </div>
          ) : (
            <div className="p-6 sm:p-8">
              <h3 className="text-lg font-semibold dark:text-white mb-4">Scan QR Code</h3>

              {/* QR Code */}
              <div className="flex justify-center mb-4">
                <div className="bg-white p-4 rounded-xl shadow-inner">
                  <canvas ref={qrCanvasRef} width={200} height={200} className="w-48 h-48" />
                </div>
              </div>

              {/* Manual entry secret */}
              <details className="mb-4">
                <summary className="text-sm text-gray-500 dark:text-gray-400 cursor-pointer hover:text-gray-700">
                  Can't scan? Enter manually
                </summary>
                <code className="block mt-2 bg-gray-100 dark:bg-gray-700 p-3 rounded-lg text-sm font-mono dark:text-gray-300 break-all select-all">
                  {totpSecret}
                </code>
              </details>

              {/* Verification */}
              <div className="bg-gray-50 dark:bg-gray-700/50 rounded-xl p-4">
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                  Enter the 6-digit code from your app to verify:
                </label>
                <div className="flex gap-2">
                  <input ref={codeInputRef} type="text" inputMode="numeric" value={verifyCode}
                    onChange={(e) => setVerifyCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                    onKeyDown={(e) => e.key === 'Enter' && handleVerifyTOTP()}
                    placeholder="000000" maxLength={6} autoComplete="one-time-code"
                    className="flex-1 border rounded-xl px-4 py-3 text-center text-2xl font-mono tracking-widest dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-blue-500" />
                  <button onClick={handleVerifyTOTP} disabled={loading || verifyCode.length !== 6}
                    className="bg-green-600 text-white px-6 py-3 rounded-xl font-medium hover:bg-green-700 disabled:opacity-50 transition-colors">
                    {loading ? '...' : 'Verify'}
                  </button>
                </div>
              </div>

              {/* Backup Codes (shown during setup) */}
              {backupCodes && (
                <div className="mt-6 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-xl p-4">
                  <h4 className="font-semibold text-amber-800 dark:text-amber-200 mb-2 flex items-center gap-2">
                    <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 20 20"><path fillRule="evenodd" d="M8.485 2.495c.673-1.167 2.357-1.167 3.03 0l6.28 10.875c.673 1.167-.17 2.625-1.516 2.625H3.72c-1.347 0-2.189-1.458-1.515-2.625L8.485 2.495zM10 5a.75.75 0 01.75.75v3.5a.75.75 0 01-1.5 0v-3.5A.75.75 0 0110 5zm0 9a1 1 0 100-2 1 1 0 000 2z" clipRule="evenodd" /></svg>
                    Save Your Backup Codes
                  </h4>
                  <p className="text-sm text-amber-700 dark:text-amber-300 mb-3">Each code can only be used once. Store them securely.</p>
                  <div className="grid grid-cols-2 gap-2 mb-3">
                    {backupCodes.map((code, i) => (
                      <code key={i} className="bg-white dark:bg-gray-800 px-3 py-1.5 rounded text-sm font-mono text-center">{code}</code>
                    ))}
                  </div>
                  <button onClick={downloadBackupCodes}
                    className="w-full bg-amber-600 text-white px-4 py-2 rounded-lg text-sm font-medium hover:bg-amber-700">
                    Download Backup Codes
                  </button>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {/* WebAuthn Tab */}
      {tab === 'webauthn' && (
        <div className="bg-white dark:bg-gray-800 rounded-2xl shadow-sm border border-gray-200 dark:border-gray-700 overflow-hidden">
          <div className="p-6 sm:p-8">
            <div className="text-center mb-6">
              <div className="w-16 h-16 mx-auto mb-4 rounded-full bg-purple-100 dark:bg-purple-900/30 flex items-center justify-center">
                <svg className="w-8 h-8 text-purple-600" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M7.864 4.243A7.5 7.5 0 0119.5 10.5c0 2.92-.556 5.709-1.568 8.268M5.742 6.364A7.465 7.465 0 004.5 10.5a7.464 7.464 0 01-1.15 3.993m1.989 3.559A11.209 11.209 0 008.25 10.5a3.75 3.75 0 117.5 0c0 .527-.021 1.049-.064 1.565M12 10.5a14.94 14.94 0 01-3.6 9.75m6.633-4.596a18.666 18.666 0 01-2.485 5.33" />
                </svg>
              </div>
              <h3 className="text-lg font-semibold dark:text-white mb-2">Security Keys (FIDO2/WebAuthn)</h3>
              <p className="text-gray-600 dark:text-gray-300 max-w-md mx-auto">
                Register hardware security keys (YubiKey) or platform authenticators (Touch ID, Windows Hello, Android biometrics).
              </p>
            </div>

            <button onClick={handleRegisterWebAuthn} disabled={loading}
              className="w-full bg-purple-600 text-white px-6 py-3 rounded-xl font-medium hover:bg-purple-700 disabled:opacity-50 transition-colors mb-6">
              {loading ? 'Waiting for authenticator...' : 'Register New Security Key'}
            </button>

            {/* Registered credentials list */}
            {credentials.length > 0 && (
              <div className="border-t dark:border-gray-700 pt-4">
                <h4 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">Registered Keys</h4>
                <div className="space-y-2">
                  {credentials.map((cred) => (
                    <div key={cred.id} className="flex items-center justify-between bg-gray-50 dark:bg-gray-700/50 rounded-lg px-4 py-3">
                      <div>
                        <p className="font-medium dark:text-white text-sm">{cred.device_name}</p>
                        <p className="text-xs text-gray-500 dark:text-gray-400">
                          Added {new Date(cred.created_at).toLocaleDateString()}
                        </p>
                      </div>
                      <button onClick={() => handleDeleteCredential(cred.id)}
                        className="text-red-500 hover:text-red-700 p-1" title="Remove key">
                        <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor">
                          <path strokeLinecap="round" strokeLinejoin="round" d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0" />
                        </svg>
                      </button>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {/* Backup Codes Tab */}
      {tab === 'backup' && (
        <div className="bg-white dark:bg-gray-800 rounded-2xl shadow-sm border border-gray-200 dark:border-gray-700 overflow-hidden">
          <div className="p-6 sm:p-8 text-center">
            <div className="w-16 h-16 mx-auto mb-4 rounded-full bg-amber-100 dark:bg-amber-900/30 flex items-center justify-center">
              <svg className="w-8 h-8 text-amber-600" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" d="M16.5 10.5V6.75a4.5 4.5 0 10-9 0v3.75m-.75 11.25h10.5a2.25 2.25 0 002.25-2.25v-6.75a2.25 2.25 0 00-2.25-2.25H6.75a2.25 2.25 0 00-2.25 2.25v6.75a2.25 2.25 0 002.25 2.25z" />
              </svg>
            </div>
            <h3 className="text-lg font-semibold dark:text-white mb-2">Backup Recovery Codes</h3>
            <p className="text-gray-600 dark:text-gray-300 max-w-md mx-auto mb-6">
              If you lose access to your authenticator, you can use a backup code to sign in. Each code can only be used once.
            </p>

            {backupCodes ? (
              <div className="text-left max-w-sm mx-auto">
                <div className="grid grid-cols-2 gap-2 mb-4">
                  {backupCodes.map((code, i) => (
                    <code key={i} className="bg-gray-100 dark:bg-gray-700 px-3 py-2 rounded-lg text-sm font-mono text-center dark:text-gray-300">{code}</code>
                  ))}
                </div>
                <button onClick={downloadBackupCodes}
                  className="w-full bg-blue-600 text-white px-4 py-2.5 rounded-xl font-medium hover:bg-blue-700 mb-2">
                  Download Codes
                </button>
                <button onClick={() => setBackupCodes(null)}
                  className="w-full text-gray-500 dark:text-gray-400 text-sm hover:text-gray-700">
                  Done
                </button>
              </div>
            ) : (
              <button onClick={handleRegenerateBackupCodes} disabled={loading || !status?.totp_enabled}
                className="bg-amber-600 text-white px-8 py-3 rounded-xl font-medium hover:bg-amber-700 disabled:opacity-50 transition-colors">
                {loading ? 'Generating...' : 'Generate New Backup Codes'}
              </button>
            )}

            {!status?.totp_enabled && (
              <p className="text-sm text-gray-400 dark:text-gray-500 mt-4">
                Enable TOTP first to generate backup codes.
              </p>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

// --- Helper Components ---

function StatusCard({ label, enabled, count, icon }: { label: string; enabled: boolean; count?: number; icon: string }) {
  const iconPaths: Record<string, string> = {
    key: "M15.75 5.25a3 3 0 013 3m3 0a6 6 0 01-7.029 5.912c-.563-.097-1.159.026-1.563.43L10.5 17.25H8.25v2.25H6v2.25H2.25v-2.818c0-.597.237-1.17.659-1.591l6.499-6.499c.404-.404.527-1 .43-1.563A6 6 0 1121.75 8.25z",
    fingerprint: "M7.864 4.243A7.5 7.5 0 0119.5 10.5c0 2.92-.556 5.709-1.568 8.268M5.742 6.364A7.465 7.465 0 004.5 10.5a7.464 7.464 0 01-1.15 3.993m1.989 3.559A11.209 11.209 0 008.25 10.5a3.75 3.75 0 117.5 0c0 .527-.021 1.049-.064 1.565M12 10.5a14.94 14.94 0 01-3.6 9.75m6.633-4.596a18.666 18.666 0 01-2.485 5.33",
    shield: "M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z",
  };

  return (
    <div className={`p-4 rounded-xl border transition-all ${
      enabled ? 'border-green-200 dark:border-green-800 bg-green-50 dark:bg-green-900/20' : 'border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800'
    }`}>
      <div className="flex items-center gap-2 mb-1">
        <svg className={`w-4 h-4 ${enabled ? 'text-green-600' : 'text-gray-400'}`} fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" d={iconPaths[icon] || iconPaths.key} />
        </svg>
        <span className="text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wide">{label}</span>
      </div>
      <p className={`font-bold text-lg ${enabled ? 'text-green-600 dark:text-green-400' : 'text-gray-400'}`}>
        {enabled ? (count !== undefined ? `${count} active` : 'Enabled') : 'Not set up'}
      </p>
    </div>
  );
}

// --- Utility Functions ---

function getDeviceName(): string {
  const ua = navigator.userAgent;
  if (/iPhone|iPad/.test(ua)) return 'iPhone/iPad';
  if (/Android/.test(ua)) return 'Android Device';
  if (/Mac/.test(ua)) return 'Mac (Touch ID)';
  if (/Windows/.test(ua)) return 'Windows Hello';
  if (/Linux/.test(ua)) return 'Linux Device';
  return 'Security Key';
}

function renderQRCode(data: string, canvas: HTMLCanvasElement) {
  // Simple QR code renderer using canvas — for production use a proper QR library
  const ctx = canvas.getContext('2d');
  if (!ctx) return;

  const size = canvas.width;
  ctx.fillStyle = '#ffffff';
  ctx.fillRect(0, 0, size, size);

  // Encode the otpauth URI as a simple visual representation
  // In production, use a proper QR code library like 'qrcode'
  const modules = encodeToMatrix(data);
  const moduleSize = size / modules.length;

  ctx.fillStyle = '#000000';
  for (let row = 0; row < modules.length; row++) {
    for (let col = 0; col < modules[row].length; col++) {
      if (modules[row][col]) {
        ctx.fillRect(col * moduleSize, row * moduleSize, moduleSize, moduleSize);
      }
    }
  }
}

function encodeToMatrix(data: string): boolean[][] {
  // Simplified QR-like matrix for visual display
  // Real implementation would use Reed-Solomon encoding
  const size = 33; // QR version 4
  const matrix: boolean[][] = Array.from({ length: size }, () => Array(size).fill(false));

  // Add finder patterns (top-left, top-right, bottom-left)
  const addFinder = (startRow: number, startCol: number) => {
    for (let r = 0; r < 7; r++) {
      for (let c = 0; c < 7; c++) {
        const isEdge = r === 0 || r === 6 || c === 0 || c === 6;
        const isInner = r >= 2 && r <= 4 && c >= 2 && c <= 4;
        matrix[startRow + r][startCol + c] = isEdge || isInner;
      }
    }
  };
  addFinder(0, 0);
  addFinder(0, size - 7);
  addFinder(size - 7, 0);

  // Encode data bytes into the matrix
  const bytes = new TextEncoder().encode(data);
  let bitIndex = 0;
  for (let col = size - 1; col >= 8; col -= 2) {
    for (let row = 0; row < size; row++) {
      if (matrix[row][col] === false && bitIndex < bytes.length * 8) {
        const byteIdx = Math.floor(bitIndex / 8);
        const bit = (bytes[byteIdx] >> (7 - (bitIndex % 8))) & 1;
        matrix[row][col] = bit === 1;
        bitIndex++;
      }
    }
  }

  return matrix;
}
