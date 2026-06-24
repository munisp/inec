import { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api as apiCall } from '../src/lib/api';

interface DedupStatus {
  total_scanned: number;
  duplicates_found: number;
  resolved: number;
  pending_review: number;
  last_scan: string;
}

export default function DuplicateDetectionScreen() {
  const [status, setStatus] = useState<DedupStatus | null>(null);
  const [loading, setLoading] = useState(false);

  const loadStatus = async () => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    try {
      const res = await apiCall<DedupStatus>('/biometric/engine/dedup/status');
      setStatus(res);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed to load dedup status');
    }
    setLoading(false);
  };

  const startScan = async () => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Heavy);
    try {
      await apiCall('/biometric/engine/dedup/start', { method: 'POST' });
      Alert.alert('Scan Started', 'Deduplication scan has been initiated.');
      await loadStatus();
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Scan trigger failed');
    }
    setLoading(false);
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: 40 }}>
      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="copy-outline" size={24} color="#dc2626" />
          <Text style={styles.cardTitle}>Duplicate Voter Detection</Text>
        </View>
        <Text style={styles.muted}>ABIS deduplication engine scans biometric profiles across the entire voter register to detect duplicate enrollments.</Text>
        <TouchableOpacity style={styles.button} onPress={loadStatus} disabled={loading} activeOpacity={0.8}>
          <Text style={styles.buttonText}>{loading ? 'Loading...' : 'Check Dedup Status'}</Text>
        </TouchableOpacity>
        {status && (
          <View style={styles.statsGrid}>
            <View style={styles.statCard}>
              <Text style={styles.statNumber}>{status.total_scanned}</Text>
              <Text style={styles.statLabel}>Scanned</Text>
            </View>
            <View style={styles.statCard}>
              <Text style={[styles.statNumber, { color: '#dc2626' }]}>{status.duplicates_found}</Text>
              <Text style={styles.statLabel}>Duplicates</Text>
            </View>
            <View style={styles.statCard}>
              <Text style={[styles.statNumber, { color: '#166534' }]}>{status.resolved}</Text>
              <Text style={styles.statLabel}>Resolved</Text>
            </View>
            <View style={styles.statCard}>
              <Text style={[styles.statNumber, { color: '#f59e0b' }]}>{status.pending_review}</Text>
              <Text style={styles.statLabel}>Pending</Text>
            </View>
          </View>
        )}
      </View>

      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="scan-circle-outline" size={24} color="#7c3aed" />
          <Text style={styles.cardTitle}>Manual Scan</Text>
        </View>
        <Text style={styles.muted}>Trigger a full or incremental biometric deduplication scan using LSH blocking.</Text>
        <TouchableOpacity style={[styles.button, { backgroundColor: '#7c3aed' }]} onPress={startScan} disabled={loading} activeOpacity={0.8}>
          <Text style={styles.buttonText}>Start Dedup Scan</Text>
        </TouchableOpacity>
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
  statsGrid: { flexDirection: 'row', gap: 8, marginTop: 12, flexWrap: 'wrap' },
  statCard: { width: '48%', backgroundColor: '#f9fafb', borderRadius: 10, padding: 12, alignItems: 'center', marginBottom: 8 },
  statNumber: { fontSize: 20, fontWeight: '700', color: '#111827' },
  statLabel: { fontSize: 11, color: '#6b7280', marginTop: 2 },
});
