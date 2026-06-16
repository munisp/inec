import React, { useState } from 'react';
import { View, Text, StyleSheet, TouchableOpacity, Alert, ScrollView } from 'react-native';
import { API_URL as API } from '../src/lib/api';

const EXPORT_TYPES = [
  { key: 'results', label: 'Election Results', icon: '📊', desc: 'CSV/JSON export of all collation results' },
  { key: 'voters', label: 'Voter Registry', icon: '👥', desc: 'Anonymized voter registration data' },
  { key: 'incidents', label: 'Incident Reports', icon: '⚠️', desc: 'All reported incidents with resolution status' },
  { key: 'audit', label: 'Audit Trail', icon: '🔗', desc: 'Complete blockchain-verified audit log' },
  { key: 'analytics', label: 'Analytics Data', icon: '📈', desc: 'Turnout, anomaly scores, predictions' },
  { key: 'bvas', label: 'BVAS Device Logs', icon: '📱', desc: 'Device sync history and diagnostics' },
];

export default function ExportCenterScreen() {
  const [exporting, setExporting] = useState<string | null>(null);

  const handleExport = async (type: string) => {
    setExporting(type);
    try {
      const res = await fetch(`${API}/export/${type}?format=csv`, { method: 'POST' });
      if (res.ok) Alert.alert('Export Started', `${type} export is being prepared. You'll be notified when ready.`);
      else Alert.alert('Error', 'Export failed — please try again');
    } catch (e) { Alert.alert('Error', 'Network error'); }
    setExporting(null);
  };

  return (
    <ScrollView style={s.container}>
      <Text style={s.title}>Export Center</Text>
      <Text style={s.subtitle}>Download election data for analysis and reporting</Text>
      {EXPORT_TYPES.map(t => (
        <TouchableOpacity key={t.key} style={s.card} onPress={() => handleExport(t.key)} disabled={exporting === t.key}>
          <View style={s.row}>
            <Text style={s.icon}>{t.icon}</Text>
            <View style={s.info}>
              <Text style={s.label}>{t.label}</Text>
              <Text style={s.desc}>{t.desc}</Text>
            </View>
          </View>
          <Text style={s.btn}>{exporting === t.key ? 'Exporting...' : 'Export'}</Text>
        </TouchableOpacity>
      ))}
    </ScrollView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc', padding: 16 },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b', marginBottom: 4 },
  subtitle: { fontSize: 14, color: '#64748b', marginBottom: 16 },
  card: { backgroundColor: '#fff', borderRadius: 10, padding: 16, marginBottom: 10, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  row: { flexDirection: 'row', alignItems: 'center', marginBottom: 10 },
  icon: { fontSize: 28, marginRight: 12 },
  info: { flex: 1 },
  label: { fontSize: 15, fontWeight: '600', color: '#1e293b' },
  desc: { fontSize: 12, color: '#64748b', marginTop: 2 },
  btn: { backgroundColor: '#16a34a', color: '#fff', textAlign: 'center', paddingVertical: 8, borderRadius: 8, fontSize: 14, fontWeight: '600', overflow: 'hidden' },
});
