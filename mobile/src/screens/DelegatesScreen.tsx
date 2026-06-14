import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, RefreshControl, TextInput } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { apiGet } from '../lib/api';

interface Delegate {
  id: number;
  full_name: string;
  delegate_type: string;
  state_code: string;
  constituency: string;
  accreditation_status: string;
  checked_in: boolean;
  credential_number: string;
}

const typeColors: Record<string, string> = {
  statutory: '#2563eb', elected: '#15803d', ex_officio: '#7c3aed', special: '#ea580c',
};

export default function DelegatesScreen() {
  const [delegates, setDelegates] = useState<Delegate[]>([]);
  const [refreshing, setRefreshing] = useState(false);
  const [search, setSearch] = useState('');
  const [summary, setSummary] = useState({ total: 0, accredited: 0, checkedIn: 0 });

  const load = async () => {
    try {
      const data = await apiGet('/gotv/primaries/delegates?election_id=1');
      const list = data.delegates || [];
      setDelegates(list);
      setSummary({
        total: list.length,
        accredited: list.filter((d: Delegate) => d.accreditation_status === 'accredited').length,
        checkedIn: list.filter((d: Delegate) => d.checked_in).length,
      });
    } catch {}
  };

  useEffect(() => { load(); }, []);

  const filtered = search
    ? delegates.filter(d => d.full_name?.toLowerCase().includes(search.toLowerCase()) || d.credential_number?.includes(search))
    : delegates;

  return (
    <View style={s.container}>
      <View style={s.statsRow}>
        <View style={s.statCard}><Text style={s.statValue}>{summary.total}</Text><Text style={s.statLabel}>Total</Text></View>
        <View style={s.statCard}><Text style={[s.statValue, { color: '#2563eb' }]}>{summary.accredited}</Text><Text style={s.statLabel}>Accredited</Text></View>
        <View style={s.statCard}><Text style={[s.statValue, { color: '#15803d' }]}>{summary.checkedIn}</Text><Text style={s.statLabel}>Checked In</Text></View>
      </View>

      <View style={s.searchRow}>
        <Ionicons name="search" size={18} color="#94a3b8" />
        <TextInput style={s.searchInput} placeholder="Search delegates..." placeholderTextColor="#94a3b8" value={search} onChangeText={setSearch} />
      </View>

      <FlatList
        data={filtered}
        keyExtractor={(item) => item.id.toString()}
        renderItem={({ item }) => (
          <View style={s.card}>
            <View style={s.row}>
              <View style={[s.typeDot, { backgroundColor: typeColors[item.delegate_type] || '#94a3b8' }]} />
              <View style={s.info}>
                <Text style={s.name}>{item.full_name}</Text>
                <Text style={s.meta}>{item.credential_number} | {item.state_code} | {item.constituency}</Text>
              </View>
              <View style={s.statusIcons}>
                <Ionicons name={item.accreditation_status === 'accredited' ? 'checkmark-circle' : 'time'} size={18} color={item.accreditation_status === 'accredited' ? '#15803d' : '#ca8a04'} />
                {item.checked_in && <Ionicons name="log-in" size={18} color="#2563eb" />}
              </View>
            </View>
            <View style={[s.typeBadge, { backgroundColor: (typeColors[item.delegate_type] || '#94a3b8') + '15' }]}>
              <Text style={[s.typeText, { color: typeColors[item.delegate_type] || '#94a3b8' }]}>
                {item.delegate_type?.replace(/_/g, ' ')}
              </Text>
            </View>
          </View>
        )}
        contentContainerStyle={s.list}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={async () => { setRefreshing(true); await load(); setRefreshing(false); }} />}
        ListEmptyComponent={<Text style={s.empty}>No delegates found</Text>}
      />
    </View>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  statsRow: { flexDirection: 'row', padding: 16, gap: 8 },
  statCard: { flex: 1, backgroundColor: '#fff', borderRadius: 10, padding: 10, alignItems: 'center', elevation: 1 },
  statValue: { fontSize: 20, fontWeight: '700', color: '#1e293b' },
  statLabel: { fontSize: 10, color: '#64748b', marginTop: 2 },
  searchRow: { flexDirection: 'row', alignItems: 'center', backgroundColor: '#fff', marginHorizontal: 16, borderRadius: 12, paddingHorizontal: 14, height: 42, borderWidth: 1, borderColor: '#e2e8f0', gap: 8 },
  searchInput: { flex: 1, fontSize: 14, color: '#1e293b' },
  list: { padding: 16, gap: 8 },
  card: { backgroundColor: '#fff', borderRadius: 12, padding: 12, elevation: 1 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  typeDot: { width: 8, height: 8, borderRadius: 4 },
  info: { flex: 1 },
  name: { fontSize: 14, fontWeight: '600', color: '#1e293b' },
  meta: { fontSize: 11, color: '#94a3b8', marginTop: 2 },
  statusIcons: { flexDirection: 'row', gap: 4 },
  typeBadge: { alignSelf: 'flex-start', paddingHorizontal: 8, paddingVertical: 2, borderRadius: 6, marginTop: 6 },
  typeText: { fontSize: 10, fontWeight: '600', textTransform: 'uppercase' },
  empty: { textAlign: 'center', color: '#94a3b8', marginTop: 48 },
});
