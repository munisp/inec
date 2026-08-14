import { useState, useEffect } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Platform, ActivityIndicator } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api } from '../src/lib/api';
import { useResolvedElection } from '../src/lib/election';

// R4-52: /collation/summary never existed; the real backend route is
// GET /dashboard/collation (state-level rows). We aggregate the rows into a
// national summary client-side.
interface CollationStateRow {
  code: string;
  name: string;
  total_pus: number;
  reported_pus: number;
  total_valid_votes: number;
  rejected_votes: number;
  party_scores?: { party_code: string; abbreviation: string; total_votes: number }[];
}

interface CollationSummary {
  election_title: string;
  total_polling_units: number;
  results_received: number;
  completion_pct: number;
  total_valid_votes: number;
  total_rejected_votes: number;
  party_totals: Record<string, number>;
}

export default function CollationScreen() {
  const [summary, setSummary] = useState<CollationSummary | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { electionId, loading: electionLoading } = useResolvedElection();

  const loadCollation = async () => {
    if (!electionId) return;
    setLoading(true);
    setError(null);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    try {
      const rows = await api<CollationStateRow[]>(`/dashboard/collation?election_id=${electionId}`);
      const list = Array.isArray(rows) ? rows : [];
      const partyTotals: Record<string, number> = {};
      let totalPUs = 0, received = 0, valid = 0, rejected = 0;
      for (const row of list) {
        totalPUs += row.total_pus || 0;
        received += row.reported_pus || 0;
        valid += row.total_valid_votes || 0;
        rejected += row.rejected_votes || 0;
        for (const ps of row.party_scores || []) {
          const label = ps.abbreviation || ps.party_code;
          partyTotals[label] = (partyTotals[label] || 0) + (ps.total_votes || 0);
        }
      }
      setSummary({
        election_title: `Election #${electionId}`,
        total_polling_units: totalPUs,
        results_received: received,
        completion_pct: totalPUs > 0 ? (received / totalPUs) * 100 : 0,
        total_valid_votes: valid,
        total_rejected_votes: rejected,
        party_totals: partyTotals,
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { loadCollation(); }, [electionId]);

  // Gate rendering until the election scope is resolved — never fetch with a hardcoded id.
  if (electionLoading || !electionId) {
    return (
      <View style={{ flex: 1, justifyContent: 'center', alignItems: 'center', padding: 24 }}>
        {electionLoading ? (
          <ActivityIndicator size="large" color="#166534" />
        ) : (
          <Text style={{ color: '#6b7280', textAlign: 'center' }}>No active election is available.</Text>
        )}
      </View>
    );
  }

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}>
      <View style={styles.header}>
        <Ionicons name="stats-chart" size={28} color="#166534" />
        <Text style={styles.title}>Result Collation</Text>
      </View>

      {loading && <ActivityIndicator size="large" color="#166534" style={{ marginTop: 40 }} />}
      {error && (
        <View style={styles.errorCard}>
          <Text style={styles.errorText}>{error}</Text>
          <TouchableOpacity style={styles.retryBtn} onPress={loadCollation}>
            <Text style={styles.retryText}>Retry</Text>
          </TouchableOpacity>
        </View>
      )}

      {summary && (
        <>
          <View style={styles.progressCard}>
            <Text style={styles.progressTitle}>{summary.election_title}</Text>
            <View style={styles.progressBarBg}>
              <View style={[styles.progressBarFill, { width: `${Math.min(summary.completion_pct, 100)}%` }]} />
            </View>
            <Text style={styles.progressText}>{summary.completion_pct.toFixed(1)}% Complete</Text>
          </View>

          <View style={styles.statsRow}>
            {[
              { label: 'Total PUs', value: summary.total_polling_units, icon: 'location' as const, color: '#3b82f6' },
              { label: 'Received', value: summary.results_received, icon: 'cloud-download' as const, color: '#22c55e' },
              { label: 'Valid Votes', value: summary.total_valid_votes, icon: 'checkmark-circle' as const, color: '#f59e0b' },
              { label: 'Rejected', value: summary.total_rejected_votes, icon: 'shield-checkmark' as const, color: '#166534' },
            ].map((s) => (
              <View key={s.label} style={styles.statCard}>
                <Ionicons name={s.icon} size={20} color={s.color} />
                <Text style={styles.statValue}>{s.value.toLocaleString()}</Text>
                <Text style={styles.statLabel}>{s.label}</Text>
              </View>
            ))}
          </View>

          <View style={styles.section}>
            <Text style={styles.sectionTitle}>Vote Summary</Text>
            <View style={styles.infoRow}>
              <Text style={styles.infoLabel}>Valid Votes</Text>
              <Text style={styles.infoValue}>{(summary.total_valid_votes || 0).toLocaleString()}</Text>
            </View>
            <View style={styles.infoRow}>
              <Text style={styles.infoLabel}>Rejected Votes</Text>
              <Text style={styles.infoValue}>{(summary.total_rejected_votes || 0).toLocaleString()}</Text>
            </View>
          </View>

          {summary.party_totals && Object.keys(summary.party_totals).length > 0 && (
            <View style={styles.section}>
              <Text style={styles.sectionTitle}>Party Results</Text>
              {Object.entries(summary.party_totals)
                .sort(([, a], [, b]) => b - a)
                .map(([party, votes]) => (
                  <View key={party} style={styles.partyRow}>
                    <Text style={styles.partyName}>{party}</Text>
                    <Text style={styles.partyVotes}>{votes.toLocaleString()}</Text>
                  </View>
                ))}
            </View>
          )}
        </>
      )}

      <TouchableOpacity style={styles.refreshBtn} onPress={loadCollation}>
        <Ionicons name="refresh" size={18} color="#fff" />
        <Text style={styles.refreshText}>Refresh</Text>
      </TouchableOpacity>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  header: { flexDirection: 'row', alignItems: 'center', gap: 10, padding: 16, paddingTop: Platform.OS === 'ios' ? 60 : 16 },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b' },
  errorCard: { margin: 16, padding: 16, backgroundColor: '#fef2f2', borderRadius: 12, alignItems: 'center' },
  errorText: { color: '#dc2626', marginBottom: 8 },
  retryBtn: { paddingHorizontal: 16, paddingVertical: 8, backgroundColor: '#dc2626', borderRadius: 8 },
  retryText: { color: '#fff', fontWeight: '600' },
  progressCard: { margin: 16, padding: 16, backgroundColor: '#fff', borderRadius: 12, borderWidth: 1, borderColor: '#e2e8f0' },
  progressTitle: { fontSize: 16, fontWeight: '600', color: '#1e293b', marginBottom: 12 },
  progressBarBg: { height: 8, backgroundColor: '#e2e8f0', borderRadius: 4, overflow: 'hidden' },
  progressBarFill: { height: '100%', backgroundColor: '#166534', borderRadius: 4 },
  progressText: { fontSize: 13, color: '#64748b', marginTop: 6, textAlign: 'right' },
  statsRow: { flexDirection: 'row', flexWrap: 'wrap', paddingHorizontal: 12, gap: 8 },
  statCard: { flex: 1, minWidth: '45%', backgroundColor: '#fff', padding: 14, borderRadius: 12, alignItems: 'center', borderWidth: 1, borderColor: '#e2e8f0' },
  statValue: { fontSize: 18, fontWeight: '700', color: '#1e293b', marginTop: 4 },
  statLabel: { fontSize: 11, color: '#64748b', marginTop: 2 },
  section: { margin: 16, padding: 16, backgroundColor: '#fff', borderRadius: 12, borderWidth: 1, borderColor: '#e2e8f0' },
  sectionTitle: { fontSize: 16, fontWeight: '600', color: '#1e293b', marginBottom: 12 },
  infoRow: { flexDirection: 'row', justifyContent: 'space-between', paddingVertical: 8, borderBottomWidth: 1, borderBottomColor: '#f1f5f9' },
  infoLabel: { fontSize: 14, color: '#64748b' },
  infoValue: { fontSize: 14, fontWeight: '600', color: '#1e293b' },
  partyRow: { flexDirection: 'row', justifyContent: 'space-between', paddingVertical: 10, borderBottomWidth: 1, borderBottomColor: '#f1f5f9' },
  partyName: { fontSize: 14, fontWeight: '600', color: '#1e293b' },
  partyVotes: { fontSize: 14, fontWeight: '700', color: '#166534' },
  refreshBtn: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 6, margin: 16, padding: 14, backgroundColor: '#166534', borderRadius: 12 },
  refreshText: { color: '#fff', fontWeight: '600', fontSize: 15 },
});
