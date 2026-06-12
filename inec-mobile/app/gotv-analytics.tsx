// GOTV Analytics & Scoring — channel ROI, voter scoring, win probability.

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

interface ChannelROI {
  channel: string;
  sent: number;
  delivered: number;
  pledged: number;
  cost_naira: number;
  cost_per_pledge: number;
}

interface ScoringData {
  total_scored: number;
  segments: { label: string; count: number; pct: number }[];
  avg_score: number;
  win_probability: number;
  vote_share: number;
}

type SubTab = 'roi' | 'scoring';

export default function GOTVAnalyticsScreen() {
  const [subTab, setSubTab] = useState<SubTab>('roi');
  const [channels, setChannels] = useState<ChannelROI[]>([]);
  const [scoring, setScoring] = useState<ScoringData | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = useCallback(async () => {
    try {
      const [roi, score] = await Promise.all([
        gotvFetch<{ channels: ChannelROI[] }>('/gotv/roi/channels'),
        gotvFetch<ScoringData>('/gotv/scoring/summary').catch(() => null),
      ]);
      setChannels(roi.channels || []);
      setScoring(score);
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
        <Text style={styles.title}>Analytics</Text>
      </View>

      <View style={styles.tabRow}>
        <TouchableOpacity
          style={[styles.tab, subTab === 'roi' && styles.tabActive]}
          onPress={() => setSubTab('roi')}
        >
          <Ionicons name="bar-chart" size={16} color={subTab === 'roi' ? '#006b3f' : '#6b7280'} />
          <Text style={[styles.tabText, subTab === 'roi' && styles.tabTextActive]}>Channel ROI</Text>
        </TouchableOpacity>
        <TouchableOpacity
          style={[styles.tab, subTab === 'scoring' && styles.tabActive]}
          onPress={() => setSubTab('scoring')}
        >
          <Ionicons name="analytics" size={16} color={subTab === 'scoring' ? '#006b3f' : '#6b7280'} />
          <Text style={[styles.tabText, subTab === 'scoring' && styles.tabTextActive]}>Scoring</Text>
        </TouchableOpacity>
      </View>

      {subTab === 'roi' && (
        <>
          {channels.length === 0 ? (
            <View style={styles.emptyCard}>
              <Ionicons name="bar-chart-outline" size={32} color="#9ca3af" />
              <Text style={styles.emptyText}>No channel data yet</Text>
            </View>
          ) : (
            channels.map(c => {
              const deliveryRate = c.sent > 0 ? Math.round((c.delivered / c.sent) * 100) : 0;
              const convRate = c.delivered > 0 ? Math.round((c.pledged / c.delivered) * 100) : 0;
              return (
                <View key={c.channel} style={styles.card}>
                  <View style={styles.cardHeader}>
                    <Text style={styles.channelName}>{c.channel}</Text>
                    <Text style={styles.costPer}>₦{c.cost_per_pledge.toLocaleString()}/pledge</Text>
                  </View>
                  <View style={styles.metricsRow}>
                    <View style={styles.metric}>
                      <Text style={styles.metricNum}>{c.sent}</Text>
                      <Text style={styles.metricLabel}>Sent</Text>
                    </View>
                    <View style={styles.metric}>
                      <Text style={styles.metricNum}>{deliveryRate}%</Text>
                      <Text style={styles.metricLabel}>Delivered</Text>
                    </View>
                    <View style={styles.metric}>
                      <Text style={[styles.metricNum, { color: '#006b3f' }]}>{c.pledged}</Text>
                      <Text style={styles.metricLabel}>Pledged</Text>
                    </View>
                    <View style={styles.metric}>
                      <Text style={[styles.metricNum, { color: convRate >= 10 ? '#22c55e' : '#f59e0b' }]}>{convRate}%</Text>
                      <Text style={styles.metricLabel}>Conv.</Text>
                    </View>
                  </View>
                </View>
              );
            })
          )}
        </>
      )}

      {subTab === 'scoring' && scoring && (
        <>
          {/* Win probability */}
          <View style={styles.winCard}>
            <Text style={styles.winLabel}>Win Probability</Text>
            <Text style={[styles.winValue, {
              color: scoring.win_probability >= 60 ? '#22c55e' : scoring.win_probability >= 40 ? '#f59e0b' : '#ef4444'
            }]}>
              {scoring.win_probability}%
            </Text>
            <Text style={styles.winMeta}>Vote share: {scoring.vote_share}%</Text>
          </View>

          {/* Scoring summary */}
          <View style={styles.card}>
            <Text style={styles.cardTitle}>Voter Scores</Text>
            <Text style={styles.cardSubtitle}>
              {scoring.total_scored} contacts scored · Avg: {scoring.avg_score.toFixed(1)}/100
            </Text>
            {scoring.segments.map(s => (
              <View key={s.label} style={styles.segRow}>
                <Text style={styles.segLabel}>{s.label}</Text>
                <View style={styles.segBarBg}>
                  <View style={[styles.segBarFill, { width: `${s.pct}%` }]} />
                </View>
                <Text style={styles.segCount}>{s.count}</Text>
              </View>
            ))}
          </View>
        </>
      )}

      {subTab === 'scoring' && !scoring && (
        <View style={styles.emptyCard}>
          <Ionicons name="analytics-outline" size={32} color="#9ca3af" />
          <Text style={styles.emptyText}>Scoring not yet available</Text>
        </View>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  header: { padding: 16, paddingBottom: 8 },
  title: { fontSize: 22, fontWeight: '700', color: '#111827' },
  tabRow: { flexDirection: 'row', marginHorizontal: 16, backgroundColor: '#f3f4f6', borderRadius: 10, padding: 4, marginBottom: 14 },
  tab: { flex: 1, flexDirection: 'row', alignItems: 'center', justifyContent: 'center', paddingVertical: 8, borderRadius: 8, gap: 6 },
  tabActive: { backgroundColor: '#fff', shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.06, shadowRadius: 2, elevation: 1 },
  tabText: { fontSize: 13, fontWeight: '500', color: '#6b7280' },
  tabTextActive: { color: '#006b3f', fontWeight: '600' },
  card: { backgroundColor: '#fff', marginHorizontal: 16, marginBottom: 12, borderRadius: 12, padding: 14, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 3, elevation: 2 },
  cardHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 10 },
  channelName: { fontSize: 15, fontWeight: '600', color: '#111827', textTransform: 'capitalize' },
  costPer: { fontSize: 12, fontWeight: '600', color: '#006b3f' },
  metricsRow: { flexDirection: 'row', justifyContent: 'space-between' },
  metric: { alignItems: 'center' },
  metricNum: { fontSize: 16, fontWeight: '700', color: '#111827' },
  metricLabel: { fontSize: 11, color: '#6b7280', marginTop: 2 },
  winCard: { backgroundColor: '#fff', marginHorizontal: 16, marginBottom: 12, borderRadius: 12, padding: 20, alignItems: 'center', shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 3, elevation: 2 },
  winLabel: { fontSize: 14, color: '#6b7280' },
  winValue: { fontSize: 48, fontWeight: '800', marginVertical: 4 },
  winMeta: { fontSize: 13, color: '#6b7280' },
  cardTitle: { fontSize: 15, fontWeight: '600', color: '#111827' },
  cardSubtitle: { fontSize: 12, color: '#6b7280', marginBottom: 12 },
  segRow: { flexDirection: 'row', alignItems: 'center', marginBottom: 8, gap: 8 },
  segLabel: { width: 60, fontSize: 12, color: '#374151', fontWeight: '500' },
  segBarBg: { flex: 1, height: 6, backgroundColor: '#e5e7eb', borderRadius: 3 },
  segBarFill: { height: 6, backgroundColor: '#006b3f', borderRadius: 3 },
  segCount: { width: 40, fontSize: 12, color: '#6b7280', textAlign: 'right' },
  emptyCard: { backgroundColor: '#fff', marginHorizontal: 16, borderRadius: 12, padding: 40, alignItems: 'center', gap: 8 },
  emptyText: { fontSize: 14, color: '#9ca3af' },
});
