import { useState, useEffect } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, TextInput, Platform, ActivityIndicator, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api } from '../src/lib/api';

interface Incident {
  id: number;
  title: string;
  description: string;
  severity: string;
  status: string;
  polling_unit_code: string;
  reported_by: number;
  created_at: string;
}

export default function IncidentsScreen() {
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const [loading, setLoading] = useState(false);
  const [showForm, setShowForm] = useState(false);
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [severity, setSeverity] = useState('medium');
  const [puCode, setPuCode] = useState('');

  const loadIncidents = async () => {
    setLoading(true);
    try {
      const data = await api<{ incidents: Incident[] }>('/incidents');
      setIncidents(data.incidents || []);
    } catch { /* ignore */ }
    setLoading(false);
  };

  useEffect(() => { loadIncidents(); }, []);

  const submitIncident = async () => {
    if (!title || !description) {
      Alert.alert('Error', 'Title and description required');
      return;
    }
    Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
    try {
      await api('/incidents', {
        method: 'POST',
        body: JSON.stringify({ title, description, severity, polling_unit_code: puCode, election_id: 1 }),
      });
      setShowForm(false);
      setTitle(''); setDescription(''); setPuCode('');
      loadIncidents();
    } catch (e) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed to submit');
    }
  };

  const sevColor = (s: string) => s === 'critical' ? '#dc2626' : s === 'high' ? '#f59e0b' : s === 'medium' ? '#3b82f6' : '#64748b';

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}>
      <View style={styles.header}>
        <Ionicons name="alert-circle" size={28} color="#dc2626" />
        <Text style={styles.title}>Incidents</Text>
        <TouchableOpacity style={styles.addBtn} onPress={() => setShowForm(!showForm)}>
          <Ionicons name={showForm ? 'close' : 'add'} size={20} color="#fff" />
        </TouchableOpacity>
      </View>

      {showForm && (
        <View style={styles.formCard}>
          <TextInput style={styles.input} placeholder="Incident title" value={title} onChangeText={setTitle} />
          <TextInput style={[styles.input, { height: 80 }]} placeholder="Description" value={description} onChangeText={setDescription} multiline />
          <TextInput style={styles.input} placeholder="Polling unit code (optional)" value={puCode} onChangeText={setPuCode} />
          <View style={styles.sevRow}>
            {['low', 'medium', 'high', 'critical'].map(s => (
              <TouchableOpacity key={s} style={[styles.sevBtn, severity === s && { backgroundColor: sevColor(s) }]} onPress={() => setSeverity(s)}>
                <Text style={[styles.sevBtnText, severity === s && { color: '#fff' }]}>{s}</Text>
              </TouchableOpacity>
            ))}
          </View>
          <TouchableOpacity style={styles.submitBtn} onPress={submitIncident}>
            <Ionicons name="send" size={16} color="#fff" />
            <Text style={styles.submitText}>Report Incident</Text>
          </TouchableOpacity>
        </View>
      )}

      {loading && <ActivityIndicator size="large" color="#dc2626" style={{ marginTop: 24 }} />}

      {incidents.map(inc => (
        <View key={inc.id} style={[styles.incidentCard, { borderLeftColor: sevColor(inc.severity) }]}>
          <View style={styles.incHeader}>
            <Text style={styles.incTitle}>{inc.title}</Text>
            <View style={[styles.badge, { backgroundColor: sevColor(inc.severity) + '20' }]}>
              <Text style={[styles.badgeText, { color: sevColor(inc.severity) }]}>{inc.severity}</Text>
            </View>
          </View>
          <Text style={styles.incDesc} numberOfLines={2}>{inc.description}</Text>
          {inc.polling_unit_code && <Text style={styles.incPU}>{inc.polling_unit_code}</Text>}
          <View style={styles.incFooter}>
            <Text style={styles.incStatus}>{inc.status}</Text>
            <Text style={styles.incTime}>{new Date(inc.created_at).toLocaleDateString()}</Text>
          </View>
        </View>
      ))}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  header: { flexDirection: 'row', alignItems: 'center', gap: 10, padding: 16, paddingTop: Platform.OS === 'ios' ? 60 : 16 },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b', flex: 1 },
  addBtn: { width: 36, height: 36, borderRadius: 18, backgroundColor: '#dc2626', alignItems: 'center', justifyContent: 'center' },
  formCard: { margin: 16, padding: 16, backgroundColor: '#fff', borderRadius: 12, borderWidth: 1, borderColor: '#e2e8f0', gap: 10 },
  input: { borderWidth: 1, borderColor: '#e2e8f0', borderRadius: 8, padding: 12, fontSize: 14 },
  sevRow: { flexDirection: 'row', gap: 6 },
  sevBtn: { flex: 1, paddingVertical: 8, borderRadius: 8, backgroundColor: '#f1f5f9', alignItems: 'center' },
  sevBtnText: { fontSize: 12, fontWeight: '600', color: '#64748b', textTransform: 'capitalize' },
  submitBtn: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 6, padding: 12, backgroundColor: '#dc2626', borderRadius: 10 },
  submitText: { color: '#fff', fontWeight: '600' },
  incidentCard: { marginHorizontal: 16, marginBottom: 10, padding: 14, backgroundColor: '#fff', borderRadius: 12, borderLeftWidth: 4 },
  incHeader: { flexDirection: 'row', alignItems: 'center', marginBottom: 4 },
  incTitle: { fontSize: 15, fontWeight: '600', color: '#1e293b', flex: 1 },
  badge: { paddingHorizontal: 8, paddingVertical: 2, borderRadius: 6 },
  badgeText: { fontSize: 11, fontWeight: '600', textTransform: 'uppercase' },
  incDesc: { fontSize: 13, color: '#64748b', marginVertical: 4 },
  incPU: { fontSize: 12, color: '#3b82f6', fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace' },
  incFooter: { flexDirection: 'row', justifyContent: 'space-between', marginTop: 6, paddingTop: 6, borderTopWidth: 1, borderTopColor: '#f1f5f9' },
  incStatus: { fontSize: 12, fontWeight: '600', color: '#166534', textTransform: 'capitalize' },
  incTime: { fontSize: 12, color: '#94a3b8' },
});
