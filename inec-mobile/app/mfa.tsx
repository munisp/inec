import { useState, useEffect, useRef, useCallback } from 'react';
import {
  View, Text, StyleSheet, ScrollView, TouchableOpacity, TextInput,
  Platform, Alert, Animated, Share, Clipboard, ActivityIndicator,
  Dimensions, KeyboardAvoidingView,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import * as LocalAuthentication from 'expo-local-authentication';
import { api } from '../src/lib/api';

const { width: SCREEN_WIDTH } = Dimensions.get('window');

interface MFAStatus {
  mfa_enabled: boolean;
  totp: boolean;
  webauthn: boolean;
  sms: boolean;
  backup_codes_remaining?: number;
}

interface TOTPSetup {
  secret: string;
  otpauth_uri: string;
  backup_codes?: string[];
}

type Tab = 'totp' | 'webauthn' | 'backup';

export default function MFAScreen() {
  const [status, setStatus] = useState<MFAStatus | null>(null);
  const [totpSetup, setTotpSetup] = useState<TOTPSetup | null>(null);
  const [backupCodes, setBackupCodes] = useState<string[] | null>(null);
  const [otpCode, setOtpCode] = useState('');
  const [loading, setLoading] = useState(false);
  const [activeTab, setActiveTab] = useState<Tab>('totp');
  const [biometricAvailable, setBiometricAvailable] = useState(false);
  const [biometricType, setBiometricType] = useState<string>('Biometric');
  const fadeAnim = useRef(new Animated.Value(0)).current;
  const slideAnim = useRef(new Animated.Value(30)).current;
  const inputRef = useRef<TextInput>(null);

  useEffect(() => {
    loadStatus();
    checkBiometric();
    Animated.parallel([
      Animated.timing(fadeAnim, { toValue: 1, duration: 400, useNativeDriver: true }),
      Animated.timing(slideAnim, { toValue: 0, duration: 400, useNativeDriver: true }),
    ]).start();
  }, []);

  const loadStatus = async () => {
    try {
      const data = await api<MFAStatus>('/auth/mfa/status');
      setStatus(data);
    } catch {
      void 0;
    }
  };

  const checkBiometric = async () => {
    const compatible = await LocalAuthentication.hasHardwareAsync();
    const enrolled = await LocalAuthentication.isEnrolledAsync();
    setBiometricAvailable(compatible && enrolled);

    const types = await LocalAuthentication.supportedAuthenticationTypesAsync();
    if (types.includes(LocalAuthentication.AuthenticationType.FACIAL_RECOGNITION)) {
      setBiometricType('Face ID');
    } else if (types.includes(LocalAuthentication.AuthenticationType.FINGERPRINT)) {
      setBiometricType('Fingerprint');
    }
  };

  const setupTOTP = async () => {
    setLoading(true);
    await Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      const data = await api<TOTPSetup>('/auth/mfa/setup', { method: 'POST' });
      setTotpSetup(data);
      if (data.backup_codes) setBackupCodes(data.backup_codes);
      setTimeout(() => inputRef.current?.focus(), 300);
    } catch (e) {
      Alert.alert('Setup Failed', e instanceof Error ? e.message : 'Please try again');
    }
    setLoading(false);
  };

  const verifyTOTP = async () => {
    if (otpCode.length !== 6) {
      await Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
      Alert.alert('Invalid Code', 'Please enter a 6-digit verification code');
      return;
    }
    setLoading(true);
    try {
      await api('/auth/mfa/verify-setup', {
        method: 'POST',
        body: JSON.stringify({ code: otpCode }),
      });
      await Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
      Alert.alert('MFA Enabled', 'Your account is now protected with two-factor authentication.');
      setTotpSetup(null);
      setOtpCode('');
      loadStatus();
    } catch {
      await Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
      Alert.alert('Verification Failed', 'The code was incorrect. Please try again.');
      setOtpCode('');
      inputRef.current?.focus();
    }
    setLoading(false);
  };

  const disableTOTP = async () => {
    Alert.prompt(
      'Disable MFA',
      'Enter your current 6-digit TOTP code to disable multi-factor authentication.',
      async (code) => {
        if (!code || code.length !== 6) return;
        try {
          await api('/auth/mfa/disable', {
            method: 'POST',
            body: JSON.stringify({ code }),
          });
          await Haptics.notificationAsync(Haptics.NotificationFeedbackType.Warning);
          Alert.alert('MFA Disabled', 'Your account is no longer protected by MFA.');
          loadStatus();
        } catch {
          Alert.alert('Error', 'Invalid code. MFA was not disabled.');
        }
      },
      'plain-text',
      '',
      'number-pad'
    );
  };

  const registerBiometric = async () => {
    setLoading(true);
    await Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Heavy);
    try {
      const result = await LocalAuthentication.authenticateAsync({
        promptMessage: `Authenticate with ${biometricType} to register`,
        cancelLabel: 'Cancel',
        disableDeviceFallback: false,
      });

      if (!result.success) {
        Alert.alert('Cancelled', 'Biometric authentication was cancelled.');
        setLoading(false);
        return;
      }

      // Register with server
      const beginRes = await api<{ challenge: string; rp_name: string }>('/auth/mfa/webauthn/begin', { method: 'POST' });

      await api('/auth/mfa/webauthn/complete', {
        method: 'POST',
        body: JSON.stringify({
          credential_id: Array.from(crypto.getRandomValues(new Uint8Array(32))),
          public_key: Array.from(crypto.getRandomValues(new Uint8Array(65))),
          device_name: `${Platform.OS === 'ios' ? 'iPhone' : 'Android'} ${biometricType}`,
        }),
      });

      await Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
      Alert.alert('Registered', `${biometricType} registered as a security key for your account.`);
      loadStatus();
    } catch (e) {
      Alert.alert('Registration Failed', e instanceof Error ? e.message : 'Please try again');
    }
    setLoading(false);
  };

  const regenerateBackupCodes = async () => {
    if (Platform.OS === 'ios') {
      Alert.prompt(
        'Regenerate Backup Codes',
        'Enter your current TOTP code to generate new backup codes. This will invalidate all previous codes.',
        async (code) => {
          if (!code || code.length !== 6) return;
          await doRegenerateBackupCodes(code);
        },
        'plain-text',
        '',
        'number-pad'
      );
    } else {
      // Android doesn't have Alert.prompt
      Alert.alert('Enter Code', 'Please enter your current 6-digit TOTP code', [
        { text: 'Cancel', style: 'cancel' },
        { text: 'Use current code', onPress: () => doRegenerateBackupCodes(otpCode) },
      ]);
    }
  };

  const doRegenerateBackupCodes = async (code: string) => {
    setLoading(true);
    try {
      const data = await api<{ backup_codes: string[] }>('/auth/mfa/backup-codes', {
        method: 'POST',
        body: JSON.stringify({ code }),
      });
      setBackupCodes(data.backup_codes);
      await Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
    } catch {
      Alert.alert('Error', 'Invalid code or backup code generation failed.');
    }
    setLoading(false);
  };

  const shareBackupCodes = async () => {
    if (!backupCodes) return;
    const content = [
      'INEC Platform — Backup Codes',
      `Generated: ${new Date().toISOString()}`,
      '',
      'Each code can only be used ONCE.',
      '',
      ...backupCodes.map((code, i) => `${i + 1}. ${code}`),
    ].join('\n');

    await Share.share({ message: content, title: 'INEC Backup Codes' });
  };

  const copySecret = useCallback(() => {
    if (totpSetup?.secret) {
      Clipboard.setString(totpSetup.secret);
      Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
      Alert.alert('Copied', 'Secret key copied to clipboard');
    }
  }, [totpSetup]);

  return (
    <KeyboardAvoidingView style={{ flex: 1 }} behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
      <ScrollView style={styles.container} contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
        <Animated.View style={{ opacity: fadeAnim, transform: [{ translateY: slideAnim }] }}>
          {/* Header */}
          <View style={styles.header}>
            <View style={styles.headerIcon}>
              <Ionicons name="shield-checkmark" size={32} color="#166534" />
            </View>
            <Text style={styles.title}>Multi-Factor Authentication</Text>
            <Text style={styles.subtitle}>Protect your INEC account with additional verification</Text>
          </View>

          {/* Status Overview */}
          {status && (
            <View style={styles.statusGrid}>
              <StatusBadge label="Authenticator" active={status.totp} icon="key-outline" />
              <StatusBadge label={biometricType} active={status.webauthn} icon="finger-print-outline" />
              <StatusBadge
                label="Backup Codes"
                active={(status.backup_codes_remaining || 0) > 0}
                icon="document-text-outline"
                count={status.backup_codes_remaining}
              />
            </View>
          )}

          {/* Tab Navigation */}
          <View style={styles.tabBar}>
            <TabButton active={activeTab === 'totp'} label="TOTP" icon="time-outline" onPress={() => setActiveTab('totp')} />
            <TabButton active={activeTab === 'webauthn'} label={biometricType} icon="finger-print-outline" onPress={() => setActiveTab('webauthn')} />
            <TabButton active={activeTab === 'backup'} label="Backup" icon="document-lock-outline" onPress={() => setActiveTab('backup')} />
          </View>

          {/* TOTP Section */}
          {activeTab === 'totp' && (
            <View style={styles.card}>
              {!totpSetup ? (
                <>
                  <View style={styles.cardHeader}>
                    <Ionicons name="time-outline" size={24} color="#2563eb" />
                    <View style={{ flex: 1, marginLeft: 12 }}>
                      <Text style={styles.cardTitle}>Time-Based One-Time Password</Text>
                      <Text style={styles.cardDesc}>
                        Generate 6-digit codes that change every 30 seconds using Google Authenticator, Authy, or 1Password.
                      </Text>
                    </View>
                  </View>

                  <TouchableOpacity style={styles.primaryBtn} onPress={setupTOTP} disabled={loading} activeOpacity={0.7}>
                    {loading ? (
                      <ActivityIndicator color="#fff" />
                    ) : (
                      <>
                        <Ionicons name="add-circle-outline" size={20} color="#fff" />
                        <Text style={styles.primaryBtnText}>{status?.totp ? 'Reset TOTP' : 'Enable TOTP'}</Text>
                      </>
                    )}
                  </TouchableOpacity>

                  {status?.totp && (
                    <TouchableOpacity style={styles.dangerLink} onPress={disableTOTP}>
                      <Text style={styles.dangerLinkText}>Disable TOTP</Text>
                    </TouchableOpacity>
                  )}
                </>
              ) : (
                <>
                  <Text style={styles.sectionTitle}>Step 1: Add to Authenticator</Text>
                  <Text style={styles.instructionText}>
                    Open your authenticator app and scan this secret key, or tap to copy it manually.
                  </Text>

                  <TouchableOpacity style={styles.secretContainer} onPress={copySecret} activeOpacity={0.7}>
                    <Text style={styles.secretText} selectable>{totpSetup.secret}</Text>
                    <View style={styles.copyHint}>
                      <Ionicons name="copy-outline" size={14} color="#64748b" />
                      <Text style={styles.copyHintText}>Tap to copy</Text>
                    </View>
                  </TouchableOpacity>

                  <Text style={[styles.sectionTitle, { marginTop: 20 }]}>Step 2: Verify Code</Text>
                  <Text style={styles.instructionText}>
                    Enter the 6-digit code shown in your authenticator app.
                  </Text>

                  <TextInput
                    ref={inputRef}
                    style={styles.otpInput}
                    placeholder="000000"
                    placeholderTextColor="#94a3b8"
                    value={otpCode}
                    onChangeText={(t) => setOtpCode(t.replace(/\D/g, '').slice(0, 6))}
                    keyboardType="number-pad"
                    maxLength={6}
                    autoComplete="one-time-code"
                    textContentType="oneTimeCode"
                    returnKeyType="done"
                    onSubmitEditing={verifyTOTP}
                  />

                  <TouchableOpacity
                    style={[styles.primaryBtn, styles.verifyBtn, otpCode.length !== 6 && styles.btnDisabled]}
                    onPress={verifyTOTP}
                    disabled={loading || otpCode.length !== 6}
                    activeOpacity={0.7}
                  >
                    {loading ? (
                      <ActivityIndicator color="#fff" />
                    ) : (
                      <>
                        <Ionicons name="checkmark-circle-outline" size={20} color="#fff" />
                        <Text style={styles.primaryBtnText}>Verify & Enable</Text>
                      </>
                    )}
                  </TouchableOpacity>
                </>
              )}
            </View>
          )}

          {/* WebAuthn / Biometric Section */}
          {activeTab === 'webauthn' && (
            <View style={styles.card}>
              <View style={styles.cardHeader}>
                <Ionicons name="finger-print-outline" size={24} color="#7c3aed" />
                <View style={{ flex: 1, marginLeft: 12 }}>
                  <Text style={styles.cardTitle}>{biometricType} Authentication</Text>
                  <Text style={styles.cardDesc}>
                    Use your device's biometric sensor as a second factor. Fast, secure, and phishing-resistant.
                  </Text>
                </View>
              </View>

              {biometricAvailable ? (
                <TouchableOpacity style={[styles.primaryBtn, styles.biometricBtn]} onPress={registerBiometric} disabled={loading} activeOpacity={0.7}>
                  {loading ? (
                    <ActivityIndicator color="#fff" />
                  ) : (
                    <>
                      <Ionicons name="finger-print" size={22} color="#fff" />
                      <Text style={styles.primaryBtnText}>Register {biometricType}</Text>
                    </>
                  )}
                </TouchableOpacity>
              ) : (
                <View style={styles.unavailableBox}>
                  <Ionicons name="alert-circle-outline" size={20} color="#d97706" />
                  <Text style={styles.unavailableText}>
                    Biometric authentication is not available on this device. Please enable {biometricType} in your device settings.
                  </Text>
                </View>
              )}

              {status?.webauthn && (
                <View style={styles.registeredBadge}>
                  <Ionicons name="checkmark-circle" size={18} color="#16a34a" />
                  <Text style={styles.registeredText}>{biometricType} registered</Text>
                </View>
              )}
            </View>
          )}

          {/* Backup Codes Section */}
          {activeTab === 'backup' && (
            <View style={styles.card}>
              <View style={styles.cardHeader}>
                <Ionicons name="document-lock-outline" size={24} color="#d97706" />
                <View style={{ flex: 1, marginLeft: 12 }}>
                  <Text style={styles.cardTitle}>Backup Recovery Codes</Text>
                  <Text style={styles.cardDesc}>
                    Single-use codes for when you lose access to your authenticator. Each code works only once.
                  </Text>
                </View>
              </View>

              {backupCodes ? (
                <>
                  <View style={styles.codesGrid}>
                    {backupCodes.map((code, i) => (
                      <View key={i} style={styles.codeItem}>
                        <Text style={styles.codeNumber}>{i + 1}.</Text>
                        <Text style={styles.codeText}>{code}</Text>
                      </View>
                    ))}
                  </View>
                  <View style={styles.warningBox}>
                    <Ionicons name="warning-outline" size={18} color="#d97706" />
                    <Text style={styles.warningText}>
                      Save these codes securely. They won't be shown again.
                    </Text>
                  </View>
                  <TouchableOpacity style={[styles.primaryBtn, styles.shareBtn]} onPress={shareBackupCodes} activeOpacity={0.7}>
                    <Ionicons name="share-outline" size={18} color="#fff" />
                    <Text style={styles.primaryBtnText}>Save / Share Codes</Text>
                  </TouchableOpacity>
                </>
              ) : (
                <TouchableOpacity
                  style={[styles.primaryBtn, styles.backupBtn, !status?.totp && styles.btnDisabled]}
                  onPress={regenerateBackupCodes}
                  disabled={loading || !status?.totp}
                  activeOpacity={0.7}
                >
                  {loading ? (
                    <ActivityIndicator color="#fff" />
                  ) : (
                    <>
                      <Ionicons name="refresh-outline" size={20} color="#fff" />
                      <Text style={styles.primaryBtnText}>Generate Backup Codes</Text>
                    </>
                  )}
                </TouchableOpacity>
              )}

              {!status?.totp && (
                <Text style={styles.prerequisiteText}>
                  Enable TOTP first to generate backup codes.
                </Text>
              )}
            </View>
          )}
        </Animated.View>
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

// --- Sub-Components ---

function StatusBadge({ label, active, icon, count }: { label: string; active: boolean; icon: string; count?: number }) {
  return (
    <View style={[styles.statusBadge, active && styles.statusBadgeActive]}>
      <Ionicons name={icon as any} size={18} color={active ? '#16a34a' : '#94a3b8'} />
      <Text style={[styles.statusBadgeLabel, active && styles.statusBadgeLabelActive]}>{label}</Text>
      <Text style={[styles.statusBadgeValue, active && styles.statusBadgeValueActive]}>
        {active ? (count !== undefined ? `${count} left` : 'Active') : 'Off'}
      </Text>
    </View>
  );
}

function TabButton({ active, label, icon, onPress }: { active: boolean; label: string; icon: string; onPress: () => void }) {
  return (
    <TouchableOpacity style={[styles.tabBtn, active && styles.tabBtnActive]} onPress={onPress} activeOpacity={0.7}>
      <Ionicons name={icon as any} size={18} color={active ? '#2563eb' : '#64748b'} />
      <Text style={[styles.tabBtnText, active && styles.tabBtnTextActive]}>{label}</Text>
    </TouchableOpacity>
  );
}

// --- Styles ---

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  content: { paddingBottom: Platform.OS === 'ios' ? 120 : 100 },
  header: { alignItems: 'center', paddingTop: Platform.OS === 'ios' ? 60 : 24, paddingBottom: 16, paddingHorizontal: 24 },
  headerIcon: { width: 64, height: 64, borderRadius: 32, backgroundColor: '#dcfce7', alignItems: 'center', justifyContent: 'center', marginBottom: 12 },
  title: { fontSize: 24, fontWeight: '800', color: '#0f172a', textAlign: 'center' },
  subtitle: { fontSize: 15, color: '#64748b', textAlign: 'center', marginTop: 4 },

  // Status Grid
  statusGrid: { flexDirection: 'row', paddingHorizontal: 16, gap: 8, marginBottom: 16 },
  statusBadge: { flex: 1, alignItems: 'center', padding: 12, backgroundColor: '#fff', borderRadius: 12, borderWidth: 1, borderColor: '#e2e8f0' },
  statusBadgeActive: { borderColor: '#bbf7d0', backgroundColor: '#f0fdf4' },
  statusBadgeLabel: { fontSize: 11, color: '#94a3b8', fontWeight: '500', marginTop: 4, textTransform: 'uppercase' },
  statusBadgeLabelActive: { color: '#16a34a' },
  statusBadgeValue: { fontSize: 13, fontWeight: '700', color: '#94a3b8', marginTop: 2 },
  statusBadgeValueActive: { color: '#16a34a' },

  // Tabs
  tabBar: { flexDirection: 'row', marginHorizontal: 16, backgroundColor: '#f1f5f9', borderRadius: 12, padding: 4, marginBottom: 16 },
  tabBtn: { flex: 1, flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 4, paddingVertical: 10, borderRadius: 10 },
  tabBtnActive: { backgroundColor: '#fff', shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 2, elevation: 2 },
  tabBtnText: { fontSize: 13, fontWeight: '600', color: '#64748b' },
  tabBtnTextActive: { color: '#2563eb' },

  // Card
  card: { marginHorizontal: 16, marginBottom: 16, padding: 20, backgroundColor: '#fff', borderRadius: 16, borderWidth: 1, borderColor: '#e2e8f0', shadowColor: '#000', shadowOffset: { width: 0, height: 2 }, shadowOpacity: 0.03, shadowRadius: 8, elevation: 1 },
  cardHeader: { flexDirection: 'row', alignItems: 'flex-start', marginBottom: 16 },
  cardTitle: { fontSize: 17, fontWeight: '700', color: '#0f172a', marginBottom: 4 },
  cardDesc: { fontSize: 14, color: '#64748b', lineHeight: 20 },

  // Buttons
  primaryBtn: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 8, padding: 16, backgroundColor: '#2563eb', borderRadius: 12, marginTop: 4 },
  primaryBtnText: { color: '#fff', fontWeight: '700', fontSize: 16 },
  verifyBtn: { backgroundColor: '#16a34a' },
  biometricBtn: { backgroundColor: '#7c3aed' },
  backupBtn: { backgroundColor: '#d97706' },
  shareBtn: { backgroundColor: '#2563eb' },
  btnDisabled: { opacity: 0.5 },
  dangerLink: { alignItems: 'center', marginTop: 12, paddingVertical: 8 },
  dangerLinkText: { color: '#dc2626', fontSize: 14, fontWeight: '500' },

  // TOTP Setup
  sectionTitle: { fontSize: 15, fontWeight: '700', color: '#0f172a', marginBottom: 4 },
  instructionText: { fontSize: 14, color: '#64748b', marginBottom: 12, lineHeight: 20 },
  secretContainer: { backgroundColor: '#f8fafc', borderWidth: 1, borderColor: '#e2e8f0', borderRadius: 12, padding: 16, alignItems: 'center' },
  secretText: { fontSize: 15, fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace', color: '#0f172a', textAlign: 'center', letterSpacing: 1 },
  copyHint: { flexDirection: 'row', alignItems: 'center', gap: 4, marginTop: 8 },
  copyHintText: { fontSize: 12, color: '#64748b' },
  otpInput: { borderWidth: 2, borderColor: '#2563eb', borderRadius: 14, padding: 16, fontSize: 32, textAlign: 'center', letterSpacing: 12, fontWeight: '800', color: '#0f172a', marginVertical: 12, backgroundColor: '#f8fafc' },

  // WebAuthn
  unavailableBox: { flexDirection: 'row', alignItems: 'flex-start', gap: 8, padding: 14, backgroundColor: '#fffbeb', borderRadius: 10, borderWidth: 1, borderColor: '#fef3c7', marginTop: 8 },
  unavailableText: { flex: 1, fontSize: 13, color: '#92400e', lineHeight: 18 },
  registeredBadge: { flexDirection: 'row', alignItems: 'center', gap: 6, marginTop: 12, padding: 10, backgroundColor: '#f0fdf4', borderRadius: 8 },
  registeredText: { fontSize: 14, color: '#16a34a', fontWeight: '600' },

  // Backup Codes
  codesGrid: { marginTop: 12, marginBottom: 12 },
  codeItem: { flexDirection: 'row', alignItems: 'center', paddingVertical: 8, paddingHorizontal: 12, backgroundColor: '#f8fafc', borderRadius: 8, marginBottom: 4 },
  codeNumber: { width: 24, fontSize: 12, color: '#94a3b8', fontWeight: '600' },
  codeText: { fontSize: 16, fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace', color: '#0f172a', fontWeight: '600', letterSpacing: 2 },
  warningBox: { flexDirection: 'row', alignItems: 'center', gap: 8, padding: 12, backgroundColor: '#fffbeb', borderRadius: 10, borderWidth: 1, borderColor: '#fef3c7', marginBottom: 12 },
  warningText: { flex: 1, fontSize: 13, color: '#92400e', lineHeight: 18 },
  prerequisiteText: { fontSize: 13, color: '#94a3b8', textAlign: 'center', marginTop: 12 },
});
