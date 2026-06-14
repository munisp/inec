import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, RefreshControl, Image } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { apiGet } from '../lib/api';

interface Aspirant {
  id: number;
  full_name: string;
  position_sought: string;
  party_id: number;
  state_of_origin: string;
  screening_status: string;
  deposit_paid: boolean;
  delegate_support_count: number;
  manifesto_summary: string;
}

const screeningColors: Record<string, { bg: string; fg: string }> = {
  pending: { bg: '#fef3c7', fg: '#a16207' },
  cleared: { bg: '#dcfce7', fg: '#15803d' },
  disqualified: { bg: '#fee2e2', fg: '#dc2626' },
  withdrawn: { bg: '#f1f5f9', fg: '#64748b' },
  appealing: { bg: '#dbeafe', fg: '#2563eb' },
};

export default function AspirantsScreen() {
  const [aspirants, setAspirants] = useState<Aspirant[]>([]);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const data = await apiGet('/gotv/primaries/aspirants?election_id=1');
      setAspirants(data.aspirants || []);
    } catch {}
  };

  useEffect(() => { load(); }, []);

  return (
    <FlatList
      data={aspirants}
      keyExtractor={(item) => item.id.toString()}
      renderItem={({ item }) => {
        const sc = screeningColors[item.screening_status] || screeningColors.pending;
        return (
          <View style={s.card}>
            <View style={s.row}>
              <View style={s.avatar}>
                <Ionicons name="person" size={24} color="#7c3aed" />
              </View>
              <View style={s.info}>
                <Text style={s.name}>{item.full_name}</Text>
                <Text style={s.position}>{item.position_sought?.replace(/_/g, ' ')}</Text>
              </View>
              <View style={[s.badge, { backgroundColor: sc.bg }]}>
                <Text style={[s.badgeText, { color: sc.fg }]}>{item.screening_status}</Text>
              </View>
            </View>

            <View style={s.details}>
              <View style={s.detailRow}>
                <Ionicons name="location-outline" size={14} color="#64748b" />
                <Text style={s.detailText}>{item.state_of_origin}</Text>
              </View>
              <View style={s.detailRow}>
                <Ionicons name="people-outline" size={14} color="#64748b" />
                <Text style={s.detailText}>{item.delegate_support_count} delegate support</Text>
              </View>
              <View style={s.detailRow}>
                <Ionicons name={item.deposit_paid ? 'checkmark-circle' : 'close-circle'} size={14} color={item.deposit_paid ? '#15803d' : '#dc2626'} />
                <Text style={s.detailText}>Deposit {item.deposit_paid ? 'Paid' : 'Pending'}</Text>
              </View>
            </View>

            {item.manifesto_summary && (
              <Text style={s.manifesto} numberOfLines={2}>{item.manifesto_summary}</Text>
            )}
          </View>
        );
      }}
      contentContainerStyle={s.list}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={async () => { setRefreshing(true); await load(); setRefreshing(false); }} />}
      ListEmptyComponent={<Text style={s.empty}>No aspirants registered</Text>}
    />
  );
}

const s = StyleSheet.create({
  list: { padding: 16, gap: 12 },
  card: { backgroundColor: '#fff', borderRadius: 14, padding: 14, elevation: 2 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 12 },
  avatar: { width: 48, height: 48, borderRadius: 24, backgroundColor: '#f5f3ff', justifyContent: 'center', alignItems: 'center' },
  info: { flex: 1 },
  name: { fontSize: 16, fontWeight: '700', color: '#1e293b' },
  position: { fontSize: 12, color: '#64748b', textTransform: 'capitalize', marginTop: 2 },
  badge: { paddingHorizontal: 10, paddingVertical: 4, borderRadius: 8 },
  badgeText: { fontSize: 10, fontWeight: '700', textTransform: 'uppercase' },
  details: { marginTop: 12, gap: 6 },
  detailRow: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  detailText: { fontSize: 13, color: '#475569' },
  manifesto: { fontSize: 12, color: '#94a3b8', marginTop: 8, fontStyle: 'italic' },
  empty: { textAlign: 'center', color: '#94a3b8', marginTop: 48 },
});
