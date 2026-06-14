import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, RefreshControl, TouchableOpacity, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { apiGet, apiPost } from '../lib/api';

interface VotingRound {
  id: number;
  round_number: number;
  position: string;
  status: string;
  voting_method: string;
  total_votes: number;
  total_eligible: number;
  created_at: string;
  aspirants: { aspirant_id: number; name: string; votes: number }[];
}

const statusConfig: Record<string, { color: string; icon: string }> = {
  pending: { color: '#94a3b8', icon: 'time' },
  open: { color: '#15803d', icon: 'radio-button-on' },
  closed: { color: '#ca8a04', icon: 'close-circle' },
  tallied: { color: '#2563eb', icon: 'bar-chart' },
  certified: { color: '#7c3aed', icon: 'ribbon' },
};

export default function VotingScreen() {
  const [rounds, setRounds] = useState<VotingRound[]>([]);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const data = await apiGet('/gotv/primaries/elections/1/rounds');
      setRounds(data.rounds || []);
    } catch {}
  };

  useEffect(() => { load(); }, []);

  const castVote = (roundId: number, aspirantId: number, aspirantName: string) => {
    Alert.alert(
      'Cast Vote',
      `Vote for ${aspirantName}? This cannot be undone.`,
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Confirm Vote',
          style: 'default',
          onPress: async () => {
            try {
              const result = await apiPost(`/gotv/primaries/rounds/${roundId}/vote`, { aspirant_id: aspirantId });
              Alert.alert('Vote Cast', `Confirmation: ${result.confirmation_code || 'Success'}`);
              load();
            } catch (err: any) {
              Alert.alert('Error', err.message || 'Failed to cast vote');
            }
          },
        },
      ]
    );
  };

  return (
    <FlatList
      data={rounds}
      keyExtractor={(item) => item.id.toString()}
      renderItem={({ item }) => {
        const config = statusConfig[item.status] || statusConfig.pending;
        const turnout = item.total_eligible > 0 ? (item.total_votes / item.total_eligible) * 100 : 0;

        return (
          <View style={s.card}>
            <View style={s.header}>
              <View style={s.roundInfo}>
                <Text style={s.roundNumber}>Round {item.round_number}</Text>
                <Text style={s.position}>{item.position?.replace(/_/g, ' ')}</Text>
              </View>
              <View style={[s.statusBadge, { backgroundColor: config.color + '15' }]}>
                <Ionicons name={config.icon as any} size={12} color={config.color} />
                <Text style={[s.statusText, { color: config.color }]}>{item.status}</Text>
              </View>
            </View>

            <View style={s.statsRow}>
              <Text style={s.stat}>Method: {item.voting_method?.replace(/_/g, ' ')}</Text>
              <Text style={s.stat}>Turnout: {turnout.toFixed(1)}%</Text>
              <Text style={s.stat}>Votes: {item.total_votes}</Text>
            </View>

            {item.aspirants && item.aspirants.length > 0 && (
              <View style={s.aspirantList}>
                {item.aspirants.map((a) => {
                  const pct = item.total_votes > 0 ? (a.votes / item.total_votes) * 100 : 0;
                  return (
                    <TouchableOpacity
                      key={a.aspirant_id}
                      style={s.aspirantRow}
                      onPress={() => item.status === 'open' ? castVote(item.id, a.aspirant_id, a.name) : null}
                      disabled={item.status !== 'open'}
                    >
                      <Text style={s.aspirantName}>{a.name}</Text>
                      <View style={s.progressBar}>
                        <View style={[s.progressFill, { width: `${pct}%` }]} />
                      </View>
                      <Text style={s.aspirantVotes}>{a.votes} ({pct.toFixed(1)}%)</Text>
                    </TouchableOpacity>
                  );
                })}
              </View>
            )}

            {item.status === 'open' && (
              <View style={s.liveRow}>
                <View style={s.liveDot} />
                <Text style={s.liveText}>Voting is live — tap aspirant to cast vote</Text>
              </View>
            )}
          </View>
        );
      }}
      contentContainerStyle={s.list}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={async () => { setRefreshing(true); await load(); setRefreshing(false); }} />}
      ListEmptyComponent={<Text style={s.empty}>No voting rounds</Text>}
    />
  );
}

const s = StyleSheet.create({
  list: { padding: 16, gap: 12 },
  card: { backgroundColor: '#fff', borderRadius: 14, padding: 16, elevation: 2 },
  header: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  roundInfo: {},
  roundNumber: { fontSize: 16, fontWeight: '700', color: '#1e293b' },
  position: { fontSize: 12, color: '#64748b', textTransform: 'capitalize', marginTop: 2 },
  statusBadge: { flexDirection: 'row', alignItems: 'center', gap: 4, paddingHorizontal: 10, paddingVertical: 4, borderRadius: 8 },
  statusText: { fontSize: 11, fontWeight: '700', textTransform: 'uppercase' },
  statsRow: { flexDirection: 'row', marginTop: 12, gap: 8 },
  stat: { fontSize: 11, color: '#64748b', textTransform: 'capitalize' },
  aspirantList: { marginTop: 12, gap: 8 },
  aspirantRow: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  aspirantName: { width: 80, fontSize: 13, fontWeight: '500', color: '#1e293b' },
  progressBar: { flex: 1, height: 8, backgroundColor: '#e2e8f0', borderRadius: 4, overflow: 'hidden' },
  progressFill: { height: '100%', backgroundColor: '#7c3aed', borderRadius: 4 },
  aspirantVotes: { width: 80, fontSize: 11, color: '#64748b', textAlign: 'right' },
  liveRow: { flexDirection: 'row', alignItems: 'center', gap: 6, marginTop: 12, justifyContent: 'center' },
  liveDot: { width: 6, height: 6, borderRadius: 3, backgroundColor: '#dc2626' },
  liveText: { fontSize: 11, color: '#dc2626', fontWeight: '500' },
  empty: { textAlign: 'center', color: '#94a3b8', marginTop: 48 },
});
