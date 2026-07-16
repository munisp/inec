import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, RefreshControl } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { apiGet } from '../lib/api';

interface ScoredContact {
  contact_id: string;
  name: string;
  score: number;
  likelihood: string;
  state_code: string;
  factors: string[];
}

export default function ScoringScreen() {
  const [contacts, setContacts] = useState<ScoredContact[]>([]);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const data = await apiGet('/gotv/scoring/leaderboard');
      setContacts(data.contacts || data.leaderboard || []);
    } catch {}
  };

  useEffect(() => { load(); }, []);

  const scoreColor = (score: number) => score >= 80 ? '#15803d' : score >= 50 ? '#ca8a04' : '#dc2626';

  return (
    <FlatList
      data={contacts}
      keyExtractor={(item) => item.contact_id}
      renderItem={({ item, index }) => (
        <View style={s.card}>
          <View style={s.rank}>
            <Text style={s.rankText}>#{index + 1}</Text>
          </View>
          <View style={s.info}>
            <Text style={s.name}>{item.name || item.contact_id}</Text>
            <Text style={s.location}>{item.state_code} | {item.likelihood}</Text>
          </View>
          <View style={[s.scoreCircle, { borderColor: scoreColor(item.score) }]}>
            <Text style={[s.scoreText, { color: scoreColor(item.score) }]}>{item.score}</Text>
          </View>
        </View>
      )}
      contentContainerStyle={s.list}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={async () => { setRefreshing(true); await load(); setRefreshing(false); }} />}
      ListEmptyComponent={<Text style={s.empty}>No scored contacts</Text>}
    />
  );
}

const s = StyleSheet.create({
  list: { padding: 16, gap: 6 },
  card: { flexDirection: 'row', alignItems: 'center', backgroundColor: '#fff', borderRadius: 12, padding: 12, elevation: 1, gap: 12 },
  rank: { width: 32, height: 32, borderRadius: 16, backgroundColor: '#f1f5f9', justifyContent: 'center', alignItems: 'center' },
  rankText: { fontSize: 12, fontWeight: '700', color: '#64748b' },
  info: { flex: 1 },
  name: { fontSize: 14, fontWeight: '600', color: '#1e293b' },
  location: { fontSize: 11, color: '#94a3b8', marginTop: 2 },
  scoreCircle: { width: 44, height: 44, borderRadius: 22, borderWidth: 3, justifyContent: 'center', alignItems: 'center' },
  scoreText: { fontSize: 16, fontWeight: '700' },
  empty: { textAlign: 'center', color: '#94a3b8', marginTop: 48 },
});
