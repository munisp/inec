// GOTV Rides — manage ride requests to get voters to polling units.

import { useState, useCallback } from 'react';
import {
  View, Text, StyleSheet, ScrollView, TouchableOpacity,
  RefreshControl, Platform, Alert,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { useFocusEffect } from 'expo-router';
import { EmptyState } from '../src/components/EmptyState';
import { CardSkeleton } from '../src/components/SkeletonLoader';
import { gotvFetch } from '../lib/gotv-auth';

interface Ride {
  request_id: string;
  contact_name: string;
  polling_unit_code: string;
  status: string;
  pickup_address: string;
  distance_km: number | null;
  volunteer_name: string | null;
  scheduled_time: string | null;
}

const STATUS_CONFIG: Record<string, { color: string; bg: string; icon: string }> = {
  pending: { color: '#f59e0b', bg: '#fffbeb', icon: 'time' },
  matched: { color: '#3b82f6', bg: '#dbeafe', icon: 'car' },
  en_route: { color: '#6366f1', bg: '#eef2ff', icon: 'navigate' },
  picked_up: { color: '#8b5cf6', bg: '#f5f3ff', icon: 'person' },
  dropped_off: { color: '#22c55e', bg: '#f0fdf4', icon: 'checkmark-circle' },
  cancelled: { color: '#ef4444', bg: '#fef2f2', icon: 'close-circle' },
  no_show: { color: '#6b7280', bg: '#f3f4f6', icon: 'alert-circle' },
};

export default function GOTVRidesScreen() {
  const [rides, setRides] = useState<Ride[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = useCallback(async () => {
    try {
      const data = await gotvFetch<{ rides: Ride[] }>('/gotv/rides');
      setRides(data.rides || []);
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

  const updateStatus = async (id: string, status: string) => {
    try {
      await gotvFetch(`/gotv/rides/${id}/status`, {
        method: 'PATCH',
        body: JSON.stringify({ status }),
      });
      await load();
    } catch (e) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Update failed');
    }
  };

  const stats = {
    pending: rides.filter(r => r.status === 'pending').length,
    active: rides.filter(r => ['matched', 'en_route', 'picked_up'].includes(r.status)).length,
    completed: rides.filter(r => r.status === 'dropped_off').length,
  };

  if (loading) return <CardSkeleton />;

  return (
    <ScrollView
      style={styles.container}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor="#006b3f" />}
    >
      <View style={styles.header}>
        <Text style={styles.title}>Ride Requests</Text>
        <Text style={styles.description}>{rides.length} total requests</Text>
      </View>

      <View style={styles.statsRow}>
        <View style={[styles.statCard, { borderTopColor: '#f59e0b' }]}>
          <Ionicons name="time" size={22} color="#f59e0b" />
          <Text style={styles.statNum}>{stats.pending}</Text>
          <Text style={styles.statLabel}>Pending</Text>
        </View>
        <View style={[styles.statCard, { borderTopColor: '#3b82f6' }]}>
          <Ionicons name="car" size={22} color="#3b82f6" />
          <Text style={styles.statNum}>{stats.active}</Text>
          <Text style={styles.statLabel}>In Progress</Text>
        </View>
        <View style={[styles.statCard, { borderTopColor: '#22c55e' }]}>
          <Ionicons name="checkmark-circle" size={22} color="#22c55e" />
          <Text style={styles.statNum}>{stats.completed}</Text>
          <Text style={styles.statLabel}>Completed</Text>
        </View>
      </View>

      {rides.length === 0 ? (
        <EmptyState icon="car-outline" title="No ride requests" description="Ride requests will appear here on election day" />
      ) : (
        rides.map(r => {
          const st = STATUS_CONFIG[r.status] || STATUS_CONFIG.pending;
          return (
            <View key={r.request_id} style={styles.card}>
              <View style={styles.cardRow}>
                <View style={[styles.iconBox, { backgroundColor: st.bg }]}>
                  <Ionicons name={st.icon as any} size={20} color={st.color} />
                </View>
                <View style={styles.cardContent}>
                  <Text style={styles.name}>{r.contact_name || 'Voter'}</Text>
                  <Text style={styles.meta}>
                    PU: {r.polling_unit_code || 'N/A'}
                    {r.distance_km ? ` · ${r.distance_km.toFixed(1)} km` : ''}
                  </Text>
                  {r.volunteer_name && (
                    <Text style={styles.driver}>Driver: {r.volunteer_name}</Text>
                  )}
                </View>
                <View style={[styles.badge, { backgroundColor: st.bg }]}>
                  <Text style={[styles.badgeText, { color: st.color }]}>{r.status.replace('_', ' ')}</Text>
                </View>
              </View>
              {r.status === 'pending' && (
                <View style={styles.actions}>
                  <TouchableOpacity
                    style={styles.acceptBtn}
                    onPress={() => updateStatus(r.request_id, 'matched')}
                  >
                    <Ionicons name="car" size={14} color="#fff" />
                    <Text style={styles.acceptText}>Accept Ride</Text>
                  </TouchableOpacity>
                </View>
              )}
              {r.status === 'matched' && (
                <View style={styles.actions}>
                  <TouchableOpacity
                    style={[styles.progressBtn, { backgroundColor: '#eef2ff' }]}
                    onPress={() => updateStatus(r.request_id, 'en_route')}
                  >
                    <Text style={[styles.progressText, { color: '#4338ca' }]}>En Route</Text>
                  </TouchableOpacity>
                </View>
              )}
              {r.status === 'en_route' && (
                <View style={styles.actions}>
                  <TouchableOpacity
                    style={[styles.progressBtn, { backgroundColor: '#f5f3ff' }]}
                    onPress={() => updateStatus(r.request_id, 'picked_up')}
                  >
                    <Text style={[styles.progressText, { color: '#7c3aed' }]}>Picked Up</Text>
                  </TouchableOpacity>
                </View>
              )}
              {r.status === 'picked_up' && (
                <View style={styles.actions}>
                  <TouchableOpacity
                    style={[styles.progressBtn, { backgroundColor: '#f0fdf4' }]}
                    onPress={() => updateStatus(r.request_id, 'dropped_off')}
                  >
                    <Text style={[styles.progressText, { color: '#15803d' }]}>Drop Off Complete</Text>
                  </TouchableOpacity>
                </View>
              )}
            </View>
          );
        })
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  header: { padding: 16, paddingBottom: 8 },
  title: { fontSize: 22, fontWeight: '700', color: '#111827' },
  description: { fontSize: 13, color: '#6b7280', marginTop: 2 },
  statsRow: { flexDirection: 'row', paddingHorizontal: 16, gap: 10, marginBottom: 14 },
  statCard: { flex: 1, backgroundColor: '#fff', padding: 12, borderRadius: 10, alignItems: 'center', borderTopWidth: 3, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.04, shadowRadius: 2, elevation: 1 },
  statNum: { fontSize: 20, fontWeight: '700', color: '#111827', marginTop: 4 },
  statLabel: { fontSize: 11, color: '#6b7280', marginTop: 2 },
  card: { backgroundColor: '#fff', marginHorizontal: 16, marginBottom: 10, borderRadius: 12, padding: 14, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 3, elevation: 2 },
  cardRow: { flexDirection: 'row', alignItems: 'center' },
  iconBox: { width: 40, height: 40, borderRadius: 10, alignItems: 'center', justifyContent: 'center' },
  cardContent: { flex: 1, marginLeft: 12 },
  name: { fontSize: 15, fontWeight: '600', color: '#111827' },
  meta: { fontSize: 12, color: '#6b7280', marginTop: 2 },
  driver: { fontSize: 12, color: '#006b3f', fontWeight: '500', marginTop: 2 },
  badge: { paddingHorizontal: 8, paddingVertical: 3, borderRadius: 6 },
  badgeText: { fontSize: 11, fontWeight: '600', textTransform: 'capitalize' },
  actions: { flexDirection: 'row', marginTop: 10, gap: 8 },
  acceptBtn: { flexDirection: 'row', alignItems: 'center', backgroundColor: '#006b3f', paddingHorizontal: 14, paddingVertical: 8, borderRadius: 8, gap: 6 },
  acceptText: { color: '#fff', fontWeight: '600', fontSize: 13 },
  progressBtn: { paddingHorizontal: 14, paddingVertical: 8, borderRadius: 8 },
  progressText: { fontWeight: '600', fontSize: 13 },
});
