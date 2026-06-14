import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, RefreshControl } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { apiGet } from '../lib/api';

interface Volunteer {
  volunteer_id: string;
  full_name: string;
  phone: string;
  assigned_state: string;
  assigned_lga: string;
  vetting_status: string;
  score: number;
  total_tasks: number;
  completed_tasks: number;
}

const vettingColors: Record<string, string> = {
  approved: '#15803d', pending: '#ca8a04', rejected: '#dc2626',
  nin_verified: '#2563eb', trained: '#7c3aed',
};

export default function VolunteersScreen() {
  const [volunteers, setVolunteers] = useState<Volunteer[]>([]);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const data = await apiGet('/gotv/volunteers?page=1&limit=50');
      setVolunteers(data.volunteers || []);
    } catch {}
  };

  useEffect(() => { load(); }, []);

  return (
    <FlatList
      data={volunteers}
      keyExtractor={(item) => item.volunteer_id}
      renderItem={({ item }) => (
        <View style={s.card}>
          <View style={s.row}>
            <View style={s.avatar}>
              <Ionicons name="person" size={18} color="#7c3aed" />
            </View>
            <View style={s.info}>
              <Text style={s.name}>{item.full_name || item.volunteer_id}</Text>
              <Text style={s.location}>{item.assigned_state}/{item.assigned_lga}</Text>
            </View>
            <View style={[s.badge, { backgroundColor: (vettingColors[item.vetting_status] || '#94a3b8') + '15' }]}>
              <Text style={[s.badgeText, { color: vettingColors[item.vetting_status] || '#94a3b8' }]}>
                {item.vetting_status?.replace(/_/g, ' ')}
              </Text>
            </View>
          </View>
          <View style={s.statsRow}>
            {item.score > 0 && (
              <View style={s.stat}>
                <Ionicons name="star" size={12} color="#ca8a04" />
                <Text style={s.statText}>{item.score}</Text>
              </View>
            )}
            <View style={s.stat}>
              <Ionicons name="checkbox" size={12} color="#15803d" />
              <Text style={s.statText}>{item.completed_tasks || 0}/{item.total_tasks || 0} tasks</Text>
            </View>
          </View>
        </View>
      )}
      contentContainerStyle={s.list}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={async () => { setRefreshing(true); await load(); setRefreshing(false); }} />}
      ListEmptyComponent={<Text style={s.empty}>No volunteers</Text>}
    />
  );
}

const s = StyleSheet.create({
  list: { padding: 16, gap: 8 },
  card: { backgroundColor: '#fff', borderRadius: 12, padding: 12, elevation: 1 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  avatar: { width: 36, height: 36, borderRadius: 18, backgroundColor: '#f5f3ff', justifyContent: 'center', alignItems: 'center' },
  info: { flex: 1 },
  name: { fontSize: 14, fontWeight: '600', color: '#1e293b' },
  location: { fontSize: 11, color: '#94a3b8', marginTop: 2 },
  badge: { paddingHorizontal: 8, paddingVertical: 3, borderRadius: 6 },
  badgeText: { fontSize: 10, fontWeight: '700', textTransform: 'uppercase' },
  statsRow: { flexDirection: 'row', gap: 12, marginTop: 8 },
  stat: { flexDirection: 'row', alignItems: 'center', gap: 4 },
  statText: { fontSize: 12, color: '#475569' },
  empty: { textAlign: 'center', color: '#94a3b8', marginTop: 48 },
});
