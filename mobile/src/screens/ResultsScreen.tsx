import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, RefreshControl, Dimensions } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { apiGet } from '../lib/api';

interface Result {
  id: number;
  polling_unit_code: string;
  state_code: string;
  lga_code: string;
  registered_voters: number;
  accredited_voters: number;
  total_votes: number;
  status: string;
  party_scores: { party: string; votes: number }[];
}

export default function ResultsScreen() {
  const [results, setResults] = useState<Result[]>([]);
  const [refreshing, setRefreshing] = useState(false);
  const [summary, setSummary] = useState({ total: 0, submitted: 0, verified: 0 });

  const load = async () => {
    try {
      const data = await apiGet('/results?election_id=1&page=1&limit=50');
      setResults(data.results || []);
      setSummary({ total: data.total || 0, submitted: data.submitted || 0, verified: data.verified || 0 });
    } catch {}
  };

  useEffect(() => { load(); }, []);

  const onRefresh = async () => { setRefreshing(true); await load(); setRefreshing(false); };

  return (
    <View style={s.container}>
      <View style={s.summaryRow}>
        {[
          { label: 'Total', value: summary.total, color: '#1e293b' },
          { label: 'Submitted', value: summary.submitted, color: '#2563eb' },
          { label: 'Verified', value: summary.verified, color: '#15803d' },
        ].map((item) => (
          <View key={item.label} style={s.summaryCard}>
            <Text style={[s.summaryValue, { color: item.color }]}>{item.value}</Text>
            <Text style={s.summaryLabel}>{item.label}</Text>
          </View>
        ))}
      </View>

      <FlatList
        data={results}
        keyExtractor={(item) => item.id.toString()}
        renderItem={({ item }) => (
          <View style={s.card}>
            <View style={s.cardHeader}>
              <Text style={s.puCode}>{item.polling_unit_code}</Text>
              <View style={[s.badge, { backgroundColor: item.status === 'verified' ? '#dcfce7' : '#fef3c7' }]}>
                <Text style={[s.badgeText, { color: item.status === 'verified' ? '#15803d' : '#ca8a04' }]}>
                  {item.status}
                </Text>
              </View>
            </View>
            <Text style={s.location}>{item.state_code} / {item.lga_code}</Text>
            <View style={s.votesRow}>
              <Text style={s.votesLabel}>Votes: {item.total_votes?.toLocaleString()}</Text>
              <Text style={s.votesLabel}>Accredited: {item.accredited_voters?.toLocaleString()}</Text>
            </View>
            {item.party_scores && item.party_scores.length > 0 && (
              <View style={s.partyRow}>
                {item.party_scores.slice(0, 4).map((ps) => (
                  <View key={ps.party} style={s.partyChip}>
                    <Text style={s.partyName}>{ps.party}</Text>
                    <Text style={s.partyVotes}>{ps.votes.toLocaleString()}</Text>
                  </View>
                ))}
              </View>
            )}
          </View>
        )}
        contentContainerStyle={s.list}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} />}
        ListEmptyComponent={<Text style={s.empty}>No results yet</Text>}
      />
    </View>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  summaryRow: { flexDirection: 'row', padding: 16, gap: 8 },
  summaryCard: { flex: 1, backgroundColor: '#fff', borderRadius: 12, padding: 12, alignItems: 'center', elevation: 1 },
  summaryValue: { fontSize: 22, fontWeight: '700' },
  summaryLabel: { fontSize: 11, color: '#64748b', marginTop: 2 },
  list: { padding: 16, paddingTop: 0, gap: 8 },
  card: { backgroundColor: '#fff', borderRadius: 12, padding: 14, elevation: 1 },
  cardHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  puCode: { fontSize: 14, fontWeight: '600', color: '#1e293b' },
  badge: { paddingHorizontal: 8, paddingVertical: 2, borderRadius: 6 },
  badgeText: { fontSize: 10, fontWeight: '600', textTransform: 'uppercase' },
  location: { fontSize: 12, color: '#64748b', marginTop: 4 },
  votesRow: { flexDirection: 'row', justifyContent: 'space-between', marginTop: 8 },
  votesLabel: { fontSize: 12, color: '#475569' },
  partyRow: { flexDirection: 'row', flexWrap: 'wrap', marginTop: 8, gap: 6 },
  partyChip: { flexDirection: 'row', backgroundColor: '#f1f5f9', borderRadius: 8, paddingHorizontal: 8, paddingVertical: 4, gap: 4 },
  partyName: { fontSize: 11, fontWeight: '600', color: '#475569' },
  partyVotes: { fontSize: 11, color: '#15803d', fontWeight: '600' },
  empty: { textAlign: 'center', color: '#94a3b8', marginTop: 48 },
});
