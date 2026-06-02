import { useState, useCallback } from 'react';
import {
  View, Text, StyleSheet, ScrollView, TouchableOpacity,
  TextInput, RefreshControl, Platform, Alert,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { useFocusEffect } from 'expo-router';
import { observerApi, AlertRule } from '../../src/lib/api';
import { EmptyState } from '../../src/components/EmptyState';
import { FeedSkeleton } from '../../src/components/SkeletonLoader';

const ALERT_TYPES = ['threshold', 'anomaly', 'delayed_reporting', 'zero_votes'] as const;
const PARTIES = ['APC', 'PDP', 'LP', 'NNPP', 'ADC', 'SDP', 'APGA', 'YPP'];
const STATES = ['Lagos', 'Kano', 'Rivers', 'Abuja FCT', 'Oyo', 'Kaduna', 'Enugu', 'Delta'];

export default function AlertsScreen() {
  const [rules, setRules] = useState<AlertRule[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [selectedType, setSelectedType] = useState<typeof ALERT_TYPES[number]>('threshold');
  const [selectedParty, setSelectedParty] = useState('');
  const [selectedState, setSelectedState] = useState('');
  const [threshold, setThreshold] = useState('');

  const loadRules = useCallback(async () => {
    try {
      const data = await observerApi.alerts();
      setRules(data);
    } catch { /* ignore */ }
    setLoading(false);
  }, []);

  useFocusEffect(useCallback(() => { loadRules(); }, [loadRules]));

  const onRefresh = useCallback(async () => {
    setRefreshing(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    await loadRules();
    setRefreshing(false);
  }, [loadRules]);

  const saveRule = async () => {
    setSaving(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      await observerApi.createAlert({
        alert_type: selectedType,
        party: selectedParty || undefined,
        state: selectedState || undefined,
        threshold: threshold ? parseInt(threshold) : undefined,
      });
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
      setShowForm(false);
      setSelectedType('threshold');
      setSelectedParty('');
      setSelectedState('');
      setThreshold('');
      loadRules();
    } catch (e: unknown) {
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed to create alert');
    } finally {
      setSaving(false);
    }
  };

  const deleteRule = async (id: string) => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    Alert.alert('Delete Alert', 'Remove this alert rule?', [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Delete',
        style: 'destructive',
        onPress: async () => {
          try {
            await observerApi.deleteAlert(id);
            Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
            loadRules();
          } catch {
            Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
          }
        },
      },
    ]);
  };

  const alertTypeIcons: Record<string, { name: keyof typeof Ionicons.glyphMap; color: string; bg: string }> = {
    threshold: { name: 'trending-up', color: '#2563eb', bg: '#dbeafe' },
    anomaly: { name: 'warning', color: '#dc2626', bg: '#fef2f2' },
    delayed_reporting: { name: 'time', color: '#d97706', bg: '#fef3c7' },
    zero_votes: { name: 'alert-circle', color: '#7c3aed', bg: '#ede9fe' },
  };

  return (
    <ScrollView
      style={styles.container}
      contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} colors={['#166534']} tintColor="#166534" />}
      showsVerticalScrollIndicator={false}
    >
      <TouchableOpacity
        style={styles.addButton}
        onPress={() => { setShowForm(!showForm); Haptics.selectionAsync(); }}
        activeOpacity={0.8}
      >
        <View style={styles.addButtonIcon}>
          <Ionicons name={showForm ? 'close' : 'add'} size={20} color="#166534" />
        </View>
        <Text style={styles.addButtonText}>{showForm ? 'Cancel' : 'New Alert Rule'}</Text>
      </TouchableOpacity>

      {showForm && (
        <View style={styles.formCard}>
          <Text style={styles.formTitle}>Create Alert Rule</Text>

          <Text style={styles.label}>Alert Type</Text>
          <View style={styles.chipRow}>
            {ALERT_TYPES.map((type) => {
              const isActive = selectedType === type;
              const cfg = alertTypeIcons[type];
              return (
                <TouchableOpacity
                  key={type}
                  style={[styles.typeChip, isActive && { backgroundColor: cfg.bg, borderColor: cfg.color }]}
                  onPress={() => { setSelectedType(type); Haptics.selectionAsync(); }}
                  activeOpacity={0.7}
                >
                  <Ionicons name={cfg.name} size={14} color={isActive ? cfg.color : '#9ca3af'} />
                  <Text style={[styles.chipText, isActive && { color: cfg.color, fontWeight: '600' }]}>
                    {type.replace(/_/g, ' ')}
                  </Text>
                </TouchableOpacity>
              );
            })}
          </View>

          <Text style={styles.label}>Filter by Party</Text>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} style={{ marginBottom: 12 }}>
            <View style={styles.chipRow}>
              <TouchableOpacity
                style={[styles.filterChip, !selectedParty && styles.filterChipActive]}
                onPress={() => { setSelectedParty(''); Haptics.selectionAsync(); }}
              >
                <Text style={[styles.chipText, !selectedParty && styles.chipTextActive]}>All</Text>
              </TouchableOpacity>
              {PARTIES.map((p) => (
                <TouchableOpacity
                  key={p}
                  style={[styles.filterChip, selectedParty === p && styles.filterChipActive]}
                  onPress={() => { setSelectedParty(p); Haptics.selectionAsync(); }}
                >
                  <Text style={[styles.chipText, selectedParty === p && styles.chipTextActive]}>{p}</Text>
                </TouchableOpacity>
              ))}
            </View>
          </ScrollView>

          <Text style={styles.label}>Filter by State</Text>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} style={{ marginBottom: 12 }}>
            <View style={styles.chipRow}>
              <TouchableOpacity
                style={[styles.filterChip, !selectedState && styles.filterChipActive]}
                onPress={() => { setSelectedState(''); Haptics.selectionAsync(); }}
              >
                <Text style={[styles.chipText, !selectedState && styles.chipTextActive]}>All</Text>
              </TouchableOpacity>
              {STATES.map((s) => (
                <TouchableOpacity
                  key={s}
                  style={[styles.filterChip, selectedState === s && styles.filterChipActive]}
                  onPress={() => { setSelectedState(s); Haptics.selectionAsync(); }}
                >
                  <Text style={[styles.chipText, selectedState === s && styles.chipTextActive]}>{s}</Text>
                </TouchableOpacity>
              ))}
            </View>
          </ScrollView>

          {selectedType === 'threshold' && (
            <View style={{ marginBottom: 14 }}>
              <Text style={styles.label}>Threshold Value</Text>
              <TextInput
                style={styles.thresholdInput}
                placeholder="e.g. 1000"
                placeholderTextColor="#9ca3af"
                value={threshold}
                onChangeText={setThreshold}
                keyboardType="numeric"
              />
            </View>
          )}

          <TouchableOpacity
            style={[styles.saveButton, saving && { opacity: 0.6 }]}
            onPress={saveRule}
            disabled={saving}
            activeOpacity={0.8}
          >
            <Ionicons name="checkmark-circle" size={18} color="#fff" />
            <Text style={styles.saveButtonText}>{saving ? 'Saving...' : 'Create Alert'}</Text>
          </TouchableOpacity>
        </View>
      )}

      <View style={styles.section}>
        <Text style={styles.sectionTitle}>Active Alerts ({rules.length})</Text>
        {loading ? <FeedSkeleton /> : rules.length > 0 ? (
          rules.map((rule) => {
            const cfg = alertTypeIcons[rule.alert_type] || alertTypeIcons.threshold;
            return (
              <View key={rule.id} style={styles.ruleCard}>
                <View style={[styles.ruleIcon, { backgroundColor: cfg.bg }]}>
                  <Ionicons name={cfg.name} size={18} color={cfg.color} />
                </View>
                <View style={{ flex: 1 }}>
                  <Text style={styles.ruleType}>{rule.alert_type.replace(/_/g, ' ')}</Text>
                  <View style={styles.ruleFilters}>
                    {rule.party ? (
                      <View style={styles.ruleBadge}>
                        <Text style={styles.ruleBadgeText}>{rule.party}</Text>
                      </View>
                    ) : null}
                    {rule.state ? (
                      <View style={styles.ruleBadge}>
                        <Text style={styles.ruleBadgeText}>{rule.state}</Text>
                      </View>
                    ) : null}
                    {rule.threshold ? (
                      <View style={styles.ruleBadge}>
                        <Text style={styles.ruleBadgeText}>≥{rule.threshold}</Text>
                      </View>
                    ) : null}
                  </View>
                </View>
                <TouchableOpacity
                  onPress={() => deleteRule(rule.id)}
                  style={styles.deleteButton}
                  activeOpacity={0.6}
                >
                  <Ionicons name="trash-outline" size={18} color="#dc2626" />
                </TouchableOpacity>
              </View>
            );
          })
        ) : (
          <EmptyState
            icon="notifications-off-outline"
            title="No alert rules"
            description="Create alert rules to get notified about election anomalies, thresholds, and delayed reporting"
          />
        )}
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  addButton: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    margin: 12,
    padding: 14,
    backgroundColor: '#fff',
    borderRadius: 14,
    borderWidth: 1.5,
    borderColor: '#dcfce7',
    borderStyle: 'dashed',
  },
  addButtonIcon: {
    width: 36,
    height: 36,
    borderRadius: 12,
    backgroundColor: '#f0fdf4',
    alignItems: 'center',
    justifyContent: 'center',
  },
  addButtonText: { fontSize: 15, fontWeight: '600', color: '#166534' },
  formCard: {
    backgroundColor: '#fff',
    marginHorizontal: 12,
    marginBottom: 12,
    padding: 16,
    borderRadius: 16,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.06,
    shadowRadius: 8,
    elevation: 2,
  },
  formTitle: { fontSize: 18, fontWeight: '700', color: '#111827', marginBottom: 16 },
  label: { fontSize: 13, fontWeight: '600', color: '#374151', marginBottom: 8 },
  chipRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginBottom: 12 },
  typeChip: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    paddingHorizontal: 12,
    paddingVertical: 8,
    borderRadius: 10,
    borderWidth: 1,
    borderColor: '#e5e7eb',
    backgroundColor: '#f9fafb',
  },
  filterChip: {
    paddingHorizontal: 14,
    paddingVertical: 8,
    borderRadius: 20,
    backgroundColor: '#f9fafb',
    borderWidth: 1,
    borderColor: '#e5e7eb',
  },
  filterChipActive: { backgroundColor: '#166534', borderColor: '#166534' },
  chipText: { fontSize: 13, color: '#6b7280', textTransform: 'capitalize' },
  chipTextActive: { color: '#fff', fontWeight: '600' },
  thresholdInput: {
    borderWidth: 1,
    borderColor: '#e5e7eb',
    borderRadius: 12,
    backgroundColor: '#f9fafb',
    padding: 12,
    fontSize: 16,
    color: '#111827',
  },
  saveButton: {
    backgroundColor: '#166534',
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    paddingVertical: 14,
    borderRadius: 12,
    shadowColor: '#166534',
    shadowOffset: { width: 0, height: 3 },
    shadowOpacity: 0.2,
    shadowRadius: 6,
    elevation: 3,
  },
  saveButtonText: { color: '#fff', fontSize: 16, fontWeight: '700' },
  section: { paddingHorizontal: 12 },
  sectionTitle: { fontSize: 16, fontWeight: '700', color: '#111827', marginBottom: 10 },
  ruleCard: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    backgroundColor: '#fff',
    padding: 14,
    borderRadius: 14,
    marginBottom: 8,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.04,
    shadowRadius: 3,
    elevation: 1,
  },
  ruleIcon: {
    width: 40,
    height: 40,
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
  },
  ruleType: { fontSize: 14, fontWeight: '600', color: '#111827', textTransform: 'capitalize' },
  ruleFilters: { flexDirection: 'row', gap: 6, marginTop: 4 },
  ruleBadge: {
    backgroundColor: '#f3f4f6',
    paddingHorizontal: 8,
    paddingVertical: 2,
    borderRadius: 6,
  },
  ruleBadgeText: { fontSize: 11, fontWeight: '500', color: '#6b7280' },
  deleteButton: {
    width: 36,
    height: 36,
    borderRadius: 12,
    backgroundColor: '#fef2f2',
    alignItems: 'center',
    justifyContent: 'center',
  },
});
