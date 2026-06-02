import { useState, useCallback } from 'react';
import {
  View, Text, StyleSheet, ScrollView, TouchableOpacity,
  RefreshControl, Platform,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { useLocalSearchParams, useFocusEffect } from 'expo-router';
import { electionApi, Result, CollationSummary } from '../src/lib/api';
import { EmptyState } from '../src/components/EmptyState';
import { CardSkeleton } from '../src/components/SkeletonLoader';

const PARTY_COLORS: Record<string, string> = {
  APC: '#0074D9', PDP: '#FF4136', LP: '#2ECC40', NNPP: '#B10DC9',
  ADC: '#FF851B', SDP: '#FFDC00', APGA: '#7FDBFF', YPP: '#01FF70',
};

type ViewMode = 'results' | 'collation';
type CollationLevel = 'state' | 'lga' | 'ward';

export default function ResultsScreen() {
  const params = useLocalSearchParams<{ election_id: string; election_name: string }>();
  const electionId = parseInt(params.election_id || '1', 10);
  const electionName = params.election_name || 'Election Results';

  const [mode, setMode] = useState<ViewMode>('results');
  const [level, setLevel] = useState<CollationLevel>('state');
  const [results, setResults] = useState<Result[]>([]);
  const [collation, setCollation] = useState<CollationSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [dashStats, setDashStats] = useState<{ total_votes: number; results_count: number; polling_units: number; rejection_rate: number } | null>(null);

  const loadData = useCallback(async () => {
    try {
      const [r, s] = await Promise.all([
        electionApi.results(electionId),
        electionApi.dashboardStats(electionId),
      ]);
      setResults(r);
      setDashStats(s);
    } catch { /* */ }
    setLoading(false);
  }, [electionId]);

  const loadCollation = useCallback(async () => {
    try {
      const data = await electionApi.collation(electionId, level);
      setCollation(data);
    } catch { /* */ }
  }, [electionId, level]);

  useFocusEffect(useCallback(() => { loadData(); loadCollation(); }, [loadData, loadCollation]));

  const onRefresh = useCallback(async () => {
    setRefreshing(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    await loadData();
    if (mode === 'collation') await loadCollation();
    setRefreshing(false);
  }, [loadData, loadCollation, mode]);

  if (loading) return <View style={styles.container}><CardSkeleton /><CardSkeleton /></View>;

  return (
    <ScrollView
      style={styles.container}
      contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} colors={['#166534']} tintColor="#166534" />}
    >
      {/* Dashboard stats */}
      {dashStats && (
        <View style={styles.statsRow}>
          <View style={styles.statCard}>
            <Text style={styles.statNumber}>{(dashStats.total_votes || 0).toLocaleString()}</Text>
            <Text style={styles.statLabel}>Total Votes</Text>
          </View>
          <View style={styles.statCard}>
            <Text style={[styles.statNumber, { color: '#2563eb' }]}>{dashStats.results_count || 0}</Text>
            <Text style={styles.statLabel}>Results</Text>
          </View>
          <View style={styles.statCard}>
            <Text style={[styles.statNumber, { color: '#f59e0b' }]}>{((dashStats.rejection_rate || 0) * 100).toFixed(1)}%</Text>
            <Text style={styles.statLabel}>Rejected</Text>
          </View>
        </View>
      )}

      {/* Mode toggle */}
      <View style={styles.toggleContainer}>
        {(['results', 'collation'] as ViewMode[]).map((m) => (
          <TouchableOpacity
            key={m}
            style={[styles.toggleButton, mode === m && styles.toggleButtonActive]}
            onPress={() => { Haptics.selectionAsync(); setMode(m); if (m === 'collation') loadCollation(); }}
          >
            <Ionicons name={m === 'results' ? 'document-text-outline' : 'layers-outline'} size={16} color={mode === m ? '#fff' : '#6b7280'} />
            <Text style={[styles.toggleText, mode === m && styles.toggleTextActive]}>{m === 'results' ? 'Results' : 'Collation'}</Text>
          </TouchableOpacity>
        ))}
      </View>

      {mode === 'collation' && (
        <View style={styles.levelRow}>
          {(['state', 'lga', 'ward'] as CollationLevel[]).map((l) => (
            <TouchableOpacity
              key={l}
              style={[styles.levelChip, level === l && styles.levelChipActive]}
              onPress={() => { Haptics.selectionAsync(); setLevel(l); }}
            >
              <Text style={[styles.levelText, level === l && styles.levelTextActive]}>{l.toUpperCase()}</Text>
            </TouchableOpacity>
          ))}
        </View>
      )}

      {mode === 'results' ? (
        results.length === 0 ? (
          <EmptyState icon="document-text-outline" title="No Results" description="No results have been submitted yet" />
        ) : (
          results.slice(0, 50).map((r) => (
            <View key={r.id} style={styles.resultCard}>
              <View style={styles.resultHeader}>
                <View style={{ flex: 1 }}>
                  <Text style={styles.puCode}>{r.polling_unit_code}</Text>
                  {r.state && <Text style={styles.puLocation}>{r.lga}, {r.state}</Text>}
                </View>
                <View style={[styles.statusBadge, { backgroundColor: r.status === 'verified' ? '#f0fdf4' : r.status === 'pending' ? '#fffbeb' : '#f9fafb' }]}>
                  <Text style={[styles.statusText, { color: r.status === 'verified' ? '#22c55e' : r.status === 'pending' ? '#f59e0b' : '#6b7280' }]}>
                    {r.status}
                  </Text>
                </View>
              </View>
              <View style={styles.votesRow}>
                <Text style={styles.votesLabel}>Valid: <Text style={styles.votesValue}>{r.total_valid_votes.toLocaleString()}</Text></Text>
                <Text style={styles.votesLabel}>Rejected: <Text style={[styles.votesValue, { color: '#ef4444' }]}>{r.rejected_votes.toLocaleString()}</Text></Text>
                <Text style={styles.votesLabel}>Total: <Text style={styles.votesValue}>{r.total_votes_cast.toLocaleString()}</Text></Text>
              </View>
              {r.party_scores && r.party_scores.length > 0 && (
                <View style={styles.partyScoresRow}>
                  {r.party_scores.slice(0, 5).map((ps) => (
                    <View key={ps.party_code} style={styles.partyPill}>
                      <View style={[styles.partyDot, { backgroundColor: PARTY_COLORS[ps.party_code] || '#6b7280' }]} />
                      <Text style={styles.partyPillText}>{ps.party_code}: {ps.votes}</Text>
                    </View>
                  ))}
                </View>
              )}
            </View>
          ))
        )
      ) : (
        collation.length === 0 ? (
          <EmptyState icon="layers-outline" title="No Collation Data" description="Collation results not available" />
        ) : (
          collation.map((c, idx) => {
            const maxVotes = Math.max(...(c.party_totals || []).map(p => p.total_votes), 1);
            return (
              <View key={idx} style={styles.collationCard}>
                <View style={styles.collationHeader}>
                  <Text style={styles.collationName}>{c.area_name || c.area_code}</Text>
                  <View style={styles.reportingBadge}>
                    <Text style={styles.reportingText}>{(c.reporting_pct || 0).toFixed(0)}% reported</Text>
                  </View>
                </View>
                <View style={styles.collationStats}>
                  <Text style={styles.collationVotes}>{(c.total_votes || 0).toLocaleString()} votes</Text>
                  <Text style={styles.collationRejected}>{(c.total_rejected || 0).toLocaleString()} rejected</Text>
                </View>
                {(c.party_totals || []).slice(0, 5).map((pt) => (
                  <View key={pt.party_code} style={styles.barRow}>
                    <Text style={styles.barLabel}>{pt.party_code}</Text>
                    <View style={styles.barTrack}>
                      <View style={[styles.barFill, { width: `${(pt.total_votes / maxVotes) * 100}%`, backgroundColor: PARTY_COLORS[pt.party_code] || '#6b7280' }]} />
                    </View>
                    <Text style={styles.barValue}>{pt.total_votes.toLocaleString()}</Text>
                  </View>
                ))}
              </View>
            );
          })
        )
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  statsRow: { flexDirection: 'row', gap: 8, padding: 16, paddingBottom: 8 },
  statCard: { flex: 1, backgroundColor: '#fff', borderRadius: 12, padding: 12, alignItems: 'center', shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.04, shadowRadius: 4, elevation: 1 },
  statNumber: { fontSize: 18, fontWeight: '800', color: '#111827' },
  statLabel: { fontSize: 10, color: '#6b7280', marginTop: 2 },
  toggleContainer: { flexDirection: 'row', marginHorizontal: 16, marginBottom: 12, backgroundColor: '#f3f4f6', borderRadius: 12, padding: 3 },
  toggleButton: { flex: 1, flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 6, paddingVertical: 10, borderRadius: 10 },
  toggleButtonActive: { backgroundColor: '#166534' },
  toggleText: { fontSize: 13, fontWeight: '600', color: '#6b7280' },
  toggleTextActive: { color: '#fff' },
  levelRow: { flexDirection: 'row', gap: 8, paddingHorizontal: 16, marginBottom: 12 },
  levelChip: { paddingHorizontal: 16, paddingVertical: 8, borderRadius: 10, backgroundColor: '#f3f4f6' },
  levelChipActive: { backgroundColor: '#166534' },
  levelText: { fontSize: 12, fontWeight: '700', color: '#6b7280' },
  levelTextActive: { color: '#fff' },
  resultCard: { marginHorizontal: 16, marginBottom: 10, backgroundColor: '#fff', borderRadius: 14, padding: 14, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  resultHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 },
  puCode: { fontSize: 14, fontWeight: '700', color: '#111827' },
  puLocation: { fontSize: 11, color: '#9ca3af', marginTop: 2 },
  statusBadge: { paddingHorizontal: 8, paddingVertical: 4, borderRadius: 8 },
  statusText: { fontSize: 10, fontWeight: '700', textTransform: 'uppercase' },
  votesRow: { flexDirection: 'row', gap: 12, marginBottom: 8 },
  votesLabel: { fontSize: 12, color: '#6b7280' },
  votesValue: { fontWeight: '700', color: '#111827' },
  partyScoresRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 6 },
  partyPill: { flexDirection: 'row', alignItems: 'center', gap: 4, backgroundColor: '#f9fafb', paddingHorizontal: 8, paddingVertical: 4, borderRadius: 8 },
  partyDot: { width: 8, height: 8, borderRadius: 4 },
  partyPillText: { fontSize: 11, fontWeight: '600', color: '#374151' },
  collationCard: { marginHorizontal: 16, marginBottom: 10, backgroundColor: '#fff', borderRadius: 14, padding: 16, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  collationHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 10 },
  collationName: { fontSize: 15, fontWeight: '700', color: '#111827' },
  reportingBadge: { backgroundColor: '#dbeafe', paddingHorizontal: 8, paddingVertical: 4, borderRadius: 8 },
  reportingText: { fontSize: 11, fontWeight: '600', color: '#2563eb' },
  collationStats: { flexDirection: 'row', gap: 16, marginBottom: 12 },
  collationVotes: { fontSize: 13, fontWeight: '600', color: '#166534' },
  collationRejected: { fontSize: 13, color: '#ef4444' },
  barRow: { flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 6 },
  barLabel: { width: 36, fontSize: 11, fontWeight: '700', color: '#374151' },
  barTrack: { flex: 1, height: 8, backgroundColor: '#f3f4f6', borderRadius: 4, overflow: 'hidden' },
  barFill: { height: '100%', borderRadius: 4 },
  barValue: { width: 50, fontSize: 11, fontWeight: '600', color: '#6b7280', textAlign: 'right' },
});
