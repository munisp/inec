import { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api as apiCall } from '../src/lib/api';

interface SyncStatus {
  total_devices: number;
  synced: number;
  pending: number;
  failed: number;
  last_sync: string;
}

export default function BVASSyncScreen() {
  const [syncStatus, setSyncStatus] = useState<SyncStatus | null>(null);
  const [loading, setLoading] = useState(false);

  const loadSyncStatus = async () => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    try {
      const res = await apiCall<SyncStatus>('/bvas/sync/status');
      setSyncStatus(res);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed to load sync status');
    }
    setLoading(false);
  };

  const triggerSync = async () => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Heavy);
    try {
      await apiCall('/bvas/sync/trigger', { method: 'POST' });
      Alert.alert('Sync Started', 'BVAS synchronization has been triggered.');
      await loadSyncStatus();
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Sync trigger failed');
    }
    setLoading(false);
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: 40 }}>
      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="sync-outline" size={24} color="#166534" />
          <Text style={styles.cardTitle}>BVAS Device Sync</Text>
        </View>
        <Text style={styles.muted}>Synchronize accreditation data from BVAS devices to central server.</Text>
        <TouchableOpacity style={styles.button} onPress={loadSyncStatus} disabled={loading} activeOpacity={0.8}>
          <Text style={styles.buttonText}>{loading ? 'Loading...' : 'Check Sync Status'}</Text>
        </TouchableOpacity>
        {syncStatus && (
          <View style={styles.statsGrid}>
            <View style={styles.statCard}>
              <Text style={styles.statNumber}>{syncStatus.total_devices}</Text>
              <Text style={styles.statLabel}>Devices</Text>
            </View>
            <View style={[styles.statCard]}>
              <Text style={[styles.statNumber, { color: '#166534' }]}>{syncStatus.synced}</Text>
              <Text style={styles.statLabel}>Synced</Text>
            </View>
            <View style={styles.statCard}>
              <Text style={[styles.statNumber, { color: '#f59e0b' }]}>{syncStatus.pending}</Text>
              <Text style={styles.statLabel}>Pending</Text>
            </View>
            <View style={styles.statCard}>
              <Text style={[styles.statNumber, { color: '#dc2626' }]}>{syncStatus.failed}</Text>
              <Text style={styles.statLabel}>Failed</Text>
            </View>
          </View>
        )}
      </View>

      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="cloud-upload-outline" size={24} color="#2563eb" />
          <Text style={styles.cardTitle}>Manual Sync</Text>
        </View>
        <Text style={styles.muted}>Trigger manual synchronization for all pending BVAS devices.</Text>
        <TouchableOpacity style={[styles.button, { backgroundColor: '#2563eb' }]} onPress={triggerSync} disabled={loading} activeOpacity={0.8}>
          <Text style={styles.buttonText}>Trigger Sync Now</Text>
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
  statsGrid: { flexDirection: 'row', gap: 8, marginTop: 12 },
  statCard: { flex: 1, backgroundColor: '#f9fafb', borderRadius: 10, padding: 12, alignItems: 'center' },
  statNumber: { fontSize: 20, fontWeight: '700', color: '#111827' },
  statLabel: { fontSize: 11, color: '#6b7280', marginTop: 2 },
});
