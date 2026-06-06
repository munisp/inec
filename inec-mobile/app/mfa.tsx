import { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, TextInput, Platform, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api } from '../src/lib/api';

interface TOTPSetup {
  secret: string;
  provisioning_uri: string;
  status: string;
}

export default function MFAScreen() {
  const [totpSetup, setTotpSetup] = useState<TOTPSetup | null>(null);
  const [otpCode, setOtpCode] = useState('');
  const [verifyResult, setVerifyResult] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const setupTOTP = async () => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      const data = await api<TOTPSetup>('/auth/mfa/totp/setup', { method: 'POST' });
      setTotpSetup(data);
    } catch (e) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed to setup TOTP');
    }
    setLoading(false);
  };

  const verifyTOTP = async () => {
    if (otpCode.length !== 6) {
      Alert.alert('Error', 'Enter 6-digit code');
      return;
    }
    Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
    try {
      const data = await api<{ status: string }>('/auth/mfa/totp/verify', {
        method: 'POST',
        body: JSON.stringify({ code: otpCode }),
      });
      setVerifyResult(data.status);
    } catch (e) {
      setVerifyResult('failed');
    }
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}>
      <View style={styles.header}>
        <Ionicons name="shield-checkmark" size={28} color="#166534" />
        <Text style={styles.title}>Multi-Factor Auth</Text>
      </View>

      <View style={styles.card}>
        <Text style={styles.cardTitle}>TOTP (Authenticator App)</Text>
        <Text style={styles.cardDesc}>
          Set up time-based one-time passwords using Google Authenticator, Authy, or similar apps.
        </Text>

        {!totpSetup ? (
          <TouchableOpacity style={styles.setupBtn} onPress={setupTOTP} disabled={loading}>
            <Ionicons name="key" size={18} color="#fff" />
            <Text style={styles.setupText}>{loading ? 'Setting up...' : 'Setup TOTP'}</Text>
          </TouchableOpacity>
        ) : (
          <View style={styles.setupResult}>
            <Text style={styles.secretLabel}>Secret Key:</Text>
            <View style={styles.secretBox}>
              <Text style={styles.secretText} selectable>{totpSetup.secret}</Text>
            </View>
            <Text style={styles.hint}>Enter this key in your authenticator app, then verify below.</Text>

            <TextInput
              style={styles.otpInput}
              placeholder="6-digit code"
              value={otpCode}
              onChangeText={setOtpCode}
              keyboardType="number-pad"
              maxLength={6}
            />
            <TouchableOpacity style={styles.verifyBtn} onPress={verifyTOTP}>
              <Text style={styles.verifyText}>Verify Code</Text>
            </TouchableOpacity>

            {verifyResult && (
              <View style={[styles.resultBadge, { backgroundColor: verifyResult === 'verified' ? '#dcfce7' : '#fef2f2' }]}>
                <Ionicons
                  name={verifyResult === 'verified' ? 'checkmark-circle' : 'close-circle'}
                  size={20}
                  color={verifyResult === 'verified' ? '#166534' : '#dc2626'}
                />
                <Text style={{ color: verifyResult === 'verified' ? '#166534' : '#dc2626', fontWeight: '600' }}>
                  {verifyResult === 'verified' ? 'TOTP Verified' : 'Verification Failed'}
                </Text>
              </View>
            )}
          </View>
        )}
      </View>

      <View style={styles.card}>
        <Text style={styles.cardTitle}>SMS OTP</Text>
        <Text style={styles.cardDesc}>
          Receive one-time verification codes via SMS for election day accreditation.
        </Text>
        <View style={styles.statusRow}>
          <Ionicons name="checkmark-circle" size={18} color="#22c55e" />
          <Text style={styles.statusText}>Available — auto-sent on accreditation</Text>
        </View>
      </View>

      <View style={styles.card}>
        <Text style={styles.cardTitle}>WebAuthn / FIDO2</Text>
        <Text style={styles.cardDesc}>
          Use biometric authentication (fingerprint, face) or hardware security keys.
        </Text>
        <View style={styles.statusRow}>
          <Ionicons name="hardware-chip" size={18} color="#3b82f6" />
          <Text style={styles.statusText}>Configure via web portal</Text>
        </View>
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  header: { flexDirection: 'row', alignItems: 'center', gap: 10, padding: 16, paddingTop: Platform.OS === 'ios' ? 60 : 16 },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b' },
  card: { margin: 16, marginBottom: 8, padding: 16, backgroundColor: '#fff', borderRadius: 12, borderWidth: 1, borderColor: '#e2e8f0' },
  cardTitle: { fontSize: 17, fontWeight: '600', color: '#1e293b', marginBottom: 6 },
  cardDesc: { fontSize: 14, color: '#64748b', lineHeight: 20 },
  setupBtn: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 8, marginTop: 14, padding: 14, backgroundColor: '#166534', borderRadius: 10 },
  setupText: { color: '#fff', fontWeight: '600', fontSize: 15 },
  setupResult: { marginTop: 14 },
  secretLabel: { fontSize: 13, fontWeight: '600', color: '#1e293b', marginBottom: 6 },
  secretBox: { padding: 12, backgroundColor: '#f1f5f9', borderRadius: 8, marginBottom: 8 },
  secretText: { fontSize: 14, fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace', color: '#1e293b', textAlign: 'center' },
  hint: { fontSize: 12, color: '#64748b', marginBottom: 12 },
  otpInput: { borderWidth: 2, borderColor: '#166534', borderRadius: 10, padding: 14, fontSize: 24, textAlign: 'center', letterSpacing: 8, fontWeight: '700' },
  verifyBtn: { marginTop: 10, padding: 14, backgroundColor: '#166534', borderRadius: 10, alignItems: 'center' },
  verifyText: { color: '#fff', fontWeight: '600', fontSize: 15 },
  resultBadge: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 8, marginTop: 12, padding: 12, borderRadius: 10 },
  statusRow: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: 12 },
  statusText: { fontSize: 14, color: '#64748b' },
});
