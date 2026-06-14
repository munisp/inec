import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, RefreshControl, TextInput } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { apiGet } from '../lib/api';

interface Contact {
  contact_id: string;
  full_name_encrypted: string;
  state_code: string;
  lga_code: string;
  ward_code: string;
  support_level: number;
  pledge_status: string;
  contacted: boolean;
}

export default function ContactsScreen() {
  const [contacts, setContacts] = useState<Contact[]>([]);
  const [refreshing, setRefreshing] = useState(false);
  const [search, setSearch] = useState('');

  const load = async () => {
    try {
      const endpoint = search ? `/gotv/contacts/search?q=${encodeURIComponent(search)}` : '/gotv/contacts?page=1&limit=50';
      const data = await apiGet(endpoint);
      setContacts(data.contacts || []);
    } catch {}
  };

  useEffect(() => { load(); }, [search]);

  const supportColors = ['#dc2626', '#ea580c', '#ca8a04', '#16a34a', '#15803d'];

  return (
    <View style={s.container}>
      <View style={s.searchRow}>
        <Ionicons name="search" size={18} color="#94a3b8" />
        <TextInput
          style={s.searchInput}
          placeholder="Search contacts..."
          placeholderTextColor="#94a3b8"
          value={search}
          onChangeText={setSearch}
        />
      </View>

      <FlatList
        data={contacts}
        keyExtractor={(item) => item.contact_id}
        renderItem={({ item }) => (
          <View style={s.card}>
            <View style={s.row}>
              <View style={s.avatar}>
                <Text style={s.avatarText}>{(item.full_name_encrypted || '?')[0].toUpperCase()}</Text>
              </View>
              <View style={s.info}>
                <Text style={s.name}>{item.full_name_encrypted || item.contact_id}</Text>
                <Text style={s.location}>{item.state_code}/{item.lga_code}/{item.ward_code}</Text>
              </View>
              <View style={s.support}>
                <View style={[s.supportDot, { backgroundColor: supportColors[Math.min(item.support_level || 0, 4)] }]} />
                <Text style={s.supportText}>L{item.support_level || 0}</Text>
              </View>
            </View>
            {item.pledge_status && (
              <View style={s.pledgeBadge}>
                <Ionicons name="hand-right" size={12} color="#7c3aed" />
                <Text style={s.pledgeText}>{item.pledge_status}</Text>
              </View>
            )}
          </View>
        )}
        contentContainerStyle={s.list}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={async () => { setRefreshing(true); await load(); setRefreshing(false); }} />}
        ListEmptyComponent={<Text style={s.empty}>No contacts found</Text>}
      />
    </View>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  searchRow: { flexDirection: 'row', alignItems: 'center', backgroundColor: '#fff', margin: 16, marginBottom: 0, borderRadius: 12, paddingHorizontal: 14, height: 44, borderWidth: 1, borderColor: '#e2e8f0', gap: 8 },
  searchInput: { flex: 1, fontSize: 15, color: '#1e293b' },
  list: { padding: 16, gap: 8 },
  card: { backgroundColor: '#fff', borderRadius: 12, padding: 12, elevation: 1 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  avatar: { width: 36, height: 36, borderRadius: 18, backgroundColor: '#e2e8f0', justifyContent: 'center', alignItems: 'center' },
  avatarText: { fontSize: 14, fontWeight: '700', color: '#475569' },
  info: { flex: 1 },
  name: { fontSize: 14, fontWeight: '600', color: '#1e293b' },
  location: { fontSize: 11, color: '#94a3b8', marginTop: 2 },
  support: { flexDirection: 'row', alignItems: 'center', gap: 4 },
  supportDot: { width: 8, height: 8, borderRadius: 4 },
  supportText: { fontSize: 11, color: '#64748b', fontWeight: '600' },
  pledgeBadge: { flexDirection: 'row', alignItems: 'center', gap: 4, marginTop: 6, backgroundColor: '#f5f3ff', paddingHorizontal: 8, paddingVertical: 3, borderRadius: 6, alignSelf: 'flex-start' },
  pledgeText: { fontSize: 11, color: '#7c3aed' },
  empty: { textAlign: 'center', color: '#94a3b8', marginTop: 48 },
});
