import { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Alert, Platform } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api as apiCall } from '../src/lib/api';

interface BiometricStatus {
  total_profiles: number;
  verified_count: number;
  pending_count: number;
  deduplication_status?: string;
}

interface VerifyResult {
  match: boolean;
  score: number;
  quality_score: number;
  pad_score: number;
  vin: string;
}

export default function BiometricsScreen() {
  const [status, setStatus] = useState<BiometricStatus | null>(null);
  const [verifyResult, setVerifyResult] = useState<VerifyResult | null>(null);
  const [loading, setLoading] = useState(false);

  const loadStatus = async () => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    try {
      const res = await apiCall<BiometricStatus>('/biometric/status');
      setStatus(res);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed to load biometric status');
    }
    setLoading(false);
  };

  const runDemoVerify = async () => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      const form = new FormData();
      form.append('vin', 'VIN-DEMO-001');
      form.append('biometric_type', 'fingerprint');
      const res = await apiCall<VerifyResult>('/biometric/verify', { method: 'POST', body: form });
      setVerifyResult(res);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Verification failed');
    }
    setLoading(false);
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: 40 }}>
      {/* Status Card */}
      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="finger-print-outline" size={24} color="#166534" />
          <Text style={styles.cardTitle}>ABIS Engine Status</Text>
        </View>
        <TouchableOpacity style={styles.button} onPress={loadStatus} disabled={loading} activeOpacity={0.8}>
          <Text style={styles.buttonText}>{loading ? 'Loading...' : 'Check Status'}</Text>
        </TouchableOpacity>
        {status && (
          <View style={styles.statsGrid}>
            <View style={styles.statCard}>
              <Text style={styles.statNumber}>{status.total_profiles}</Text>
              <Text style={styles.statLabel}>Total Profiles</Text>
            </View>
            <View style={styles.statCard}>
              <Text style={[styles.statNumber, { color: '#166534' }]}>{status.verified_count}</Text>
              <Text style={styles.statLabel}>Verified</Text>
            </View>
            <View style={styles.statCard}>
              <Text style={[styles.statNumber, { color: '#f59e0b' }]}>{status.pending_count}</Text>
              <Text style={styles.statLabel}>Pending</Text>
            </View>
          </View>
        )}
      </View>

      {/* Verify Card */}
      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="scan-outline" size={24} color="#7c3aed" />
          <Text style={styles.cardTitle}>Biometric Verification</Text>
        </View>
        <Text style={styles.muted}>Verify voter identity using fingerprint or face biometrics against the ABIS database.</Text>
        <TouchableOpacity style={[styles.button, { backgroundColor: '#7c3aed' }]} onPress={runDemoVerify} disabled={loading} activeOpacity={0.8}>
          <Text style={styles.buttonText}>Run Verification</Text>
        </TouchableOpacity>
        {verifyResult && (
          <View style={[styles.resultBanner, { backgroundColor: verifyResult.match ? '#dcfce7' : '#fef2f2' }]}>
            <Ionicons name={verifyResult.match ? 'checkmark-circle' : 'close-circle'} size={28} color={verifyResult.match ? '#166534' : '#dc2626'} />
            <View style={{ flex: 1, marginLeft: 12 }}>
              <Text style={{ fontSize: 16, fontWeight: '700', color: verifyResult.match ? '#166534' : '#dc2626' }}>
                {verifyResult.match ? 'Identity Verified' : 'No Match Found'}
              </Text>
              <View style={styles.scoreRow}>
                <Text style={styles.scoreLabel}>Match: {(verifyResult.score * 100).toFixed(0)}%</Text>
                <Text style={styles.scoreLabel}>Quality: {(verifyResult.quality_score * 100).toFixed(0)}%</Text>
                <Text style={styles.scoreLabel}>Liveness: {(verifyResult.pad_score * 100).toFixed(0)}%</Text>
              </View>
              <Text style={[styles.muted, { marginTop: 4 }]}>VIN: {verifyResult.vin}</Text>
            </View>
          </View>
        )}
      </View>

      {/* Capabilities */}
      <View style={styles.card}>
        <Text style={styles.cardTitle}>Capabilities</Text>
        {[
          { icon: 'finger-print-outline' as const, title: 'Fingerprint Matching', desc: 'ABIS engine with real template comparison' },
          { icon: 'person-circle-outline' as const, title: 'Face Recognition', desc: 'ArcFace 512-d embeddings with InsightFace' },
          { icon: 'shield-checkmark-outline' as const, title: 'Liveness Detection', desc: 'CDCN (Central Difference CNN) anti-spoofing' },
          { icon: 'copy-outline' as const, title: 'Deduplication', desc: 'Cross-reference biometric profiles for duplicates' },
        ].map((cap) => (
          <View key={cap.title} style={styles.capRow}>
            <Ionicons name={cap.icon} size={20} color="#166534" />
            <View style={{ flex: 1, marginLeft: 10 }}>
              <Text style={{ fontSize: 14, fontWeight: '600', color: '#111827' }}>{cap.title}</Text>
              <Text style={styles.muted}>{cap.desc}</Text>
            </View>
          </View>
        ))}
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb', padding: 16 },
  card: { backgroundColor: '#fff', borderRadius: 16, padding: 16, marginBottom: 16, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  cardHeader: { flexDirection: 'row', alignItems: 'center', gap: 10, marginBottom: 12 },
  cardTitle: { fontSize: 16, fontWeight: '700', color: '#111827' },
  muted: { fontSize: 13, color: '#9ca3af', marginBottom: 8 },
  button: { backgroundColor: '#166534', borderRadius: 12, padding: 14, alignItems: 'center', marginTop: 8 },
  buttonText: { color: '#fff', fontSize: 15, fontWeight: '600' },
  statsGrid: { flexDirection: 'row', gap: 8, marginTop: 12 },
  statCard: { flex: 1, backgroundColor: '#f9fafb', borderRadius: 10, padding: 12, alignItems: 'center' },
  statNumber: { fontSize: 20, fontWeight: '700', color: '#111827' },
  statLabel: { fontSize: 11, color: '#6b7280', marginTop: 2 },
  resultBanner: { flexDirection: 'row', alignItems: 'center', borderRadius: 12, padding: 14, marginTop: 12 },
  scoreRow: { flexDirection: 'row', gap: 10, marginTop: 4 },
  scoreLabel: { fontSize: 12, color: '#6b7280' },
  capRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 10, borderBottomWidth: 1, borderBottomColor: '#f3f4f6' },
});
