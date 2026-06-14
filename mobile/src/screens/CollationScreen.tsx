import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, RefreshControl } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { apiGet } from '../lib/api';

interface CollationResult {
  id: number;
  level: string;
  area_code: string;
  area_name: string;
  total_registered: number;
  total_accredited: number;
  total_votes: number;
  turnout_pct: number;
  status: string;
}

export default function CollationScreen() {
  const [results, setResults] = useState<CollationResult[]>([]);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const data = await apiGet('/collation?election_id=1');
      setResults(data.collation_results || []);
    } catch {}
  };

  useEffect(() => { load(); }, []);

  return (
    <FlatList
      data={results}
      keyExtractor={(item) => item.id?.toString() || item.area_code}
      renderItem={({ item }) => (
        <View style={s.card}>
          <View style={s.header}>
            <Text style={s.area}>{item.area_name || item.area_code}</Text>
            <View style={[s.badge, { backgroundColor: item.status === 'completed' ? '#dcfce7' : '#fef9c3' }]}>
              <Text style={[s.badgeText, { color: item.status === 'completed' ? '#15803d' : '#a16207' }]}>{item.level}</Text>
            </View>
          </View>
          <View style={s.statsGrid}>
            <View style={s.stat}>
              <Text style={s.statValue}>{(item.total_registered || 0).toLocaleString()}</Text>
              <Text style={s.statLabel}>Registered</Text>
            </View>
            <View style={s.stat}>
              <Text style={s.statValue}>{(item.total_accredited || 0).toLocaleString()}</Text>
              <Text style={s.statLabel}>Accredited</Text>
            </View>
            <View style={s.stat}>
              <Text style={[s.statValue, { color: '#15803d' }]}>{(item.turnout_pct || 0).toFixed(1)}%</Text>
              <Text style={s.statLabel}>Turnout</Text>
            </View>
          </View>
        </View>
      )}
      contentContainerStyle={s.list}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={async () => { setRefreshing(true); await load(); setRefreshing(false); }} />}
      ListEmptyComponent={<Text style={s.empty}>No collation data</Text>}
    />
  );
}

const s = StyleSheet.create({
  list: { padding: 16, gap: 10 },
  card: { backgroundColor: '#fff', borderRadius: 12, padding: 14, elevation: 1 },
  header: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  area: { fontSize: 15, fontWeight: '600', color: '#1e293b', flex: 1 },
  badge: { paddingHorizontal: 8, paddingVertical: 2, borderRadius: 6 },
  badgeText: { fontSize: 10, fontWeight: '700', textTransform: 'uppercase' },
  statsGrid: { flexDirection: 'row', marginTop: 12, gap: 8 },
  stat: { flex: 1, alignItems: 'center', backgroundColor: '#f8fafc', borderRadius: 8, padding: 8 },
  statValue: { fontSize: 16, fontWeight: '700', color: '#1e293b' },
  statLabel: { fontSize: 10, color: '#64748b', marginTop: 2 },
  empty: { textAlign: 'center', color: '#94a3b8', marginTop: 48 },
});
