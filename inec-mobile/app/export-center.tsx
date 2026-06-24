import { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api as apiCall } from '../src/lib/api';

interface ExportJob {
  job_id: string;
  format: string;
  status: string;
  created_at: string;
  download_url?: string;
}

export default function ExportCenterScreen() {
  const [jobs, setJobs] = useState<ExportJob[]>([]);
  const [loading, setLoading] = useState(false);

  const triggerExport = async (format: string) => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      const res = await apiCall<ExportJob>('/export/trigger', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ format, election_id: 1 }),
      });
      setJobs(prev => [res, ...prev]);
      Alert.alert('Export Started', `${format.toUpperCase()} export job created.`);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Export failed');
    }
    setLoading(false);
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: 40 }}>
      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="download-outline" size={24} color="#166534" />
          <Text style={styles.cardTitle}>Export Center</Text>
        </View>
        <Text style={styles.muted}>Export election results, audit logs, and analytics data in various formats.</Text>

        <View style={{ gap: 8, marginTop: 8 }}>
          {[
            { format: 'csv', label: 'CSV Export', icon: 'document-text-outline' as const, color: '#166534' },
            { format: 'pdf', label: 'PDF Report', icon: 'document-outline' as const, color: '#dc2626' },
            { format: 'parquet', label: 'Parquet (Analytics)', icon: 'analytics-outline' as const, color: '#7c3aed' },
            { format: 'json', label: 'JSON API Dump', icon: 'code-slash-outline' as const, color: '#2563eb' },
          ].map((exp) => (
            <TouchableOpacity key={exp.format} style={[styles.exportButton, { borderColor: exp.color }]} onPress={() => triggerExport(exp.format)} disabled={loading} activeOpacity={0.8}>
              <Ionicons name={exp.icon} size={20} color={exp.color} />
              <Text style={[styles.exportText, { color: exp.color }]}>{exp.label}</Text>
            </TouchableOpacity>
          ))}
        </View>
      </View>

      {jobs.length > 0 && (
        <View style={styles.card}>
          <Text style={styles.cardTitle}>Recent Exports</Text>
          {jobs.map((job) => (
            <View key={job.job_id} style={styles.jobRow}>
              <View style={{ flex: 1 }}>
                <Text style={{ fontSize: 14, fontWeight: '600', color: '#111827' }}>{job.format.toUpperCase()}</Text>
                <Text style={styles.muted}>{job.status} | {job.job_id.slice(0, 8)}</Text>
              </View>
              <Ionicons name={job.status === 'completed' ? 'checkmark-circle' : 'time-outline'} size={20} color={job.status === 'completed' ? '#166534' : '#f59e0b'} />
            </View>
          ))}
        </View>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb', padding: 16 },
  card: { backgroundColor: '#fff', borderRadius: 16, padding: 16, marginBottom: 16, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  cardHeader: { flexDirection: 'row', alignItems: 'center', gap: 10, marginBottom: 12 },
  cardTitle: { fontSize: 16, fontWeight: '700', color: '#111827' },
  muted: { fontSize: 13, color: '#9ca3af', marginBottom: 8 },
  exportButton: { flexDirection: 'row', alignItems: 'center', gap: 10, padding: 14, borderWidth: 2, borderRadius: 12, backgroundColor: '#fff' },
  exportText: { fontSize: 15, fontWeight: '600' },
  jobRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 10, borderBottomWidth: 1, borderBottomColor: '#f3f4f6' },
});
