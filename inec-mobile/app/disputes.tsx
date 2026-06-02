import { useState, useCallback } from 'react';
import {
  View, Text, StyleSheet, ScrollView, TouchableOpacity, TextInput,
  RefreshControl, Platform, Alert, Modal,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { useFocusEffect } from 'expo-router';
import { disputeApi, Dispute, DisputeStats } from '../src/lib/api';
import { EmptyState } from '../src/components/EmptyState';
import { CardSkeleton } from '../src/components/SkeletonLoader';

const STATUS_CONFIG: Record<string, { color: string; bg: string; icon: keyof typeof Ionicons.glyphMap }> = {
  filed: { color: '#ef4444', bg: '#fef2f2', icon: 'document-text' },
  under_review: { color: '#f59e0b', bg: '#fffbeb', icon: 'time' },
  escalated: { color: '#8b5cf6', bg: '#ede9fe', icon: 'arrow-up-circle' },
  resolved: { color: '#22c55e', bg: '#f0fdf4', icon: 'checkmark-circle' },
  dismissed: { color: '#6b7280', bg: '#f9fafb', icon: 'close-circle' },
};

const CATEGORIES = ['vote_count_discrepancy', 'voter_intimidation', 'ballot_tampering', 'late_opening', 'electoral_violence', 'other'];

export default function DisputesScreen() {
  const [disputes, setDisputes] = useState<Dispute[]>([]);
  const [stats, setStats] = useState<DisputeStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [showFileModal, setShowFileModal] = useState(false);
  const [filing, setFiling] = useState(false);
  const [formCategory, setFormCategory] = useState('');
  const [formDescription, setFormDescription] = useState('');
  const [formPuCode, setFormPuCode] = useState('');

  const loadData = useCallback(async () => {
    try {
      const [d, s] = await Promise.all([disputeApi.list(), disputeApi.stats()]);
      setDisputes(d);
      setStats(s);
    } catch { /* */ }
    setLoading(false);
  }, []);

  useFocusEffect(useCallback(() => { loadData(); }, [loadData]));

  const onRefresh = useCallback(async () => {
    setRefreshing(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    await loadData();
    setRefreshing(false);
  }, [loadData]);

  const fileDispute = async () => {
    if (!formCategory || !formDescription.trim() || !formPuCode.trim()) {
      Alert.alert('Missing Fields', 'Please fill all required fields');
      return;
    }
    setFiling(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      await disputeApi.file({
        election_id: 1,
        polling_unit_code: formPuCode,
        category: formCategory,
        description: formDescription,
      });
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
      setShowFileModal(false);
      setFormCategory('');
      setFormDescription('');
      setFormPuCode('');
      await loadData();
    } catch (e) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed to file dispute');
    }
    setFiling(false);
  };

  if (loading) return <View style={styles.container}><CardSkeleton /><CardSkeleton /></View>;

  return (
    <View style={styles.container}>
      <ScrollView
        contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} colors={['#166534']} tintColor="#166534" />}
      >
        {/* Stats cards */}
        {stats && (
          <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.statsRow}>
            <View style={[styles.statCard, { borderTopColor: '#166534' }]}>
              <Text style={styles.statNumber}>{stats.total}</Text>
              <Text style={styles.statLabel}>Total</Text>
            </View>
            {Object.entries(stats.by_status).map(([status, count]) => {
              const cfg = STATUS_CONFIG[status] || STATUS_CONFIG.filed;
              return (
                <View key={status} style={[styles.statCard, { borderTopColor: cfg.color }]}>
                  <Text style={[styles.statNumber, { color: cfg.color }]}>{count}</Text>
                  <Text style={styles.statLabel}>{status.replace(/_/g, ' ')}</Text>
                </View>
              );
            })}
          </ScrollView>
        )}

        {disputes.length === 0 ? (
          <EmptyState icon="shield-outline" title="No Disputes" description="No election disputes have been filed yet" />
        ) : (
          disputes.map((d) => {
            const cfg = STATUS_CONFIG[d.status] || STATUS_CONFIG.filed;
            return (
              <TouchableOpacity key={d.id} style={styles.disputeCard} activeOpacity={0.7}>
                <View style={styles.disputeHeader}>
                  <View style={[styles.statusBadge, { backgroundColor: cfg.bg }]}>
                    <Ionicons name={cfg.icon} size={12} color={cfg.color} />
                    <Text style={[styles.statusText, { color: cfg.color }]}>{d.status.replace(/_/g, ' ')}</Text>
                  </View>
                  <View style={[styles.priorityBadge, { backgroundColor: d.priority === 'high' ? '#fef2f2' : d.priority === 'medium' ? '#fffbeb' : '#f0fdf4' }]}>
                    <Text style={[styles.priorityText, { color: d.priority === 'high' ? '#ef4444' : d.priority === 'medium' ? '#f59e0b' : '#22c55e' }]}>
                      {d.priority}
                    </Text>
                  </View>
                </View>
                <Text style={styles.disputeCategory}>{d.category.replace(/_/g, ' ')}</Text>
                <Text style={styles.disputeDescription} numberOfLines={2}>{d.description}</Text>
                <View style={styles.disputeMeta}>
                  <View style={styles.metaItem}>
                    <Ionicons name="location-outline" size={12} color="#9ca3af" />
                    <Text style={styles.metaText}>{d.polling_unit_code}</Text>
                  </View>
                  <View style={styles.metaItem}>
                    <Ionicons name="time-outline" size={12} color="#9ca3af" />
                    <Text style={styles.metaText}>{new Date(d.filed_at).toLocaleDateString()}</Text>
                  </View>
                </View>
              </TouchableOpacity>
            );
          })
        )}
      </ScrollView>

      {/* FAB */}
      <TouchableOpacity
        style={styles.fab}
        onPress={() => { Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium); setShowFileModal(true); }}
        activeOpacity={0.8}
      >
        <Ionicons name="add" size={24} color="#fff" />
      </TouchableOpacity>

      {/* File Dispute Modal */}
      <Modal visible={showFileModal} animationType="slide" presentationStyle="pageSheet">
        <View style={styles.modalContainer}>
          <View style={styles.modalHeader}>
            <Text style={styles.modalTitle}>File Dispute</Text>
            <TouchableOpacity onPress={() => setShowFileModal(false)}>
              <Ionicons name="close" size={24} color="#6b7280" />
            </TouchableOpacity>
          </View>

          <ScrollView style={styles.modalBody}>
            <Text style={styles.fieldLabel}>Polling Unit Code *</Text>
            <TextInput style={styles.textInput} value={formPuCode} onChangeText={setFormPuCode} placeholder="e.g. PU-001" placeholderTextColor="#9ca3af" />

            <Text style={styles.fieldLabel}>Category *</Text>
            <View style={styles.categoryGrid}>
              {CATEGORIES.map((cat) => (
                <TouchableOpacity
                  key={cat}
                  style={[styles.categoryChip, formCategory === cat && styles.categoryChipActive]}
                  onPress={() => { Haptics.selectionAsync(); setFormCategory(cat); }}
                >
                  <Text style={[styles.categoryChipText, formCategory === cat && styles.categoryChipTextActive]}>
                    {cat.replace(/_/g, ' ')}
                  </Text>
                </TouchableOpacity>
              ))}
            </View>

            <Text style={styles.fieldLabel}>Description *</Text>
            <TextInput
              style={[styles.textInput, styles.textArea]}
              value={formDescription}
              onChangeText={setFormDescription}
              placeholder="Describe the incident in detail..."
              placeholderTextColor="#9ca3af"
              multiline
              numberOfLines={4}
              textAlignVertical="top"
            />

            <TouchableOpacity style={styles.submitButton} onPress={fileDispute} disabled={filing} activeOpacity={0.8}>
              <Ionicons name="paper-plane" size={18} color="#fff" />
              <Text style={styles.submitButtonText}>{filing ? 'Filing...' : 'Submit Dispute'}</Text>
            </TouchableOpacity>
          </ScrollView>
        </View>
      </Modal>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  statsRow: { paddingHorizontal: 16, paddingVertical: 12, gap: 10 },
  statCard: { backgroundColor: '#fff', borderRadius: 12, padding: 14, minWidth: 90, borderTopWidth: 3, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.04, shadowRadius: 4, elevation: 1 },
  statNumber: { fontSize: 22, fontWeight: '800', color: '#111827' },
  statLabel: { fontSize: 11, color: '#6b7280', textTransform: 'capitalize', marginTop: 2 },
  disputeCard: { marginHorizontal: 16, marginBottom: 10, backgroundColor: '#fff', borderRadius: 14, padding: 16, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  disputeHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 },
  statusBadge: { flexDirection: 'row', alignItems: 'center', gap: 4, paddingHorizontal: 8, paddingVertical: 4, borderRadius: 8 },
  statusText: { fontSize: 11, fontWeight: '700', textTransform: 'capitalize' },
  priorityBadge: { paddingHorizontal: 8, paddingVertical: 4, borderRadius: 8 },
  priorityText: { fontSize: 11, fontWeight: '700', textTransform: 'uppercase' },
  disputeCategory: { fontSize: 14, fontWeight: '700', color: '#111827', textTransform: 'capitalize', marginBottom: 4 },
  disputeDescription: { fontSize: 13, color: '#6b7280', lineHeight: 18 },
  disputeMeta: { flexDirection: 'row', gap: 16, marginTop: 10 },
  metaItem: { flexDirection: 'row', alignItems: 'center', gap: 4 },
  metaText: { fontSize: 11, color: '#9ca3af' },
  fab: { position: 'absolute', right: 20, bottom: Platform.OS === 'ios' ? 100 : 80, width: 56, height: 56, borderRadius: 28, backgroundColor: '#166534', justifyContent: 'center', alignItems: 'center', shadowColor: '#166534', shadowOffset: { width: 0, height: 4 }, shadowOpacity: 0.3, shadowRadius: 8, elevation: 6 },
  modalContainer: { flex: 1, backgroundColor: '#fff' },
  modalHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', padding: 16, borderBottomWidth: 1, borderBottomColor: '#f3f4f6' },
  modalTitle: { fontSize: 18, fontWeight: '700', color: '#111827' },
  modalBody: { padding: 16 },
  fieldLabel: { fontSize: 13, fontWeight: '600', color: '#374151', marginBottom: 6, marginTop: 16 },
  textInput: { backgroundColor: '#f9fafb', borderWidth: 1, borderColor: '#e5e7eb', borderRadius: 12, padding: 12, fontSize: 15, color: '#111827' },
  textArea: { height: 120, paddingTop: 12 },
  categoryGrid: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  categoryChip: { paddingHorizontal: 12, paddingVertical: 8, borderRadius: 10, backgroundColor: '#f3f4f6', borderWidth: 1.5, borderColor: 'transparent' },
  categoryChipActive: { backgroundColor: '#dcfce7', borderColor: '#166534' },
  categoryChipText: { fontSize: 12, fontWeight: '600', color: '#6b7280', textTransform: 'capitalize' },
  categoryChipTextActive: { color: '#166534' },
  submitButton: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 8, backgroundColor: '#166534', paddingVertical: 14, borderRadius: 14, marginTop: 24, marginBottom: 40 },
  submitButtonText: { fontSize: 16, fontWeight: '700', color: '#fff' },
});
