import { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api as apiCall } from '../src/lib/api';

interface ValidationResult {
  total_results: number;
  valid: number;
  warnings: number;
  errors: number;
  checks: { name: string; status: string; details: string }[];
}

export default function DataValidationScreen() {
  const [result, setResult] = useState<ValidationResult | null>(null);
  const [loading, setLoading] = useState(false);

  const runValidation = async () => {
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      const res = await apiCall<ValidationResult>('/validation/run', { method: 'POST' });
      setResult(res);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Validation failed');
    }
    setLoading(false);
  };

  const statusColor = (s: string) => {
    if (s === 'pass') return '#166534';
    if (s === 'warning') return '#f59e0b';
    return '#dc2626';
  };

  const statusIcon = (s: string): 'checkmark-circle' | 'alert-circle' | 'close-circle' => {
    if (s === 'pass') return 'checkmark-circle';
    if (s === 'warning') return 'alert-circle';
    return 'close-circle';
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: 40 }}>
      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="checkmark-done-outline" size={24} color="#166534" />
          <Text style={styles.cardTitle}>Data Validation</Text>
        </View>
        <Text style={styles.muted}>Run integrity checks on election data: vote totals, accreditation counts, duplicate detection.</Text>
        <TouchableOpacity style={styles.button} onPress={runValidation} disabled={loading} activeOpacity={0.8}>
          <Text style={styles.buttonText}>{loading ? 'Validating...' : 'Run Validation Suite'}</Text>
        </TouchableOpacity>
      </View>

      {result && (
        <>
          <View style={styles.statsGrid}>
            <View style={[styles.statCard, { backgroundColor: '#dcfce7' }]}>
              <Text style={[styles.statNumber, { color: '#166534' }]}>{result.valid}</Text>
              <Text style={styles.statLabel}>Valid</Text>
            </View>
            <View style={[styles.statCard, { backgroundColor: '#fef9c3' }]}>
              <Text style={[styles.statNumber, { color: '#ca8a04' }]}>{result.warnings}</Text>
              <Text style={styles.statLabel}>Warnings</Text>
            </View>
            <View style={[styles.statCard, { backgroundColor: '#fef2f2' }]}>
              <Text style={[styles.statNumber, { color: '#dc2626' }]}>{result.errors}</Text>
              <Text style={styles.statLabel}>Errors</Text>
            </View>
          </View>

          <View style={styles.card}>
            <Text style={styles.cardTitle}>Check Results</Text>
            {result.checks.map((check, i) => (
              <View key={i} style={styles.checkRow}>
                <Ionicons name={statusIcon(check.status)} size={20} color={statusColor(check.status)} />
                <View style={{ flex: 1, marginLeft: 10 }}>
                  <Text style={{ fontSize: 14, fontWeight: '600', color: '#111827' }}>{check.name}</Text>
                  <Text style={styles.muted}>{check.details}</Text>
                </View>
              </View>
            ))}
          </View>
        </>
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
  button: { backgroundColor: '#166534', borderRadius: 12, padding: 14, alignItems: 'center', marginTop: 8 },
  buttonText: { color: '#fff', fontSize: 15, fontWeight: '600' },
  statsGrid: { flexDirection: 'row', gap: 8, marginBottom: 16 },
  statCard: { flex: 1, borderRadius: 10, padding: 12, alignItems: 'center' },
  statNumber: { fontSize: 24, fontWeight: '700' },
  statLabel: { fontSize: 11, color: '#6b7280', marginTop: 2 },
  checkRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 10, borderBottomWidth: 1, borderBottomColor: '#f3f4f6' },
});
