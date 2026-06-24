import { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, TouchableOpacity, TextInput, Alert } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api as apiCall } from '../src/lib/api';

interface SMSResponse {
  response: string;
  phone: string;
  channel: string;
}

interface SMSStats {
  total_sms: number;
  total_ussd: number;
  today: number;
  by_type: { type: string; count: number }[];
}

export default function SMSVerificationScreen() {
  const [phone, setPhone] = useState('');
  const [message, setMessage] = useState('');
  const [response, setResponse] = useState<string | null>(null);
  const [stats, setStats] = useState<SMSStats | null>(null);
  const [loading, setLoading] = useState(false);

  const sendSMS = async () => {
    if (!phone.trim()) return;
    setLoading(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      const res = await apiCall<SMSResponse>('/sms/verify', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ phone, message: message || 'STATUS' }),
      });
      setResponse(res.response);
    } catch (e: unknown) {
      Alert.alert('Error', e instanceof Error ? e.message : 'SMS request failed');
    }
    setLoading(false);
  };

  const loadStats = async () => {
    try {
      const res = await apiCall<SMSStats>('/sms/stats');
      setStats(res);
    } catch {
      Alert.alert('Error', 'Failed to load SMS stats');
    }
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: 40 }}>
      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="chatbubble-outline" size={24} color="#166534" />
          <Text style={styles.cardTitle}>SMS Result Verification</Text>
        </View>
        <Text style={styles.muted}>Send RESULT {'<PU-CODE>'}, VERIFY {'<PU-CODE>'}, or STATUS to check election results via SMS.</Text>

        <TextInput
          style={styles.input}
          placeholder="Phone number (+234...)"
          value={phone}
          onChangeText={setPhone}
          keyboardType="phone-pad"
        />
        <TextInput
          style={[styles.input, { marginTop: 8 }]}
          placeholder="Message (e.g. RESULT AB-001-W001-PU001)"
          value={message}
          onChangeText={setMessage}
          autoCapitalize="characters"
        />
        <TouchableOpacity style={styles.button} onPress={sendSMS} disabled={loading} activeOpacity={0.8}>
          <Text style={styles.buttonText}>{loading ? 'Sending...' : 'Send SMS Query'}</Text>
        </TouchableOpacity>

        {response && (
          <View style={styles.responseBox}>
            <Text style={styles.responseText}>{response}</Text>
          </View>
        )}
      </View>

      <View style={styles.card}>
        <View style={styles.cardHeader}>
          <Ionicons name="bar-chart-outline" size={24} color="#2563eb" />
          <Text style={styles.cardTitle}>SMS/USSD Stats</Text>
        </View>
        <TouchableOpacity style={[styles.button, { backgroundColor: '#2563eb' }]} onPress={loadStats} activeOpacity={0.8}>
          <Text style={styles.buttonText}>Load Stats</Text>
        </TouchableOpacity>
        {stats && (
          <View style={styles.statsGrid}>
            <View style={styles.statCard}>
              <Text style={styles.statNumber}>{stats.total_sms}</Text>
              <Text style={styles.statLabel}>SMS Queries</Text>
            </View>
            <View style={styles.statCard}>
              <Text style={styles.statNumber}>{stats.total_ussd}</Text>
              <Text style={styles.statLabel}>USSD Sessions</Text>
            </View>
            <View style={styles.statCard}>
              <Text style={[styles.statNumber, { color: '#166534' }]}>{stats.today}</Text>
              <Text style={styles.statLabel}>Today</Text>
            </View>
          </View>
        )}
      </View>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb', padding: 16 },
  card: { backgroundColor: '#fff', borderRadius: 16, padding: 16, marginBottom: 16, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  cardHeader: { flexDirection: 'row', alignItems: 'center', gap: 10, marginBottom: 12 },
  cardTitle: { fontSize: 16, fontWeight: '700', color: '#111827' },
  muted: { fontSize: 13, color: '#9ca3af', marginBottom: 8 },
  input: { borderWidth: 1, borderColor: '#d1d5db', borderRadius: 12, padding: 14, fontSize: 15, backgroundColor: '#f9fafb' },
  button: { backgroundColor: '#166534', borderRadius: 12, padding: 14, alignItems: 'center', marginTop: 8 },
  buttonText: { color: '#fff', fontSize: 15, fontWeight: '600' },
  responseBox: { backgroundColor: '#f0fdf4', borderRadius: 12, padding: 14, marginTop: 12, borderWidth: 1, borderColor: '#bbf7d0' },
  responseText: { fontSize: 13, fontFamily: 'monospace', color: '#166534', lineHeight: 20 },
  statsGrid: { flexDirection: 'row', gap: 8, marginTop: 12 },
  statCard: { flex: 1, backgroundColor: '#f9fafb', borderRadius: 10, padding: 12, alignItems: 'center' },
  statNumber: { fontSize: 20, fontWeight: '700', color: '#111827' },
  statLabel: { fontSize: 11, color: '#6b7280', marginTop: 2 },
});
