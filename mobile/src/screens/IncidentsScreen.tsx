import React, { useEffect, useState } from 'react';
import { View, Text, FlatList, StyleSheet, RefreshControl, TouchableOpacity, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { apiGet, apiPost } from '../lib/api';

interface Incident {
  id: number;
  type: string;
  severity: string;
  description: string;
  polling_unit_code: string;
  state_code: string;
  reported_by: string;
  status: string;
  created_at: string;
}

const severityColors: Record<string, string> = {
  critical: '#dc2626', high: '#ea580c', medium: '#ca8a04', low: '#16a34a',
};

export default function IncidentsScreen() {
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    try {
      const data = await apiGet('/incidents?election_id=1');
      setIncidents(data.incidents || []);
    } catch {}
  };

  useEffect(() => { load(); }, []);

  const reportIncident = () => {
    Alert.prompt('Report Incident', 'Describe the incident:', async (text) => {
      if (text) {
        try {
          await apiPost('/incidents', { election_id: 1, type: 'field_report', severity: 'medium', description: text });
          Alert.alert('Submitted', 'Incident reported successfully');
          load();
        } catch { Alert.alert('Error', 'Failed to submit (queued for later)'); }
      }
    });
  };

  return (
    <View style={s.container}>
      <TouchableOpacity style={s.reportBtn} onPress={reportIncident}>
        <Ionicons name="add-circle" size={20} color="#fff" />
        <Text style={s.reportBtnText}>Report Incident</Text>
      </TouchableOpacity>

      <FlatList
        data={incidents}
        keyExtractor={(item) => item.id.toString()}
        renderItem={({ item }) => (
          <View style={s.card}>
            <View style={s.row}>
              <View style={[s.dot, { backgroundColor: severityColors[item.severity] || '#94a3b8' }]} />
              <Text style={s.type}>{item.type?.replace(/_/g, ' ')}</Text>
              <Text style={[s.severity, { color: severityColors[item.severity] || '#94a3b8' }]}>
                {item.severity}
              </Text>
            </View>
            <Text style={s.desc}>{item.description}</Text>
            <Text style={s.meta}>PU: {item.polling_unit_code} | {item.state_code}</Text>
          </View>
        )}
        contentContainerStyle={s.list}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={async () => { setRefreshing(true); await load(); setRefreshing(false); }} />}
        ListEmptyComponent={<Text style={s.empty}>No incidents reported</Text>}
      />
    </View>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  reportBtn: { flexDirection: 'row', alignItems: 'center', gap: 6, backgroundColor: '#dc2626', margin: 16, marginBottom: 0, padding: 14, borderRadius: 12, justifyContent: 'center' },
  reportBtnText: { color: '#fff', fontWeight: '600', fontSize: 15 },
  list: { padding: 16, gap: 10 },
  card: { backgroundColor: '#fff', borderRadius: 12, padding: 14, elevation: 1 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  dot: { width: 8, height: 8, borderRadius: 4 },
  type: { flex: 1, fontSize: 14, fontWeight: '600', color: '#1e293b', textTransform: 'capitalize' },
  severity: { fontSize: 11, fontWeight: '700', textTransform: 'uppercase' },
  desc: { fontSize: 13, color: '#475569', marginTop: 6 },
  meta: { fontSize: 11, color: '#94a3b8', marginTop: 6 },
  empty: { textAlign: 'center', color: '#94a3b8', marginTop: 48 },
});
