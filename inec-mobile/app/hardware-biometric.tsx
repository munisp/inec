import { useState, useEffect, useCallback } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Alert, Platform } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import * as LocalAuthentication from 'expo-local-authentication';

type AuthType = 'fingerprint' | 'facial' | 'iris';

interface BiometricCapability {
  available: boolean;
  enrolled: boolean;
  level: number;
  types: LocalAuthentication.AuthenticationType[];
  typeLabels: string[];
}

interface AuthResult {
  success: boolean;
  method: string;
  timestamp: string;
  error?: string;
}

function authTypeLabel(t: LocalAuthentication.AuthenticationType): string {
  switch (t) {
    case LocalAuthentication.AuthenticationType.FINGERPRINT:
      return 'Fingerprint';
    case LocalAuthentication.AuthenticationType.FACIAL_RECOGNITION:
      return 'Facial Recognition';
    case LocalAuthentication.AuthenticationType.IRIS:
      return 'Iris Scan';
    default:
      return 'Unknown';
  }
}

function authTypeIcon(t: LocalAuthentication.AuthenticationType): string {
  switch (t) {
    case LocalAuthentication.AuthenticationType.FINGERPRINT:
      return 'finger-print-outline';
    case LocalAuthentication.AuthenticationType.FACIAL_RECOGNITION:
      return 'person-circle-outline';
    case LocalAuthentication.AuthenticationType.IRIS:
      return 'eye-outline';
    default:
      return 'help-outline';
  }
}

export default function HardwareBiometricScreen() {
  const [capability, setCapability] = useState<BiometricCapability | null>(null);
  const [authHistory, setAuthHistory] = useState<AuthResult[]>([]);
  const [authenticating, setAuthenticating] = useState(false);

  useEffect(() => {
    checkCapabilities();
  }, []);

  const checkCapabilities = async () => {
    try {
      const [available, level, types, enrolled] = await Promise.all([
        LocalAuthentication.hasHardwareAsync(),
        LocalAuthentication.getEnrolledLevelAsync(),
        LocalAuthentication.supportedAuthenticationTypesAsync(),
        LocalAuthentication.isEnrolledAsync(),
      ]);

      setCapability({
        available,
        enrolled,
        level,
        types,
        typeLabels: types.map(authTypeLabel),
      });
    } catch (e: unknown) {
      Alert.alert('Error', 'Failed to check biometric capabilities');
    }
  };

  const authenticate = useCallback(async (reason: string) => {
    if (authenticating) return;
    setAuthenticating(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);

    try {
      const result = await LocalAuthentication.authenticateAsync({
        promptMessage: reason,
        cancelLabel: 'Cancel',
        disableDeviceFallback: false,
        fallbackLabel: 'Use passcode',
      });

      const authResult: AuthResult = {
        success: result.success,
        method: result.success ? 'biometric' : 'cancelled',
        timestamp: new Date().toISOString(),
        error: result.success ? undefined : (result.error || 'Authentication cancelled'),
      };

      setAuthHistory(prev => [authResult, ...prev.slice(0, 9)]);

      if (result.success) {
        Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
        Alert.alert('Authenticated', 'Biometric verification successful.');
      } else {
        Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
        Alert.alert('Failed', result.error || 'Authentication was cancelled.');
      }
    } catch (e: unknown) {
      const errorMsg = e instanceof Error ? e.message : 'Authentication error';
      setAuthHistory(prev => [{
        success: false,
        method: 'error',
        timestamp: new Date().toISOString(),
        error: errorMsg,
      }, ...prev.slice(0, 9)]);
      Alert.alert('Error', errorMsg);
    }

    setAuthenticating(false);
  }, [authenticating]);

  const securityLevel = (level: number): string => {
    switch (level) {
      case LocalAuthentication.SecurityLevel.NONE: return 'None';
      case LocalAuthentication.SecurityLevel.SECRET: return 'PIN/Pattern/Password';
      case LocalAuthentication.SecurityLevel.BIOMETRIC_STRONG: return 'Strong Biometric (Class 3)';
      case LocalAuthentication.SecurityLevel.BIOMETRIC_WEAK: return 'Weak Biometric (Class 2)';
      default: return `Level ${level}`;
    }
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: 40 }}>
      {/* Device Capabilities */}
      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="hardware-chip-outline" size={24} color="#166534" />
          <Text style={styles.cardTitle}>Device Biometric Hardware</Text>
        </View>

        {capability ? (
          <>
            <View style={styles.statusRow}>
              <Text style={styles.statusLabel}>Hardware Available</Text>
              <Ionicons name={capability.available ? 'checkmark-circle' : 'close-circle'} size={20} color={capability.available ? '#166534' : '#dc2626'} />
            </View>
            <View style={styles.statusRow}>
              <Text style={styles.statusLabel}>Biometrics Enrolled</Text>
              <Ionicons name={capability.enrolled ? 'checkmark-circle' : 'close-circle'} size={20} color={capability.enrolled ? '#166534' : '#dc2626'} />
            </View>
            <View style={styles.statusRow}>
              <Text style={styles.statusLabel}>Security Level</Text>
              <Text style={styles.statusValue}>{securityLevel(capability.level)}</Text>
            </View>
            <View style={styles.statusRow}>
              <Text style={styles.statusLabel}>Platform</Text>
              <Text style={styles.statusValue}>{Platform.OS} {Platform.Version}</Text>
            </View>

            {capability.types.length > 0 && (
              <View style={{ marginTop: 12 }}>
                <Text style={[styles.statusLabel, { marginBottom: 8 }]}>Supported Modalities</Text>
                {capability.types.map((t, i) => (
                  <View key={i} style={styles.modalityRow}>
                    <Ionicons name={authTypeIcon(t) as keyof typeof Ionicons.glyphMap} size={20} color="#166534" />
                    <Text style={{ fontSize: 14, color: '#111827', marginLeft: 8 }}>{authTypeLabel(t)}</Text>
                  </View>
                ))}
              </View>
            )}

            {!capability.available && (
              <View style={styles.warningBox}>
                <Ionicons name="warning-outline" size={20} color="#ca8a04" />
                <Text style={styles.warningText}>No biometric hardware detected. Use passcode fallback for voter verification.</Text>
              </View>
            )}
          </>
        ) : (
          <Text style={styles.muted}>Checking device capabilities...</Text>
        )}
      </View>

      {/* Authentication Actions */}
      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="shield-checkmark-outline" size={24} color="#2563eb" />
          <Text style={styles.cardTitle}>Biometric Actions</Text>
        </View>

        <TouchableOpacity
          style={styles.authButton}
          onPress={() => authenticate('Verify identity for BVAS accreditation')}
          disabled={authenticating || !capability?.available}
          activeOpacity={0.8}
        >
          <Ionicons name="finger-print-outline" size={24} color="#fff" />
          <View style={{ flex: 1, marginLeft: 12 }}>
            <Text style={styles.authButtonTitle}>BVAS Accreditation</Text>
            <Text style={styles.authButtonSub}>Verify voter identity before ballot</Text>
          </View>
        </TouchableOpacity>

        <TouchableOpacity
          style={[styles.authButton, { backgroundColor: '#2563eb' }]}
          onPress={() => authenticate('Authenticate as election official')}
          disabled={authenticating || !capability?.available}
          activeOpacity={0.8}
        >
          <Ionicons name="person-circle-outline" size={24} color="#fff" />
          <View style={{ flex: 1, marginLeft: 12 }}>
            <Text style={styles.authButtonTitle}>Official Login</Text>
            <Text style={styles.authButtonSub}>Biometric authentication for officials</Text>
          </View>
        </TouchableOpacity>

        <TouchableOpacity
          style={[styles.authButton, { backgroundColor: '#7c3aed' }]}
          onPress={() => authenticate('Verify result submission authorization')}
          disabled={authenticating || !capability?.available}
          activeOpacity={0.8}
        >
          <Ionicons name="document-lock-outline" size={24} color="#fff" />
          <View style={{ flex: 1, marginLeft: 12 }}>
            <Text style={styles.authButtonTitle}>Result Submission Auth</Text>
            <Text style={styles.authButtonSub}>Biometric sign-off on election results</Text>
          </View>
        </TouchableOpacity>
      </View>

      {/* Auth History */}
      {authHistory.length > 0 && (
        <View style={styles.card}>
          <Text style={styles.cardTitle}>Authentication Log</Text>
          {authHistory.map((h, i) => (
            <View key={i} style={styles.historyRow}>
              <Ionicons
                name={h.success ? 'checkmark-circle' : 'close-circle'}
                size={18}
                color={h.success ? '#166534' : '#dc2626'}
              />
              <View style={{ flex: 1, marginLeft: 8 }}>
                <Text style={{ fontSize: 13, color: h.success ? '#166534' : '#dc2626', fontWeight: '600' }}>
                  {h.success ? 'Verified' : 'Failed'}
                </Text>
                <Text style={styles.muted}>{new Date(h.timestamp).toLocaleTimeString()}</Text>
              </View>
            </View>
          ))}
        </View>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb', padding: 16 },
  card: { backgroundColor: '#fff', borderRadius: 16, padding: 16, marginBottom: 16, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  cardHeader: { flexDirection: 'row', alignItems: 'center', gap: 10, marginBottom: 12 },
  cardTitle: { fontSize: 16, fontWeight: '700', color: '#111827', marginBottom: 4 },
  muted: { fontSize: 12, color: '#9ca3af' },
  statusRow: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', paddingVertical: 8, borderBottomWidth: 1, borderBottomColor: '#f3f4f6' },
  statusLabel: { fontSize: 14, color: '#6b7280' },
  statusValue: { fontSize: 14, fontWeight: '600', color: '#111827' },
  modalityRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 6 },
  warningBox: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: 12, padding: 12, backgroundColor: '#fef9c3', borderRadius: 10 },
  warningText: { flex: 1, fontSize: 13, color: '#854d0e' },
  authButton: { flexDirection: 'row', alignItems: 'center', backgroundColor: '#166534', borderRadius: 12, padding: 16, marginBottom: 8 },
  authButtonTitle: { fontSize: 15, fontWeight: '700', color: '#fff' },
  authButtonSub: { fontSize: 12, color: 'rgba(255,255,255,0.8)', marginTop: 2 },
  historyRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 8, borderBottomWidth: 1, borderBottomColor: '#f3f4f6' },
});
