// GOTV Segments — dynamic contact segmentation with filter builder.

import { useState, useCallback } from 'react';
import {
  View, Text, StyleSheet, ScrollView, TouchableOpacity,
  RefreshControl, Platform,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { useFocusEffect } from 'expo-router';
import { EmptyState } from '../src/components/EmptyState';
import { CardSkeleton } from '../src/components/SkeletonLoader';
import { gotvFetch } from '../lib/gotv-auth';

interface Segment {
  segment_id: string;
  name: string;
  description: string;
  filter_definition: Record<string, unknown>;
  contact_count: number;
  created_at: string;
}

export default function GOTVSegmentsScreen() {
  const [segments, setSegments] = useState<Segment[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = useCallback(async () => {
    try {
      const data = await gotvFetch<{ segments: Segment[] }>('/gotv/segments');
      setSegments(data.segments || []);
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

  if (loading) return <CardSkeleton />;

  return (
    <ScrollView
      style={styles.container}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor="#006b3f" />}
    >
      <View style={styles.header}>
        <Text style={styles.title}>Segments</Text>
        <Text style={styles.description}>Dynamic audience targeting</Text>
      </View>

      {segments.length === 0 ? (
        <EmptyState icon="funnel-outline" title="No segments" description="Create segments from the web portal" />
      ) : (
        segments.map(s => {
          const filters = Object.keys(s.filter_definition || {});
          return (
            <View key={s.segment_id} style={styles.card}>
              <View style={styles.cardHeader}>
                <View style={styles.iconCircle}>
                  <Ionicons name="people" size={20} color="#006b3f" />
                </View>
                <View style={styles.cardContent}>
                  <Text style={styles.cardTitle}>{s.name}</Text>
                  {s.description && <Text style={styles.cardDesc} numberOfLines={2}>{s.description}</Text>}
                </View>
                <View style={styles.countBadge}>
                  <Text style={styles.countText}>{s.contact_count.toLocaleString()}</Text>
                  <Text style={styles.countLabel}>contacts</Text>
                </View>
              </View>
              {filters.length > 0 && (
                <View style={styles.filtersRow}>
                  {filters.map(f => (
                    <View key={f} style={styles.filterChip}>
                      <Ionicons name="filter" size={10} color="#6b7280" />
                      <Text style={styles.filterText}>{f}</Text>
                    </View>
                  ))}
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
  header: { padding: 16, paddingBottom: 12 },
  title: { fontSize: 22, fontWeight: '700', color: '#111827' },
  description: { fontSize: 13, color: '#6b7280', marginTop: 2 },
  card: { backgroundColor: '#fff', marginHorizontal: 16, marginBottom: 12, borderRadius: 12, padding: 14, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 3, elevation: 2 },
  cardHeader: { flexDirection: 'row', alignItems: 'center' },
  iconCircle: { width: 40, height: 40, borderRadius: 20, backgroundColor: '#f0fdf4', alignItems: 'center', justifyContent: 'center' },
  cardContent: { flex: 1, marginLeft: 12 },
  cardTitle: { fontSize: 15, fontWeight: '600', color: '#111827' },
  cardDesc: { fontSize: 12, color: '#6b7280', marginTop: 2 },
  countBadge: { alignItems: 'center', backgroundColor: '#f3f4f6', paddingHorizontal: 10, paddingVertical: 6, borderRadius: 8 },
  countText: { fontSize: 16, fontWeight: '700', color: '#006b3f' },
  countLabel: { fontSize: 10, color: '#6b7280' },
  filtersRow: { flexDirection: 'row', flexWrap: 'wrap', marginTop: 10, gap: 6 },
  filterChip: { flexDirection: 'row', alignItems: 'center', backgroundColor: '#f9fafb', paddingHorizontal: 8, paddingVertical: 4, borderRadius: 6, borderWidth: 1, borderColor: '#e5e7eb', gap: 4 },
  filterText: { fontSize: 11, color: '#6b7280' },
});
