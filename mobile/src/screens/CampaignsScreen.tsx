import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, RefreshControl, TouchableOpacity } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { apiGet } from '../lib/api';

interface Campaign {
  id: number;
  name: string;
  campaign_type: string;
  status: string;
  target_count: number;
  reached_count: number;
  state_code: string;
  created_at: string;
}

const typeIcons: Record<string, string> = {
  sms: 'chatbubble', phone_bank: 'call', door_to_door: 'walk', rally: 'megaphone',
  whatsapp: 'logo-whatsapp', social_media: 'share-social',
};

export default function CampaignsScreen() {
  const [campaigns, setCampaigns] = useState<Campaign[]>([]);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const data = await apiGet('/gotv/campaigns');
      setCampaigns(data.campaigns || []);
    } catch {}
  };

  useEffect(() => { load(); }, []);

  return (
    <FlatList
      data={campaigns}
      keyExtractor={(item) => item.id.toString()}
      renderItem={({ item }) => {
        const progress = item.target_count > 0 ? (item.reached_count / item.target_count) * 100 : 0;
        return (
          <View style={s.card}>
            <View style={s.row}>
              <Ionicons name={(typeIcons[item.campaign_type] || 'megaphone') as any} size={20} color="#2563eb" />
              <Text style={s.name}>{item.name}</Text>
              <View style={[s.badge, { backgroundColor: item.status === 'active' ? '#dcfce7' : '#f1f5f9' }]}>
                <Text style={[s.badgeText, { color: item.status === 'active' ? '#15803d' : '#64748b' }]}>{item.status}</Text>
              </View>
            </View>
            <Text style={s.type}>{item.campaign_type?.replace(/_/g, ' ')} | {item.state_code || 'All'}</Text>
            <View style={s.progressRow}>
              <View style={s.progressBar}>
                <View style={[s.progressFill, { width: `${Math.min(progress, 100)}%` }]} />
              </View>
              <Text style={s.progressText}>{progress.toFixed(0)}%</Text>
            </View>
            <Text style={s.reach}>{item.reached_count?.toLocaleString()} / {item.target_count?.toLocaleString()} reached</Text>
          </View>
        );
      }}
      contentContainerStyle={s.list}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={async () => { setRefreshing(true); await load(); setRefreshing(false); }} />}
      ListEmptyComponent={<Text style={s.empty}>No campaigns</Text>}
    />
  );
}

const s = StyleSheet.create({
  list: { padding: 16, gap: 10 },
  card: { backgroundColor: '#fff', borderRadius: 12, padding: 14, elevation: 1 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  name: { flex: 1, fontSize: 15, fontWeight: '600', color: '#1e293b' },
  badge: { paddingHorizontal: 8, paddingVertical: 2, borderRadius: 6 },
  badgeText: { fontSize: 10, fontWeight: '600', textTransform: 'uppercase' },
  type: { fontSize: 12, color: '#64748b', marginTop: 6, textTransform: 'capitalize' },
  progressRow: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: 10 },
  progressBar: { flex: 1, height: 6, backgroundColor: '#e2e8f0', borderRadius: 3, overflow: 'hidden' },
  progressFill: { height: '100%', backgroundColor: '#2563eb', borderRadius: 3 },
  progressText: { fontSize: 12, fontWeight: '600', color: '#475569', width: 36, textAlign: 'right' },
  reach: { fontSize: 11, color: '#94a3b8', marginTop: 4 },
  empty: { textAlign: 'center', color: '#94a3b8', marginTop: 48 },
});
