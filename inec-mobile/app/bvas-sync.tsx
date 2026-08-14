import { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api as apiCall } from '../src/lib/api';

// R4-52: /bvas/sync/status never existed; real route is GET /ems/sync/stats.
interface SyncStatus {
  total: number;
  synced: number;
  queued: number;
  conflicts: number;
  failed: number;
  offline_devices: number;
}

export default function BVASSyncScreen() {
  const [syncStatus, setSyncStatus] = useState<SyncStatus | null>(null);
  const [loading, setLoading] = useState(false);

  const loadSyncStatus = async () => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    try {
      const res = await apiCall<SyncStatus>('/ems/sync/stats');
      setSyncStatus(res);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed to load sync status');
    }
    setLoading(false);
  };

  // R4-52: /bvas/sync/trigger never existed. Server-side sync is device-push
  // (POST /ems/sync/submit from BVAS devices); there is no central "trigger
  // all devices" action, so this screen is read-only and says so honestly.

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
              <Text style={styles.statNumber}>{syncStatus.total}</Text>
              <Text style={styles.statLabel}>Queue Items</Text>
            </View>
            <View style={[styles.statCard]}>
              <Text style={[styles.statNumber, { color: '#166534' }]}>{syncStatus.synced}</Text>
              <Text style={styles.statLabel}>Synced</Text>
            </View>
            <View style={styles.statCard}>
              <Text style={[styles.statNumber, { color: '#f59e0b' }]}>{syncStatus.queued}</Text>
              <Text style={styles.statLabel}>Queued</Text>
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
        <Text style={styles.muted}>Sync is initiated by BVAS devices pushing to the server; there is no central trigger. Conflicts are resolved from the web console (BVAS Sync page).</Text>
        {syncStatus && (
          <Text style={styles.muted}>Conflicts awaiting resolution: {syncStatus.conflicts} · Offline devices: {syncStatus.offline_devices}</Text>
        )}
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
