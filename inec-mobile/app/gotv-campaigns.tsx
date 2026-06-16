// GOTV Campaigns — create, list, launch campaigns with multi-channel dispatch.

import { useState, useCallback } from 'react';
import {
  View, Text, StyleSheet, ScrollView, TouchableOpacity,
  RefreshControl, TextInput, Alert, Platform, Modal,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { useFocusEffect } from 'expo-router';
import { EmptyState } from '../src/components/EmptyState';
import { CardSkeleton } from '../src/components/SkeletonLoader';
import { gotvFetch } from '../lib/gotv-auth';

interface Campaign {
  campaign_id: string;
  name: string;
  campaign_type: string;
  status: string;
  total_contacts: number;
  contacts_reached: number;
  created_at: string;
  message_template: string;
}

const CAMPAIGN_TYPES = [
  'sms', 'whatsapp', 'email', 'push', 'ussd',
  'twitter', 'facebook', 'instagram', 'tiktok',
  'phone_bank', 'door_to_door',
];

const STATUS_COLORS: Record<string, { color: string; bg: string }> = {
  draft: { color: '#6b7280', bg: '#f3f4f6' },
  active: { color: '#22c55e', bg: '#f0fdf4' },
  paused: { color: '#f59e0b', bg: '#fffbeb' },
  completed: { color: '#8b5cf6', bg: '#f5f3ff' },
  scheduled: { color: '#3b82f6', bg: '#dbeafe' },
  cancelled: { color: '#ef4444', bg: '#fef2f2' },
};

export default function GOTVCampaignsScreen() {
  const [campaigns, setCampaigns] = useState<Campaign[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [showCreate, setShowCreate] = useState(false);
  const [newName, setNewName] = useState('');
  const [newType, setNewType] = useState('sms');
  const [newMessage, setNewMessage] = useState('');
  const [creating, setCreating] = useState(false);

  const load = useCallback(async () => {
    try {
      const data = await gotvFetch<{ campaigns: Campaign[] }>('/gotv/campaigns');
      setCampaigns(data.campaigns || []);
    } catch { /* empty */ }
    setLoading(false);
  }, []);

  useFocusEffect(useCallback(() => { load(); }, [load]));

  const onRefresh = useCallback(async () => {
    setRefreshing(true);
    if (Platform.OS !== 'web') Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    await load();
    setRefreshing(false);
  }, [load]);

  const createCampaign = async () => {
    if (!newName.trim() || !newMessage.trim()) {
      Alert.alert('Error', 'Name and message are required');
      return;
    }
    setCreating(true);
    try {
      await gotvFetch('/gotv/campaigns', {
        method: 'POST',
        body: JSON.stringify({
          name: newName.trim(),
          campaign_type: newType,
          message_template: newMessage.trim(),
        }),
      });
      setShowCreate(false);
      setNewName('');
      setNewMessage('');
      await load();
    } catch (e) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed to create campaign');
    }
    setCreating(false);
  };

  const launchCampaign = async (id: string) => {
    Alert.alert('Launch Campaign', 'This will begin sending messages to all targeted contacts.', [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Launch', style: 'destructive',
        onPress: async () => {
          try {
            await gotvFetch(`/gotv/campaigns/${id}/launch`, { method: 'POST' });
            await load();
          } catch (e) {
            Alert.alert('Error', e instanceof Error ? e.message : 'Launch failed');
          }
        },
      },
    ]);
  };

  if (loading) return <CardSkeleton />;

  return (
    <ScrollView
      style={styles.container}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor="#006b3f" />}
    >
      <View style={styles.header}>
        <Text style={styles.title}>Campaigns</Text>
        <TouchableOpacity style={styles.createBtn} onPress={() => setShowCreate(true)}>
          <Ionicons name="add-circle" size={20} color="#fff" />
          <Text style={styles.createBtnText}>New</Text>
        </TouchableOpacity>
      </View>

      <View style={styles.stats}>
        <View style={styles.statBox}>
          <Text style={styles.statNum}>{campaigns.length}</Text>
          <Text style={styles.statLabel}>Total</Text>
        </View>
        <View style={styles.statBox}>
          <Text style={styles.statNum}>{campaigns.filter(c => c.status === 'active').length}</Text>
          <Text style={styles.statLabel}>Active</Text>
        </View>
        <View style={styles.statBox}>
          <Text style={styles.statNum}>
            {campaigns.reduce((s, c) => s + (c.contacts_reached || 0), 0)}
          </Text>
          <Text style={styles.statLabel}>Reached</Text>
        </View>
      </View>

      {campaigns.length === 0 ? (
        <EmptyState icon="megaphone-outline" title="No campaigns" description="Create your first campaign" />
      ) : (
        campaigns.map(c => {
          const st = STATUS_COLORS[c.status] || STATUS_COLORS.draft;
          const pct = c.total_contacts > 0 ? Math.round((c.contacts_reached / c.total_contacts) * 100) : 0;
          return (
            <View key={c.campaign_id} style={styles.card}>
              <View style={styles.cardHeader}>
                <Text style={styles.cardTitle} numberOfLines={1}>{c.name}</Text>
                <View style={[styles.badge, { backgroundColor: st.bg }]}>
                  <Text style={[styles.badgeText, { color: st.color }]}>{c.status}</Text>
                </View>
              </View>
              <View style={styles.cardMeta}>
                <Ionicons name="paper-plane" size={14} color="#6b7280" />
                <Text style={styles.metaText}>{c.campaign_type}</Text>
                <Ionicons name="people" size={14} color="#6b7280" style={{ marginLeft: 12 }} />
                <Text style={styles.metaText}>{c.contacts_reached}/{c.total_contacts} ({pct}%)</Text>
              </View>
              <View style={styles.progressBar}>
                <View style={[styles.progressFill, { width: `${pct}%` }]} />
              </View>
              {c.status === 'draft' && (
                <TouchableOpacity style={styles.launchBtn} onPress={() => launchCampaign(c.campaign_id)}>
                  <Ionicons name="rocket" size={16} color="#fff" />
                  <Text style={styles.launchBtnText}>Launch</Text>
                </TouchableOpacity>
              )}
            </View>
          );
        })
      )}

      <Modal visible={showCreate} animationType="slide" presentationStyle="pageSheet">
        <View style={styles.modal}>
          <View style={styles.modalHeader}>
            <Text style={styles.modalTitle}>New Campaign</Text>
            <TouchableOpacity onPress={() => setShowCreate(false)}>
              <Ionicons name="close" size={24} color="#374151" />
            </TouchableOpacity>
          </View>
          <TextInput style={styles.input} placeholder="Campaign name" value={newName} onChangeText={setNewName} />
          <Text style={styles.label}>Channel</Text>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.typeRow}>
            {CAMPAIGN_TYPES.map(t => (
              <TouchableOpacity
                key={t}
                style={[styles.typeChip, newType === t && styles.typeChipActive]}
                onPress={() => setNewType(t)}
              >
                <Text style={[styles.typeChipText, newType === t && styles.typeChipTextActive]}>{t}</Text>
              </TouchableOpacity>
            ))}
          </ScrollView>
          <TextInput
            style={[styles.input, { height: 100, textAlignVertical: 'top' }]}
            placeholder="Message template (use {{name}}, {{pu}}, {{party}})"
            value={newMessage}
            onChangeText={setNewMessage}
            multiline
          />
          <TouchableOpacity style={styles.submitBtn} onPress={createCampaign} disabled={creating}>
            <Text style={styles.submitBtnText}>{creating ? 'Creating...' : 'Create Campaign'}</Text>
          </TouchableOpacity>
        </View>
      </Modal>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  header: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', padding: 16 },
  title: { fontSize: 22, fontWeight: '700', color: '#111827' },
  createBtn: { flexDirection: 'row', alignItems: 'center', backgroundColor: '#006b3f', paddingHorizontal: 12, paddingVertical: 8, borderRadius: 8, gap: 4 },
  createBtnText: { color: '#fff', fontWeight: '600', fontSize: 14 },
  stats: { flexDirection: 'row', paddingHorizontal: 16, gap: 12, marginBottom: 12 },
  statBox: { flex: 1, backgroundColor: '#fff', padding: 12, borderRadius: 10, alignItems: 'center', shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 2, elevation: 1 },
  statNum: { fontSize: 20, fontWeight: '700', color: '#006b3f' },
  statLabel: { fontSize: 12, color: '#6b7280', marginTop: 2 },
  card: { backgroundColor: '#fff', marginHorizontal: 16, marginBottom: 12, borderRadius: 12, padding: 14, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.06, shadowRadius: 3, elevation: 2 },
  cardHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  cardTitle: { fontSize: 16, fontWeight: '600', color: '#111827', flex: 1, marginRight: 8 },
  badge: { paddingHorizontal: 8, paddingVertical: 3, borderRadius: 6 },
  badgeText: { fontSize: 11, fontWeight: '600', textTransform: 'uppercase' },
  cardMeta: { flexDirection: 'row', alignItems: 'center', marginTop: 8, gap: 4 },
  metaText: { fontSize: 13, color: '#6b7280' },
  progressBar: { height: 4, backgroundColor: '#e5e7eb', borderRadius: 2, marginTop: 10 },
  progressFill: { height: 4, backgroundColor: '#006b3f', borderRadius: 2 },
  launchBtn: { flexDirection: 'row', alignItems: 'center', backgroundColor: '#006b3f', paddingHorizontal: 12, paddingVertical: 8, borderRadius: 8, alignSelf: 'flex-start', marginTop: 10, gap: 4 },
  launchBtnText: { color: '#fff', fontWeight: '600', fontSize: 13 },
  modal: { flex: 1, padding: 20, backgroundColor: '#fff' },
  modalHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 20 },
  modalTitle: { fontSize: 20, fontWeight: '700', color: '#111827' },
  input: { borderWidth: 1, borderColor: '#d1d5db', borderRadius: 10, paddingHorizontal: 14, paddingVertical: 12, fontSize: 15, marginBottom: 14, backgroundColor: '#f9fafb' },
  label: { fontSize: 14, fontWeight: '600', color: '#374151', marginBottom: 8 },
  typeRow: { marginBottom: 14 },
  typeChip: { paddingHorizontal: 12, paddingVertical: 6, borderRadius: 16, backgroundColor: '#f3f4f6', marginRight: 8 },
  typeChipActive: { backgroundColor: '#006b3f' },
  typeChipText: { fontSize: 13, color: '#374151' },
  typeChipTextActive: { color: '#fff', fontWeight: '600' },
  submitBtn: { backgroundColor: '#006b3f', paddingVertical: 14, borderRadius: 10, alignItems: 'center', marginTop: 8 },
  submitBtnText: { color: '#fff', fontWeight: '700', fontSize: 16 },
});
