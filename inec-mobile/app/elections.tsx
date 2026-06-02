import { useState, useCallback } from 'react';
import {
  View, Text, StyleSheet, ScrollView, TouchableOpacity,
  RefreshControl, Platform,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { useFocusEffect, router } from 'expo-router';
import { electionApi, Election } from '../src/lib/api';
import { EmptyState } from '../src/components/EmptyState';
import { CardSkeleton } from '../src/components/SkeletonLoader';

const STATUS_COLORS: Record<string, { color: string; bg: string }> = {
  active: { color: '#22c55e', bg: '#f0fdf4' },
  upcoming: { color: '#2563eb', bg: '#dbeafe' },
  completed: { color: '#6b7280', bg: '#f9fafb' },
  cancelled: { color: '#ef4444', bg: '#fef2f2' },
};

export default function ElectionsScreen() {
  const [elections, setElections] = useState<Election[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const loadElections = useCallback(async () => {
    try {
      const data = await electionApi.list();
      setElections(data);
    } catch { /* */ }
    setLoading(false);
  }, []);

  useFocusEffect(useCallback(() => { loadElections(); }, [loadElections]));

  const onRefresh = useCallback(async () => {
    setRefreshing(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    await loadElections();
    setRefreshing(false);
  }, [loadElections]);

  if (loading) return <View style={styles.container}><CardSkeleton /><CardSkeleton /><CardSkeleton /></View>;

  return (
    <ScrollView
      style={styles.container}
      contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} colors={['#166534']} tintColor="#166534" />}
    >
      <View style={styles.header}>
        <Ionicons name="podium" size={20} color="#166534" />
        <Text style={styles.headerTitle}>{elections.length} Elections</Text>
      </View>

      {elections.length === 0 ? (
        <EmptyState icon="podium-outline" title="No Elections" description="No elections are currently available" />
      ) : (
        elections.map((e) => {
          const progress = e.total_polling_units > 0 ? (e.results_submitted / e.total_polling_units) * 100 : 0;
          const cfg = STATUS_COLORS[e.status] || STATUS_COLORS.upcoming;
          return (
            <TouchableOpacity
              key={e.id}
              style={styles.electionCard}
              activeOpacity={0.7}
              onPress={() => {
                Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                router.push({ pathname: '/results', params: { election_id: e.id, election_name: e.name } } as never);
              }}
            >
              <View style={styles.cardHeader}>
                <View style={{ flex: 1 }}>
                  <Text style={styles.electionName}>{e.name}</Text>
                  <Text style={styles.electionType}>{e.type} Election</Text>
                </View>
                <View style={[styles.statusBadge, { backgroundColor: cfg.bg }]}>
                  <View style={[styles.statusDot, { backgroundColor: cfg.color }]} />
                  <Text style={[styles.statusText, { color: cfg.color }]}>{e.status}</Text>
                </View>
              </View>

              <View style={styles.statsGrid}>
                <View style={styles.statItem}>
                  <Ionicons name="people-outline" size={16} color="#6b7280" />
                  <Text style={styles.statValue}>{(e.registered_voters || 0).toLocaleString()}</Text>
                  <Text style={styles.statLabel}>Voters</Text>
                </View>
                <View style={styles.statItem}>
                  <Ionicons name="location-outline" size={16} color="#6b7280" />
                  <Text style={styles.statValue}>{(e.total_polling_units || 0).toLocaleString()}</Text>
                  <Text style={styles.statLabel}>PUs</Text>
                </View>
                <View style={styles.statItem}>
                  <Ionicons name="document-text-outline" size={16} color="#6b7280" />
                  <Text style={styles.statValue}>{(e.results_submitted || 0).toLocaleString()}</Text>
                  <Text style={styles.statLabel}>Results</Text>
                </View>
              </View>

              {e.status === 'active' && (
                <View style={styles.progressSection}>
                  <View style={styles.progressHeader}>
                    <Text style={styles.progressLabel}>Reporting Progress</Text>
                    <Text style={styles.progressPct}>{progress.toFixed(1)}%</Text>
                  </View>
                  <View style={styles.progressTrack}>
                    <View style={[styles.progressFill, { width: `${Math.min(progress, 100)}%` }]} />
                  </View>
                </View>
              )}

              <View style={styles.cardFooter}>
                <View style={styles.dateRow}>
                  <Ionicons name="calendar-outline" size={14} color="#9ca3af" />
                  <Text style={styles.dateText}>{new Date(e.date).toLocaleDateString('en-NG', { year: 'numeric', month: 'long', day: 'numeric' })}</Text>
                </View>
                <Ionicons name="chevron-forward" size={16} color="#d1d5db" />
              </View>
            </TouchableOpacity>
          );
        })
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  header: { flexDirection: 'row', alignItems: 'center', gap: 8, padding: 16, paddingBottom: 8 },
  headerTitle: { fontSize: 15, fontWeight: '700', color: '#374151' },
  electionCard: { marginHorizontal: 16, marginBottom: 12, backgroundColor: '#fff', borderRadius: 16, padding: 16, shadowColor: '#000', shadowOffset: { width: 0, height: 2 }, shadowOpacity: 0.06, shadowRadius: 8, elevation: 3 },
  cardHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 14 },
  electionName: { fontSize: 16, fontWeight: '700', color: '#111827', marginBottom: 2 },
  electionType: { fontSize: 12, color: '#6b7280', textTransform: 'capitalize' },
  statusBadge: { flexDirection: 'row', alignItems: 'center', gap: 5, paddingHorizontal: 10, paddingVertical: 5, borderRadius: 10 },
  statusDot: { width: 6, height: 6, borderRadius: 3 },
  statusText: { fontSize: 11, fontWeight: '700', textTransform: 'capitalize' },
  statsGrid: { flexDirection: 'row', gap: 8, marginBottom: 14 },
  statItem: { flex: 1, backgroundColor: '#f9fafb', borderRadius: 10, padding: 10, alignItems: 'center', gap: 4 },
  statValue: { fontSize: 14, fontWeight: '800', color: '#111827' },
  statLabel: { fontSize: 10, color: '#9ca3af', fontWeight: '500' },
  progressSection: { marginBottom: 12 },
  progressHeader: { flexDirection: 'row', justifyContent: 'space-between', marginBottom: 6 },
  progressLabel: { fontSize: 12, color: '#6b7280', fontWeight: '500' },
  progressPct: { fontSize: 12, fontWeight: '700', color: '#166534' },
  progressTrack: { height: 6, backgroundColor: '#e5e7eb', borderRadius: 3, overflow: 'hidden' },
  progressFill: { height: '100%', backgroundColor: '#166534', borderRadius: 3 },
  cardFooter: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', paddingTop: 10, borderTopWidth: 1, borderTopColor: '#f3f4f6' },
  dateRow: { flexDirection: 'row', alignItems: 'center', gap: 4 },
  dateText: { fontSize: 12, color: '#9ca3af' },
});
