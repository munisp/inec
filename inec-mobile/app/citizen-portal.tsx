import React, { useState } from 'react';
import { View, Text, ScrollView, StyleSheet, TextInput, TouchableOpacity, Alert } from 'react-native';
import { API_URL as API } from '../src/lib/api';

export default function CitizenPortalScreen() {
  const [vin, setVin] = useState('');
  const [result, setResult] = useState<any>(null);
  const [loading, setLoading] = useState(false);

  const lookup = async () => {
    if (!vin.trim()) { Alert.alert('Error', 'Enter your VIN'); return; }
    setLoading(true);
    try {
      const res = await fetch(`${API}/citizen/verify?vin=${encodeURIComponent(vin)}`);
      if (res.ok) setResult(await res.json());
      else Alert.alert('Not Found', 'VIN not found in the registry');
    } catch (e) { Alert.alert('Error', 'Network error'); }
    setLoading(false);
  };

  return (
    <ScrollView style={s.container}>
      <Text style={s.title}>Citizen Portal</Text>
      <Text style={s.subtitle}>Verify your voter registration and find your polling unit</Text>

      <View style={s.card}>
        <Text style={s.label}>Voter Identification Number (VIN)</Text>
        <TextInput style={s.input} placeholder="Enter your VIN" value={vin} onChangeText={setVin} autoCapitalize="characters" />
        <TouchableOpacity style={s.btn} onPress={lookup} disabled={loading}>
          <Text style={s.btnText}>{loading ? 'Checking...' : 'Verify Registration'}</Text>
        </TouchableOpacity>
      </View>

      {result && (
        <View style={s.card}>
          <Text style={s.cardTitle}>Registration Status</Text>
          <View style={[s.statusBadge, { backgroundColor: result.is_registered ? '#dcfce7' : '#fee2e2' }]}>
            <Text style={{ color: result.is_registered ? '#16a34a' : '#dc2626', fontWeight: '600' }}>
              {result.is_registered ? 'Registered' : 'Not Registered'}
            </Text>
          </View>
          {result.polling_unit && <Text style={s.detail}>Polling Unit: {result.polling_unit}</Text>}
          {result.state && <Text style={s.detail}>State: {result.state}</Text>}
          {result.lga && <Text style={s.detail}>LGA: {result.lga}</Text>}
          {result.ward && <Text style={s.detail}>Ward: {result.ward}</Text>}
        </View>
      )}

      <View style={s.card}>
        <Text style={s.cardTitle}>Quick Links</Text>
        <Text style={s.link}>• Check election results in real-time</Text>
        <Text style={s.link}>• Report an incident at your polling unit</Text>
        <Text style={s.link}>• Find your nearest polling unit on the map</Text>
        <Text style={s.link}>• View candidate profiles</Text>
      </View>
    </ScrollView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc', padding: 16 },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b', marginBottom: 4 },
  subtitle: { fontSize: 14, color: '#64748b', marginBottom: 16 },
  card: { backgroundColor: '#fff', borderRadius: 10, padding: 16, marginBottom: 12, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  cardTitle: { fontSize: 16, fontWeight: '600', color: '#1e293b', marginBottom: 8 },
  label: { fontSize: 13, fontWeight: '600', color: '#475569', marginBottom: 6 },
  input: { borderWidth: 1, borderColor: '#e2e8f0', borderRadius: 8, padding: 12, fontSize: 15, marginBottom: 12 },
  btn: { backgroundColor: '#16a34a', paddingVertical: 12, borderRadius: 8, alignItems: 'center' },
  btnText: { color: '#fff', fontSize: 15, fontWeight: '600' },
  statusBadge: { alignSelf: 'flex-start', paddingHorizontal: 12, paddingVertical: 6, borderRadius: 8, marginBottom: 8 },
  detail: { fontSize: 14, color: '#475569', marginTop: 4 },
  link: { fontSize: 14, color: '#3b82f6', marginVertical: 3 },
});
