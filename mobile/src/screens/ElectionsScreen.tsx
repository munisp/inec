import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, TouchableOpacity, RefreshControl } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { apiGet } from '../lib/api';

interface Election {
  id: number;
  title: string;
  election_type: string;
  election_date: string;
  status: string;
  state_scope: string;
}

const statusColors: Record<string, string> = {
  draft: '#94a3b8', scheduled: '#ca8a04', voting: '#dc2626',
  collating: '#2563eb', completed: '#15803d',
};

export default function ElectionsScreen() {
  const [elections, setElections] = useState<Election[]>([]);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const data = await apiGet('/elections');
      setElections(data.elections || []);
    } catch {}
  };

  useEffect(() => { load(); }, []);

  const onRefresh = async () => { setRefreshing(true); await load(); setRefreshing(false); };

  const renderItem = ({ item }: { item: Election }) => (
    <View style={s.card}>
      <View style={s.cardHeader}>
        <Text style={s.cardTitle}>{item.title}</Text>
        <View style={[s.badge, { backgroundColor: (statusColors[item.status] || '#94a3b8') + '20' }]}>
          <Text style={[s.badgeText, { color: statusColors[item.status] || '#94a3b8' }]}>
            {item.status?.toUpperCase()}
          </Text>
        </View>
      </View>
      <View style={s.cardBody}>
        <View style={s.infoRow}>
          <Ionicons name="calendar-outline" size={14} color="#64748b" />
          <Text style={s.infoText}>{item.election_date || 'TBD'}</Text>
        </View>
        <View style={s.infoRow}>
          <Ionicons name="document-text-outline" size={14} color="#64748b" />
          <Text style={s.infoText}>{item.election_type?.replace(/_/g, ' ')}</Text>
        </View>
        <View style={s.infoRow}>
          <Ionicons name="location-outline" size={14} color="#64748b" />
          <Text style={s.infoText}>{item.state_scope || 'National'}</Text>
        </View>
      </View>
    </View>
  );

  return (
    <FlatList
      data={elections}
      keyExtractor={(item) => item.id.toString()}
      renderItem={renderItem}
      contentContainerStyle={s.list}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} />}
      ListEmptyComponent={<Text style={s.empty}>No elections found</Text>}
    />
  );
}

const s = StyleSheet.create({
  list: { padding: 16, gap: 12 },
  card: { backgroundColor: '#fff', borderRadius: 12, padding: 16, elevation: 2, shadowOpacity: 0.05 },
  cardHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  cardTitle: { fontSize: 16, fontWeight: '600', color: '#1e293b', flex: 1 },
  badge: { paddingHorizontal: 8, paddingVertical: 3, borderRadius: 6 },
  badgeText: { fontSize: 10, fontWeight: '700' },
  cardBody: { marginTop: 12, gap: 6 },
  infoRow: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  infoText: { fontSize: 13, color: '#64748b', textTransform: 'capitalize' },
  empty: { textAlign: 'center', color: '#94a3b8', marginTop: 48 },
});
