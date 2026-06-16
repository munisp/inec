// GOTV Leaderboard — Shows top volunteers with gamification badges.
// Auth: standalone GOTV mobile JWT.

import { useState, useEffect } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, FlatList } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { router } from 'expo-router';
import { gotvFetch } from '../lib/gotv-auth';

interface LeaderboardEntry {
  volunteer_id: string;
  full_name: string;
  role: string;
  score: number;
  rank: number;
  badge: string;
  doors_knocked: number;
  calls_made: number;
  rides_given: number;
}

const BADGE_CONFIG: Record<string, { icon: string; color: string; label: string }> = {
  champion: { icon: 'trophy', color: '#eab308', label: 'Champion' },
  top_performer: { icon: 'medal', color: '#3b82f6', label: 'Top Performer' },
  all_star: { icon: 'star', color: '#8b5cf6', label: 'All Star' },
};

const PERIODS = ['daily', 'weekly', 'monthly', 'all'] as const;

export default function GOTVLeaderboardScreen() {
  const [entries, setEntries] = useState<LeaderboardEntry[]>([]);
  const [period, setPeriod] = useState<typeof PERIODS[number]>('weekly');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    loadLeaderboard();
  }, [period]);

  const loadLeaderboard = async () => {
    setLoading(true);
    try {
      const data = await gotvFetch<{ entries: LeaderboardEntry[] }>(`/gotv/leaderboard?period=${period}&limit=50`);
      if (data && data.entries) {
        setEntries(data.entries);
      }
    } catch {
      setEntries([]);
    } finally {
      setLoading(false);
    }
  };

  const renderItem = ({ item }: { item: LeaderboardEntry }) => {
    const badge = BADGE_CONFIG[item.badge];
    const isTop3 = item.rank <= 3;
    const rankColor = item.rank === 1 ? '#eab308' : item.rank === 2 ? '#9ca3af' : item.rank === 3 ? '#cd7f32' : '#6b7280';

    return (
      <View style={[styles.entry, isTop3 && styles.top3Entry]}>
        <View style={[styles.rankCircle, { borderColor: rankColor }]}>
          <Text style={[styles.rankText, { color: rankColor }]}>#{item.rank}</Text>
        </View>
        <View style={styles.entryInfo}>
          <View style={styles.nameRow}>
            <Text style={styles.entryName}>{item.full_name}</Text>
            <View style={styles.roleBadge}>
              <Text style={styles.roleBadgeText}>{item.role}</Text>
            </View>
            {badge && (
              <View style={[styles.gameBadge, { backgroundColor: badge.color + '20' }]}>
                <Ionicons name={badge.icon as any} size={12} color={badge.color} />
                <Text style={[styles.gameBadgeText, { color: badge.color }]}>{badge.label}</Text>
              </View>
            )}
          </View>
          <View style={styles.statsRow}>
            <Text style={styles.stat}>🚪 {item.doors_knocked}</Text>
            <Text style={styles.stat}>📞 {item.calls_made}</Text>
            <Text style={styles.stat}>🚗 {item.rides_given}</Text>
          </View>
        </View>
        <View style={styles.scoreContainer}>
          <Text style={styles.scoreValue}>{item.score}</Text>
          <Text style={styles.scoreLabel}>pts</Text>
        </View>
      </View>
    );
  };

  return (
    <View style={styles.container}>
      <View style={styles.header}>
        <TouchableOpacity onPress={() => router.back()}>
          <Ionicons name="arrow-back" size={24} color="#1f2937" />
        </TouchableOpacity>
        <Text style={styles.headerTitle}>Leaderboard</Text>
        <Ionicons name="trophy" size={24} color="#eab308" />
      </View>

      <View style={styles.periodBar}>
        {PERIODS.map(p => (
          <TouchableOpacity
            key={p}
            style={[styles.periodBtn, period === p && styles.periodBtnActive]}
            onPress={() => setPeriod(p)}
          >
            <Text style={[styles.periodText, period === p && styles.periodTextActive]}>
              {p.charAt(0).toUpperCase() + p.slice(1)}
            </Text>
          </TouchableOpacity>
        ))}
      </View>

      {loading ? (
        <View style={styles.center}>
          <Text style={styles.loadingText}>Loading leaderboard...</Text>
        </View>
      ) : entries.length === 0 ? (
        <View style={styles.center}>
          <Ionicons name="trophy-outline" size={48} color="#9ca3af" />
          <Text style={styles.emptyText}>No activity for this period</Text>
        </View>
      ) : (
        <FlatList
          data={entries}
          keyExtractor={item => item.volunteer_id}
          renderItem={renderItem}
          contentContainerStyle={styles.list}
        />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  header: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', padding: 16, paddingTop: 56, backgroundColor: '#fff', borderBottomWidth: 1, borderBottomColor: '#e5e7eb' },
  headerTitle: { fontSize: 18, fontWeight: '700', color: '#1f2937' },
  periodBar: { flexDirection: 'row', padding: 8, backgroundColor: '#fff', gap: 4 },
  periodBtn: { flex: 1, paddingVertical: 8, alignItems: 'center', borderRadius: 8, backgroundColor: '#f3f4f6' },
  periodBtnActive: { backgroundColor: '#3b82f6' },
  periodText: { fontSize: 13, fontWeight: '500', color: '#6b7280' },
  periodTextActive: { color: '#fff' },
  center: { flex: 1, justifyContent: 'center', alignItems: 'center' },
  loadingText: { color: '#9ca3af', marginTop: 8 },
  emptyText: { color: '#9ca3af', marginTop: 12, fontSize: 14 },
  list: { padding: 12, gap: 8 },
  entry: { backgroundColor: '#fff', borderRadius: 12, padding: 12, flexDirection: 'row', alignItems: 'center', borderWidth: 1, borderColor: '#e5e7eb' },
  top3Entry: { borderColor: '#fbbf2440', backgroundColor: '#fffbeb' },
  rankCircle: { width: 40, height: 40, borderRadius: 20, borderWidth: 2, justifyContent: 'center', alignItems: 'center' },
  rankText: { fontSize: 14, fontWeight: '700' },
  entryInfo: { flex: 1, marginLeft: 12 },
  nameRow: { flexDirection: 'row', alignItems: 'center', gap: 6, flexWrap: 'wrap' },
  entryName: { fontSize: 15, fontWeight: '600', color: '#1f2937' },
  roleBadge: { backgroundColor: '#f3f4f6', paddingHorizontal: 6, paddingVertical: 2, borderRadius: 4 },
  roleBadgeText: { fontSize: 10, color: '#6b7280' },
  gameBadge: { flexDirection: 'row', alignItems: 'center', gap: 2, paddingHorizontal: 6, paddingVertical: 2, borderRadius: 4 },
  gameBadgeText: { fontSize: 10, fontWeight: '500' },
  statsRow: { flexDirection: 'row', gap: 12, marginTop: 4 },
  stat: { fontSize: 12, color: '#6b7280' },
  scoreContainer: { alignItems: 'center' },
  scoreValue: { fontSize: 20, fontWeight: '700', color: '#1f2937' },
  scoreLabel: { fontSize: 10, color: '#9ca3af' },
});
