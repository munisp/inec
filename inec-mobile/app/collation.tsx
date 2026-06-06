import { useState, useEffect } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Platform, ActivityIndicator } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api } from '../src/lib/api';

interface CollationSummary {
  election_id: number;
  election_title: string;
  total_polling_units: number;
  results_received: number;
  results_validated: number;
  results_finalized: number;
  completion_pct: number;
  total_valid_votes: number;
  total_rejected_votes: number;
  party_totals: Record<string, number>;
}

export default function CollationScreen() {
  const [summary, setSummary] = useState<CollationSummary | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadCollation = async () => {
    setLoading(true);
    setError(null);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    try {
      const data = await api<CollationSummary>('/collation/summary?election_id=1');
      setSummary(data);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { loadCollation(); }, []);

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
              { label: 'Validated', value: summary.results_validated, icon: 'checkmark-circle' as const, color: '#f59e0b' },
              { label: 'Finalized', value: summary.results_finalized, icon: 'shield-checkmark' as const, color: '#166534' },
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
