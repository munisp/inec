// GOTV Pledges — track voter pledges with status management.

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

interface Pledge {
  pledge_id: string;
  contact_id: string;
  contact_name: string;
  pledge_type: string;
  status: string;
  polling_unit_code: string;
  pledged_at: string;
  confirmed_at: string | null;
}

const STATUS_COLORS: Record<string, { color: string; bg: string; icon: string }> = {
  pledged: { color: '#f59e0b', bg: '#fffbeb', icon: 'hand-right' },
  reminded: { color: '#3b82f6', bg: '#dbeafe', icon: 'notifications' },
  confirmed: { color: '#22c55e', bg: '#f0fdf4', icon: 'checkmark-circle' },
  fulfilled: { color: '#8b5cf6', bg: '#f5f3ff', icon: 'trophy' },
  cancelled: { color: '#ef4444', bg: '#fef2f2', icon: 'close-circle' },
};

export default function GOTVPledgesScreen() {
  const [pledges, setPledges] = useState<Pledge[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [filter, setFilter] = useState<string>('all');

  const load = useCallback(async () => {
    try {
      const data = await gotvFetch<{ pledges: Pledge[] }>('/gotv/pledges');
      setPledges(data.pledges || []);
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
      await gotvFetch(`/gotv/pledges/${id}`, {
        method: 'PATCH',
        body: JSON.stringify({ status }),
      });
      await load();
    } catch (e) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Update failed');
    }
  };

  const filtered = filter === 'all' ? pledges : pledges.filter(p => p.status === filter);

  const stats = {
    total: pledges.length,
    confirmed: pledges.filter(p => p.status === 'confirmed' || p.status === 'fulfilled').length,
    pending: pledges.filter(p => p.status === 'pledged' || p.status === 'reminded').length,
  };

  if (loading) return <CardSkeleton />;

  return (
    <ScrollView
      style={styles.container}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor="#006b3f" />}
    >
      <View style={styles.header}>
        <Text style={styles.title}>Pledges</Text>
      </View>

      <View style={styles.statsRow}>
        <View style={[styles.statCard, { borderLeftColor: '#006b3f' }]}>
          <Text style={styles.statNum}>{stats.total}</Text>
          <Text style={styles.statLabel}>Total</Text>
        </View>
        <View style={[styles.statCard, { borderLeftColor: '#22c55e' }]}>
          <Text style={styles.statNum}>{stats.confirmed}</Text>
          <Text style={styles.statLabel}>Confirmed</Text>
        </View>
        <View style={[styles.statCard, { borderLeftColor: '#f59e0b' }]}>
          <Text style={styles.statNum}>{stats.pending}</Text>
          <Text style={styles.statLabel}>Pending</Text>
        </View>
      </View>

      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.filterRow}>
        {['all', 'pledged', 'reminded', 'confirmed', 'fulfilled', 'cancelled'].map(f => (
          <TouchableOpacity
            key={f}
            style={[styles.chip, filter === f && styles.chipActive]}
            onPress={() => setFilter(f)}
          >
            <Text style={[styles.chipText, filter === f && styles.chipTextActive]}>
              {f.charAt(0).toUpperCase() + f.slice(1)}
            </Text>
          </TouchableOpacity>
        ))}
      </ScrollView>

      {filtered.length === 0 ? (
        <EmptyState icon="thumbs-up-outline" title="No pledges" description="Pledges will appear here" />
      ) : (
        filtered.map(p => {
          const st = STATUS_COLORS[p.status] || STATUS_COLORS.pledged;
          return (
            <View key={p.pledge_id} style={styles.card}>
              <View style={styles.cardRow}>
                <View style={[styles.iconBox, { backgroundColor: st.bg }]}>
                  <Ionicons name={st.icon as any} size={20} color={st.color} />
                </View>
                <View style={styles.cardContent}>
                  <Text style={styles.name} numberOfLines={1}>{p.contact_name || 'Contact'}</Text>
                  <Text style={styles.meta}>{p.pledge_type} · PU: {p.polling_unit_code || 'N/A'}</Text>
                </View>
                <View style={[styles.badge, { backgroundColor: st.bg }]}>
                  <Text style={[styles.badgeText, { color: st.color }]}>{p.status}</Text>
                </View>
              </View>
              {(p.status === 'pledged' || p.status === 'reminded') && (
                <View style={styles.actions}>
                  <TouchableOpacity
                    style={[styles.actionBtn, { backgroundColor: '#dbeafe' }]}
                    onPress={() => updateStatus(p.pledge_id, 'reminded')}
                  >
                    <Text style={[styles.actionText, { color: '#1d4ed8' }]}>Remind</Text>
                  </TouchableOpacity>
                  <TouchableOpacity
                    style={[styles.actionBtn, { backgroundColor: '#f0fdf4' }]}
                    onPress={() => updateStatus(p.pledge_id, 'confirmed')}
                  >
                    <Text style={[styles.actionText, { color: '#15803d' }]}>Confirm</Text>
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
  statsRow: { flexDirection: 'row', paddingHorizontal: 16, gap: 10, marginBottom: 12 },
  statCard: { flex: 1, backgroundColor: '#fff', padding: 12, borderRadius: 10, borderLeftWidth: 3, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.04, shadowRadius: 2, elevation: 1 },
  statNum: { fontSize: 20, fontWeight: '700', color: '#111827' },
  statLabel: { fontSize: 11, color: '#6b7280', marginTop: 2 },
  filterRow: { paddingHorizontal: 16, marginBottom: 12, maxHeight: 36 },
  chip: { paddingHorizontal: 14, paddingVertical: 6, borderRadius: 16, backgroundColor: '#f3f4f6', marginRight: 8 },
  chipActive: { backgroundColor: '#006b3f' },
  chipText: { fontSize: 13, color: '#374151' },
  chipTextActive: { color: '#fff', fontWeight: '600' },
  card: { backgroundColor: '#fff', marginHorizontal: 16, marginBottom: 10, borderRadius: 12, padding: 14, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 3, elevation: 2 },
  cardRow: { flexDirection: 'row', alignItems: 'center' },
  iconBox: { width: 40, height: 40, borderRadius: 10, alignItems: 'center', justifyContent: 'center' },
  cardContent: { flex: 1, marginLeft: 12 },
  name: { fontSize: 15, fontWeight: '600', color: '#111827' },
  meta: { fontSize: 12, color: '#6b7280', marginTop: 2 },
  badge: { paddingHorizontal: 8, paddingVertical: 3, borderRadius: 6 },
  badgeText: { fontSize: 11, fontWeight: '600', textTransform: 'capitalize' },
  actions: { flexDirection: 'row', marginTop: 10, gap: 8 },
  actionBtn: { paddingHorizontal: 14, paddingVertical: 6, borderRadius: 8 },
  actionText: { fontSize: 13, fontWeight: '600' },
});
