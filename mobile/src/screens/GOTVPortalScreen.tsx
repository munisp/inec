import React, { useEffect, useState } from 'react';
import { View, Text, ScrollView, StyleSheet, TouchableOpacity, RefreshControl } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { apiGet } from '../lib/api';

interface GOTVStats {
  total_contacts: number;
  total_volunteers: number;
  total_pledges: number;
  active_campaigns: number;
  total_rides: number;
  pending_rides: number;
}

export default function GOTVPortalScreen({ navigation }: any) {
  const [stats, setStats] = useState<GOTVStats | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const data = await apiGet('/gotv/dashboard');
      setStats(data);
    } catch {}
  };

  useEffect(() => { load(); }, []);
  const onRefresh = async () => { setRefreshing(true); await load(); setRefreshing(false); };

  const menuItems = [
    { title: 'Campaigns', icon: 'megaphone' as const, screen: 'Campaigns', count: stats?.active_campaigns, color: '#2563eb' },
    { title: 'Contacts', icon: 'people' as const, screen: 'Contacts', count: stats?.total_contacts, color: '#15803d' },
    { title: 'Volunteers', icon: 'hand-left' as const, screen: 'Volunteers', count: stats?.total_volunteers, color: '#7c3aed' },
    { title: 'War Room', icon: 'radio' as const, screen: 'WarRoom', count: null, color: '#dc2626' },
    { title: 'Field Tasks', icon: 'checkbox' as const, screen: 'Tasks', count: null, color: '#ca8a04' },
    { title: 'Scoring', icon: 'trophy' as const, screen: 'Scoring', count: null, color: '#ea580c' },
  ];

  return (
    <ScrollView style={s.container} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} />}>
      <View style={s.header}>
        <Text style={s.title}>GOTV Command Center</Text>
        <Text style={s.subtitle}>Get Out The Vote — Party Mobilization</Text>
      </View>

      <View style={s.statsRow}>
        {[
          { label: 'Contacts', value: stats?.total_contacts || 0 },
          { label: 'Volunteers', value: stats?.total_volunteers || 0 },
          { label: 'Pledges', value: stats?.total_pledges || 0 },
          { label: 'Rides', value: stats?.total_rides || 0 },
        ].map((s2) => (
          <View key={s2.label} style={s.statCard}>
            <Text style={s.statValue}>{s2.value.toLocaleString()}</Text>
            <Text style={s.statLabel}>{s2.label}</Text>
          </View>
        ))}
      </View>

      <View style={s.grid}>
        {menuItems.map((item) => (
          <TouchableOpacity
            key={item.title}
            style={s.menuCard}
            onPress={() => navigation.navigate(item.screen)}
          >
            <View style={[s.menuIcon, { backgroundColor: item.color + '15' }]}>
              <Ionicons name={item.icon} size={24} color={item.color} />
            </View>
            <Text style={s.menuTitle}>{item.title}</Text>
            {item.count != null && <Text style={s.menuCount}>{item.count.toLocaleString()}</Text>}
          </TouchableOpacity>
        ))}
      </View>
    </ScrollView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  header: { padding: 20 },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b' },
  subtitle: { fontSize: 13, color: '#64748b', marginTop: 2 },
  statsRow: { flexDirection: 'row', paddingHorizontal: 12, gap: 6 },
  statCard: { flex: 1, backgroundColor: '#fff', borderRadius: 10, padding: 10, alignItems: 'center', elevation: 1 },
  statValue: { fontSize: 18, fontWeight: '700', color: '#1e293b' },
  statLabel: { fontSize: 10, color: '#64748b', marginTop: 2 },
  grid: { flexDirection: 'row', flexWrap: 'wrap', padding: 12, gap: 12, marginTop: 8 },
  menuCard: { width: '47%', backgroundColor: '#fff', borderRadius: 14, padding: 16, elevation: 2, alignItems: 'center' },
  menuIcon: { width: 48, height: 48, borderRadius: 14, justifyContent: 'center', alignItems: 'center', marginBottom: 10 },
  menuTitle: { fontSize: 14, fontWeight: '600', color: '#1e293b' },
  menuCount: { fontSize: 12, color: '#64748b', marginTop: 2 },
});
