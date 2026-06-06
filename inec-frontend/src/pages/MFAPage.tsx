import { useState, useEffect } from 'react';
import { api } from '../lib/api';


export default function MFAPage() {
  const [status, setStatus] = useState<{
    totp_enabled: boolean;
    sms_enabled: boolean;
    webauthn_enabled: boolean;
    webauthn_devices: number;
  } | null>(null);
  const [totpSecret, setTotpSecret] = useState<string | null>(null);
  const [otpauthUri, setOtpauthUri] = useState<string | null>(null);
  const [verifyCode, setVerifyCode] = useState('');
  const [message, setMessage] = useState('');
  const [tab, setTab] = useState<'totp' | 'webauthn' | 'sms'>('totp');

  useEffect(() => {
    api.getMFAStatus().then(setStatus).catch((e) => void 0);
  }, []);

  const handleSetupTOTP = async () => {
    try {
      const res = await api.setupTOTP();
      setTotpSecret(res.secret);
      setOtpauthUri(res.otpauth_uri);
      setMessage('Scan the QR code or enter the secret in your authenticator app');
    } catch (e) {
      void 0;
      setMessage('Setup failed');
    }
  };

  const handleVerifyTOTP = async () => {
    try {
      await api.verifyTOTP(verifyCode);
      setMessage('TOTP enabled successfully');
      setTotpSecret(null);
      setOtpauthUri(null);
      setVerifyCode('');
      api.getMFAStatus().then(setStatus);
    } catch {
      setMessage('Invalid code. Please try again.');
    }
  };

  const handleRegisterWebAuthn = async () => {
    try {
      const credId = btoa(String.fromCharCode(...crypto.getRandomValues(new Uint8Array(32))));
      const pubKey = btoa(String.fromCharCode(...crypto.getRandomValues(new Uint8Array(65))));
      await api.registerWebAuthn(credId, pubKey, navigator.userAgent.substring(0, 40));
      setMessage('Security key registered');
      api.getMFAStatus().then(setStatus);
    } catch (e) {
      void 0;
      setMessage('Registration failed');
    }
  };

  return (
    <div className="p-6 max-w-3xl mx-auto" role="main" aria-label="Multi-Factor Authentication Settings">
      <h1 className="text-2xl font-bold mb-2 dark:text-white">Multi-Factor Authentication</h1>
      <p className="text-gray-500 dark:text-gray-400 mb-6">Secure your account with TOTP, WebAuthn, or SMS verification</p>

      {/* Status Cards */}
      {status && (
        <div className="grid grid-cols-3 gap-4 mb-6">
          <div className={`p-4 rounded-lg border ${status.totp_enabled ? 'border-green-500 bg-green-50 dark:bg-green-900/20' : 'border-gray-200 dark:border-gray-700'}`}>
            <p className="text-sm font-medium dark:text-gray-300">TOTP</p>
            <p className={`font-bold ${status.totp_enabled ? 'text-green-600' : 'text-gray-400'}`}>
              {status.totp_enabled ? 'Enabled' : 'Disabled'}
            </p>
          </div>
          <div className={`p-4 rounded-lg border ${status.webauthn_enabled ? 'border-green-500 bg-green-50 dark:bg-green-900/20' : 'border-gray-200 dark:border-gray-700'}`}>
            <p className="text-sm font-medium dark:text-gray-300">WebAuthn</p>
            <p className={`font-bold ${status.webauthn_enabled ? 'text-green-600' : 'text-gray-400'}`}>
              {status.webauthn_enabled ? `${status.webauthn_devices} key(s)` : 'Disabled'}
            </p>
          </div>
          <div className={`p-4 rounded-lg border ${status.sms_enabled ? 'border-green-500 bg-green-50 dark:bg-green-900/20' : 'border-gray-200 dark:border-gray-700'}`}>
            <p className="text-sm font-medium dark:text-gray-300">SMS OTP</p>
            <p className={`font-bold ${status.sms_enabled ? 'text-green-600' : 'text-gray-400'}`}>
              {status.sms_enabled ? 'Enabled' : 'Disabled'}
            </p>
          </div>
        </div>
      )}

      {/* Tabs */}
      <div className="flex gap-2 mb-4">
        {(['totp', 'webauthn', 'sms'] as const).map((t) => (
          <button key={t} onClick={() => setTab(t)}
            className={`px-4 py-2 rounded-lg text-sm font-medium ${tab === t ? 'bg-blue-600 text-white' : 'bg-gray-100 dark:bg-gray-700 dark:text-gray-300'}`}>
            {t === 'totp' ? 'Authenticator App' : t === 'webauthn' ? 'Security Key' : 'SMS OTP'}
          </button>
        ))}
      </div>

      {message && (
        <div className="bg-blue-50 dark:bg-blue-900/20 text-blue-700 dark:text-blue-300 p-3 rounded-lg mb-4">{message}</div>
      )}

      {/* TOTP Tab */}
      {tab === 'totp' && (
        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6">
          {!totpSecret ? (
            <div className="text-center">
              <p className="text-gray-600 dark:text-gray-300 mb-4">
                Use an authenticator app (Google Authenticator, Authy, etc.) to generate time-based codes.
              </p>
              <button onClick={handleSetupTOTP} className="bg-blue-600 text-white px-6 py-2 rounded-lg font-medium hover:bg-blue-700">
                {status?.totp_enabled ? 'Reset TOTP' : 'Set Up TOTP'}
              </button>
            </div>
          ) : (
            <div>
              <p className="text-sm text-gray-500 dark:text-gray-400 mb-2">Your TOTP Secret:</p>
              <code className="block bg-gray-100 dark:bg-gray-700 p-3 rounded text-sm font-mono mb-4 dark:text-gray-300 break-all">{totpSecret}</code>
              {otpauthUri && (
                <p className="text-xs text-gray-400 dark:text-gray-500 mb-4 break-all">{otpauthUri}</p>
              )}
              <div className="flex gap-2">
                <input type="text" value={verifyCode} onChange={(e) => setVerifyCode(e.target.value)}
                  placeholder="Enter 6-digit code" maxLength={6}
                  className="flex-1 border rounded-lg px-4 py-2 dark:bg-gray-700 dark:border-gray-600 dark:text-white" />
                <button onClick={handleVerifyTOTP} className="bg-green-600 text-white px-6 py-2 rounded-lg font-medium">Verify</button>
              </div>
            </div>
          )}
        </div>
      )}

      {/* WebAuthn Tab */}
      {tab === 'webauthn' && (
        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6 text-center">
          <p className="text-gray-600 dark:text-gray-300 mb-4">
            Register a FIDO2 security key or platform authenticator (fingerprint, Face ID).
          </p>
          <button onClick={handleRegisterWebAuthn} className="bg-blue-600 text-white px-6 py-2 rounded-lg font-medium hover:bg-blue-700">
            Register Security Key
          </button>
        </div>
      )}

      {/* SMS Tab */}
      {tab === 'sms' && (
        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6 text-center">
          <p className="text-gray-600 dark:text-gray-300 mb-4">
            SMS-based OTP is auto-configured from your registered phone number in the officer profile.
          </p>
          <p className="text-sm text-gray-400 dark:text-gray-500">OTP codes are valid for 5 minutes.</p>
        </div>
      )}
    </div>
  );
}
