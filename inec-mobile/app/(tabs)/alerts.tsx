import { useState, useCallback } from 'react';
import {
  View, Text, TextInput, TouchableOpacity, StyleSheet,
  FlatList, Alert, ActivityIndicator,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { useFocusEffect } from 'expo-router';
import { observerApi, AlertRule } from '../../src/lib/api';

const ALERT_TYPES = [
  { value: 'result_submitted', label: 'Result Submitted' },
  { value: 'anomaly_detected', label: 'Anomaly Detected' },
  { value: 'geofence_violation', label: 'Geofence Violation' },
];

export default function AlertsScreen() {
  const [rules, setRules] = useState<AlertRule[]>([]);
  const [partyCode, setPartyCode] = useState('');
  const [stateCode, setStateCode] = useState('');
  const [lgaCode, setLgaCode] = useState('');
  const [alertType, setAlertType] = useState('result_submitted');
  const [creating, setCreating] = useState(false);
  const [loading, setLoading] = useState(true);

  const loadRules = useCallback(async () => {
    try {
      const data = await observerApi.alerts();
      setRules(data);
    } catch { /* ignore */ }
    setLoading(false);
  }, []);

  useFocusEffect(useCallback(() => { loadRules(); }, [loadRules]));

  const createRule = async () => {
    if (!partyCode.trim() && !stateCode.trim()) {
      Alert.alert('Required', 'Enter at least a Party Code or State Code');
      return;
    }
    setCreating(true);
    try {
      await observerApi.createAlert({
        party_code: partyCode,
        state_code: stateCode,
        lga_code: lgaCode,
        alert_type: alertType,
      });
      setPartyCode('');
      setStateCode('');
      setLgaCode('');
      loadRules();
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : 'Failed to create alert';
      Alert.alert('Error', msg);
    } finally {
      setCreating(false);
    }
  };

  const deleteRule = async (id: number) => {
    Alert.alert('Delete Alert', 'Remove this alert rule?', [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Delete', style: 'destructive',
        onPress: async () => {
          try {
            await observerApi.deleteAlert(id);
            loadRules();
          } catch { /* ignore */ }
        },
      },
    ]);
  };

  const renderRule = ({ item }: { item: AlertRule }) => (
    <View style={styles.ruleCard}>
      <View style={styles.ruleInfo}>
        <View style={styles.ruleHeader}>
          <Ionicons name="notifications" size={16} color="#166534" />
          <Text style={styles.ruleType}>
            {ALERT_TYPES.find((t) => t.value === item.alert_type)?.label || item.alert_type}
          </Text>
        </View>
        <View style={styles.ruleFilters}>
          {item.party_code ? (
            <View style={styles.filterBadge}>
              <Text style={styles.filterText}>Party: {item.party_code}</Text>
            </View>
          ) : null}
          {item.state_code ? (
            <View style={styles.filterBadge}>
              <Text style={styles.filterText}>State: {item.state_code}</Text>
            </View>
          ) : null}
          {item.lga_code ? (
            <View style={styles.filterBadge}>
              <Text style={styles.filterText}>LGA: {item.lga_code}</Text>
            </View>
          ) : null}
        </View>
        <Text style={styles.ruleDate}>{new Date(item.created_at).toLocaleDateString()}</Text>
      </View>
      <TouchableOpacity style={styles.deleteButton} onPress={() => deleteRule(item.id)}>
        <Ionicons name="trash-outline" size={20} color="#ef4444" />
      </TouchableOpacity>
    </View>
  );

  return (
    <View style={styles.container}>
      {/* Create Alert Form */}
      <View style={styles.form}>
        <Text style={styles.formTitle}>Create Alert Rule</Text>
        <View style={styles.formRow}>
          <TextInput
            style={[styles.input, { flex: 1 }]}
            placeholder="Party (e.g. APC)"
            value={partyCode}
            onChangeText={setPartyCode}
            autoCapitalize="characters"
          />
          <TextInput
            style={[styles.input, { flex: 1 }]}
            placeholder="State Code"
            value={stateCode}
            onChangeText={setStateCode}
            autoCapitalize="characters"
          />
          <TextInput
            style={[styles.input, { flex: 1 }]}
            placeholder="LGA Code"
            value={lgaCode}
            onChangeText={setLgaCode}
          />
        </View>

        <View style={styles.pickerContainer}>
          <Text style={styles.pickerLabel}>Alert Type:</Text>
          <View style={styles.pickerWrapper}>
            {ALERT_TYPES.map((type) => (
              <TouchableOpacity
                key={type.value}
                style={[styles.typeChip, alertType === type.value && styles.typeChipActive]}
                onPress={() => setAlertType(type.value)}
              >
                <Text style={[styles.typeChipText, alertType === type.value && styles.typeChipTextActive]}>
                  {type.label}
                </Text>
              </TouchableOpacity>
            ))}
          </View>
        </View>

        <TouchableOpacity
          style={[styles.createButton, creating && styles.disabledButton]}
          onPress={createRule}
          disabled={creating}
        >
          {creating ? (
            <ActivityIndicator color="#fff" size="small" />
          ) : (
            <>
              <Ionicons name="add-circle-outline" size={18} color="#fff" />
              <Text style={styles.createText}>Create Alert</Text>
            </>
          )}
        </TouchableOpacity>
      </View>

      {/* Rules List */}
      {loading ? (
        <ActivityIndicator style={{ marginTop: 40 }} color="#166534" />
      ) : (
        <FlatList
          data={rules}
          keyExtractor={(item) => String(item.id)}
          renderItem={renderRule}
          ListEmptyComponent={
            <View style={styles.emptyContainer}>
              <Ionicons name="notifications-off-outline" size={48} color="#d1d5db" />
              <Text style={styles.emptyText}>No alert rules configured</Text>
              <Text style={styles.emptySubtext}>Create a rule to get notified when results arrive</Text>
            </View>
          }
          contentContainerStyle={rules.length === 0 ? { flex: 1, justifyContent: 'center' } : { paddingBottom: 20 }}
        />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  form: { backgroundColor: '#fff', padding: 16, borderBottomWidth: 1, borderBottomColor: '#e5e7eb' },
  formTitle: { fontSize: 16, fontWeight: '600', color: '#1f2937', marginBottom: 12 },
  formRow: { flexDirection: 'row', gap: 8 },
  input: {
    borderWidth: 1, borderColor: '#d1d5db', borderRadius: 8,
    padding: 10, fontSize: 13, backgroundColor: '#f9fafb', marginBottom: 8,
  },
  pickerContainer: { marginBottom: 12 },
  pickerLabel: { fontSize: 13, color: '#6b7280', marginBottom: 6 },
  pickerWrapper: { flexDirection: 'row', gap: 6, flexWrap: 'wrap' },
  typeChip: {
    paddingHorizontal: 12, paddingVertical: 8, borderRadius: 16,
    backgroundColor: '#f3f4f6', borderWidth: 1, borderColor: '#e5e7eb',
  },
  typeChipActive: { backgroundColor: '#dcfce7', borderColor: '#166534' },
  typeChipText: { fontSize: 12, color: '#6b7280' },
  typeChipTextActive: { color: '#166534', fontWeight: '600' },
  createButton: {
    backgroundColor: '#166534', padding: 14, borderRadius: 8,
    flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 6,
  },
  disabledButton: { opacity: 0.6 },
  createText: { color: '#fff', fontWeight: '600', fontSize: 15 },
  ruleCard: {
    flexDirection: 'row', alignItems: 'center', backgroundColor: '#fff',
    marginHorizontal: 12, marginVertical: 4, padding: 14, borderRadius: 8,
    borderWidth: 1, borderColor: '#e5e7eb',
  },
  ruleInfo: { flex: 1 },
  ruleHeader: { flexDirection: 'row', alignItems: 'center', gap: 6, marginBottom: 6 },
  ruleType: { fontSize: 14, fontWeight: '600', color: '#1f2937' },
  ruleFilters: { flexDirection: 'row', gap: 6, flexWrap: 'wrap' },
  filterBadge: { backgroundColor: '#eff6ff', paddingHorizontal: 8, paddingVertical: 3, borderRadius: 10 },
  filterText: { fontSize: 11, color: '#1e40af' },
  ruleDate: { fontSize: 11, color: '#9ca3af', marginTop: 4 },
  deleteButton: { padding: 8 },
  emptyContainer: { alignItems: 'center', gap: 8 },
  emptyText: { fontSize: 16, color: '#6b7280', fontWeight: '500' },
  emptySubtext: { fontSize: 13, color: '#9ca3af' },
});
