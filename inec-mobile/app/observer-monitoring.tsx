import { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api as apiCall } from '../src/lib/api';

interface ObserverStats {
  total_observers: number;
  active_now: number;
  reports_submitted: number;
  incidents_flagged: number;
}

export default function ObserverMonitoringScreen() {
  const [stats, setStats] = useState<ObserverStats | null>(null);
  const [loading, setLoading] = useState(false);

  const loadStats = async () => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    try {
      const res = await apiCall<ObserverStats>('/observers/stats');
      setStats(res);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed to load observer stats');
    }
    setLoading(false);
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: 40 }}>
      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="eye-outline" size={24} color="#166534" />
          <Text style={styles.cardTitle}>Observer Monitoring</Text>
        </View>
        <Text style={styles.muted}>Track accredited election observers across all polling units in real-time.</Text>
        <TouchableOpacity style={styles.button} onPress={loadStats} disabled={loading} activeOpacity={0.8}>
          <Text style={styles.buttonText}>{loading ? 'Loading...' : 'Load Observer Data'}</Text>
        </TouchableOpacity>
        {stats && (
          <View style={styles.statsGrid}>
            <View style={styles.statCard}>
              <Text style={styles.statNumber}>{stats.total_observers}</Text>
              <Text style={styles.statLabel}>Observers</Text>
            </View>
            <View style={styles.statCard}>
              <Text style={[styles.statNumber, { color: '#166534' }]}>{stats.active_now}</Text>
              <Text style={styles.statLabel}>Active Now</Text>
            </View>
            <View style={styles.statCard}>
              <Text style={[styles.statNumber, { color: '#2563eb' }]}>{stats.reports_submitted}</Text>
              <Text style={styles.statLabel}>Reports</Text>
            </View>
            <View style={styles.statCard}>
              <Text style={[styles.statNumber, { color: '#dc2626' }]}>{stats.incidents_flagged}</Text>
              <Text style={styles.statLabel}>Incidents</Text>
            </View>
          </View>
        )}
      </View>

      <View style={styles.card}>
        <Text style={styles.cardTitle}>Observer Features</Text>
        {[
          { icon: 'location-outline' as const, title: 'GPS Tracking', desc: 'Real-time location of accredited observers' },
          { icon: 'camera-outline' as const, title: 'Photo Reports', desc: 'Upload evidence photos from polling units' },
          { icon: 'alert-circle-outline' as const, title: 'Incident Reporting', desc: 'Flag irregularities with severity classification' },
          { icon: 'time-outline' as const, title: 'Activity Timeline', desc: 'Chronological activity log per observer' },
        ].map((f) => (
          <View key={f.title} style={styles.capRow}>
            <Ionicons name={f.icon} size={20} color="#166534" />
            <View style={{ flex: 1, marginLeft: 10 }}>
              <Text style={{ fontSize: 14, fontWeight: '600', color: '#111827' }}>{f.title}</Text>
              <Text style={styles.muted}>{f.desc}</Text>
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
  statsGrid: { flexDirection: 'row', gap: 8, marginTop: 12, flexWrap: 'wrap' },
  statCard: { width: '48%', backgroundColor: '#f9fafb', borderRadius: 10, padding: 12, alignItems: 'center', marginBottom: 8 },
  statNumber: { fontSize: 20, fontWeight: '700', color: '#111827' },
  statLabel: { fontSize: 11, color: '#6b7280', marginTop: 2 },
  capRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 10, borderBottomWidth: 1, borderBottomColor: '#f3f4f6' },
});
