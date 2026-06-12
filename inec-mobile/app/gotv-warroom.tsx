// GOTV War Room — real-time operational overview for election day.

import { useState, useCallback } from 'react';
import {
  View, Text, StyleSheet, ScrollView, TouchableOpacity,
  RefreshControl, Platform,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { useFocusEffect } from 'expo-router';
import { CardSkeleton } from '../src/components/SkeletonLoader';
import { gotvFetch } from '../lib/gotv-auth';

interface WarRoomData {
  ops: {
    total_contacts: number;
    contacts_reached: number;
    pledges_confirmed: number;
    rides_completed: number;
    active_volunteers: number;
    doors_knocked: number;
    calls_made: number;
  };
  alerts: Array<{ type: string; message: string; timestamp: string }>;
  coverage: Array<{ state: string; contacts: number; reached_pct: number }>;
}

export default function GOTVWarRoomScreen() {
  const [data, setData] = useState<WarRoomData | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = useCallback(async () => {
    try {
      const result = await gotvFetch<WarRoomData>('/gotv/warroom');
      setData(result);
    } catch { /* empty */ }
    setLoading(false);
  }, []);

  useFocusEffect(useCallback(() => { load(); }, [load]));

  const onRefresh = useCallback(async () => {
    setRefreshing(true);
    if (Platform.OS !== 'web') Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    await load();
    setRefreshing(false);
  }, [load]);

  if (loading || !data) return <CardSkeleton />;

  const ops = data.ops;
  const reachPct = ops.total_contacts > 0 ? Math.round((ops.contacts_reached / ops.total_contacts) * 100) : 0;

  return (
    <ScrollView
      style={styles.container}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor="#006b3f" />}
    >
      <View style={styles.header}>
        <Ionicons name="radio" size={20} color="#ef4444" />
        <Text style={styles.title}>War Room</Text>
        <View style={styles.liveBadge}>
          <View style={styles.liveDot} />
          <Text style={styles.liveText}>LIVE</Text>
        </View>
      </View>

      {/* Overall reach progress */}
      <View style={styles.progressCard}>
        <Text style={styles.progressLabel}>Overall Reach</Text>
        <Text style={styles.progressValue}>{reachPct}%</Text>
        <View style={styles.progressBar}>
          <View style={[styles.progressFill, { width: `${Math.min(reachPct, 100)}%` }]} />
        </View>
        <Text style={styles.progressMeta}>
          {ops.contacts_reached.toLocaleString()} / {ops.total_contacts.toLocaleString()} contacts
        </Text>
      </View>

      {/* Key metrics grid */}
      <View style={styles.grid}>
        <View style={styles.metricCard}>
          <Ionicons name="checkmark-done" size={24} color="#22c55e" />
          <Text style={styles.metricNum}>{ops.pledges_confirmed}</Text>
          <Text style={styles.metricLabel}>Pledges</Text>
        </View>
        <View style={styles.metricCard}>
          <Ionicons name="car" size={24} color="#3b82f6" />
          <Text style={styles.metricNum}>{ops.rides_completed}</Text>
          <Text style={styles.metricLabel}>Rides Done</Text>
        </View>
        <View style={styles.metricCard}>
          <Ionicons name="people" size={24} color="#8b5cf6" />
          <Text style={styles.metricNum}>{ops.active_volunteers}</Text>
          <Text style={styles.metricLabel}>Active Vols</Text>
        </View>
        <View style={styles.metricCard}>
          <Ionicons name="home" size={24} color="#f59e0b" />
          <Text style={styles.metricNum}>{ops.doors_knocked}</Text>
          <Text style={styles.metricLabel}>Doors</Text>
        </View>
        <View style={styles.metricCard}>
          <Ionicons name="call" size={24} color="#06b6d4" />
          <Text style={styles.metricNum}>{ops.calls_made}</Text>
          <Text style={styles.metricLabel}>Calls</Text>
        </View>
      </View>

      {/* Alerts */}
      {data.alerts && data.alerts.length > 0 && (
        <View style={styles.section}>
          <Text style={styles.sectionTitle}>Alerts</Text>
          {data.alerts.slice(0, 5).map((a, i) => (
            <View key={i} style={styles.alertCard}>
              <Ionicons
                name={a.type === 'critical' ? 'warning' : 'information-circle'}
                size={18}
                color={a.type === 'critical' ? '#ef4444' : '#f59e0b'}
              />
              <Text style={styles.alertText} numberOfLines={2}>{a.message}</Text>
            </View>
          ))}
        </View>
      )}

      {/* Coverage by state */}
      {data.coverage && data.coverage.length > 0 && (
        <View style={styles.section}>
          <Text style={styles.sectionTitle}>Coverage by State</Text>
          {data.coverage.slice(0, 10).map((c, i) => (
            <View key={i} style={styles.coverageRow}>
              <Text style={styles.coverageState}>{c.state}</Text>
              <View style={styles.coverageBarBg}>
                <View style={[styles.coverageBarFill, { width: `${c.reached_pct || 0}%` }]} />
              </View>
              <Text style={styles.coveragePct}>{c.reached_pct || 0}%</Text>
            </View>
          ))}
        </View>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#0f172a' },
  header: { flexDirection: 'row', alignItems: 'center', padding: 16, gap: 8 },
  title: { fontSize: 22, fontWeight: '700', color: '#f1f5f9', flex: 1 },
  liveBadge: { flexDirection: 'row', alignItems: 'center', backgroundColor: '#1e293b', paddingHorizontal: 8, paddingVertical: 4, borderRadius: 12, gap: 4 },
  liveDot: { width: 6, height: 6, borderRadius: 3, backgroundColor: '#ef4444' },
  liveText: { fontSize: 10, fontWeight: '700', color: '#ef4444' },
  progressCard: { backgroundColor: '#1e293b', marginHorizontal: 16, borderRadius: 12, padding: 16, marginBottom: 14 },
  progressLabel: { fontSize: 13, color: '#94a3b8' },
  progressValue: { fontSize: 32, fontWeight: '800', color: '#22c55e', marginVertical: 4 },
  progressBar: { height: 6, backgroundColor: '#334155', borderRadius: 3, marginBottom: 6 },
  progressFill: { height: 6, backgroundColor: '#22c55e', borderRadius: 3 },
  progressMeta: { fontSize: 12, color: '#64748b' },
  grid: { flexDirection: 'row', flexWrap: 'wrap', paddingHorizontal: 12, gap: 8, marginBottom: 14 },
  metricCard: { width: '30%', backgroundColor: '#1e293b', borderRadius: 10, padding: 12, alignItems: 'center', flexGrow: 1 },
  metricNum: { fontSize: 18, fontWeight: '700', color: '#f1f5f9', marginTop: 6 },
  metricLabel: { fontSize: 11, color: '#94a3b8', marginTop: 2 },
  section: { paddingHorizontal: 16, marginBottom: 16 },
  sectionTitle: { fontSize: 15, fontWeight: '600', color: '#e2e8f0', marginBottom: 10 },
  alertCard: { flexDirection: 'row', alignItems: 'center', backgroundColor: '#1e293b', borderRadius: 8, padding: 10, marginBottom: 6, gap: 8 },
  alertText: { flex: 1, fontSize: 13, color: '#cbd5e1' },
  coverageRow: { flexDirection: 'row', alignItems: 'center', marginBottom: 8, gap: 8 },
  coverageState: { width: 50, fontSize: 12, color: '#94a3b8', fontWeight: '500' },
  coverageBarBg: { flex: 1, height: 6, backgroundColor: '#334155', borderRadius: 3 },
  coverageBarFill: { height: 6, backgroundColor: '#3b82f6', borderRadius: 3 },
  coveragePct: { width: 36, fontSize: 12, color: '#94a3b8', textAlign: 'right' },
});
