import { useState, useEffect, useCallback } from 'react';
import { View, Text, ScrollView, TouchableOpacity, ActivityIndicator, RefreshControl } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { api } from '../src/lib/api';

interface ComplianceData {
  standard: string;
  compliance_framework: string;
  assessment_criteria: string[];
  election_overview: { total_polling_units: number; units_reporting: number; coverage_pct: number; total_votes_cast: number };
  security_assessment: { total_incidents: number; open_disputes: number; unresolved_anomalies: number; security_level: string };
  observer_coverage: { total_observers: number; coverage_ratio: number };
  recommendations: string[];
}

export default function ComplianceScreen() {
  const [data, setData] = useState<ComplianceData | null>(null);
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [standard, setStandard] = useState('ecowas');

  const load = useCallback(async () => {
    try {
      const res = await api<ComplianceData>(`/reports/compliance?standard=${standard}&election_id=1`);
      setData(res);
    } catch { setData(null); }
  }, [standard]);

  useEffect(() => { setLoading(true); load().finally(() => setLoading(false)); }, [load]);

  const onRefresh = async () => { setRefreshing(true); await load(); setRefreshing(false); Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success); };

  const secColor = (level: string) => {
    if (level === 'excellent') return '#16a34a';
    if (level === 'good') return '#2563eb';
    if (level === 'fair') return '#d97706';
    return '#dc2626';
  };

  return (
    <ScrollView style={{ flex: 1, backgroundColor: '#f8fafc' }} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} />}>
      <View style={{ padding: 16 }}>
        <Text style={{ fontSize: 24, fontWeight: '700', color: '#0f172a', marginBottom: 16 }} accessibilityRole="header">Compliance Report</Text>

        <ScrollView horizontal showsHorizontalScrollIndicator={false} style={{ marginBottom: 16 }}>
          {['ecowas', 'au', 'eu'].map(s => (
            <TouchableOpacity key={s} onPress={() => { setStandard(s); Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light); }}
              style={{ backgroundColor: standard === s ? '#2563eb' : '#e2e8f0', paddingHorizontal: 20, paddingVertical: 10, borderRadius: 20, marginRight: 8 }}
              accessibilityRole="tab" accessibilityState={{ selected: standard === s }}>
              <Text style={{ color: standard === s ? '#fff' : '#334155', fontWeight: '600', textTransform: 'uppercase' }}>{s}</Text>
            </TouchableOpacity>
          ))}
        </ScrollView>

        {loading && <ActivityIndicator size="large" style={{ marginTop: 40 }} />}

        {data && !loading && (
          <>
            <View style={{ backgroundColor: '#eff6ff', borderRadius: 12, padding: 14, marginBottom: 16, borderLeftWidth: 4, borderLeftColor: '#2563eb' }}>
              <Text style={{ fontSize: 11, color: '#2563eb', fontWeight: '600' }}>FRAMEWORK</Text>
              <Text style={{ fontSize: 14, color: '#0f172a', marginTop: 4, fontWeight: '500' }}>{data.compliance_framework}</Text>
            </View>

            <Text style={{ fontSize: 16, fontWeight: '700', color: '#0f172a', marginBottom: 10 }}>Election Overview</Text>
            <ScrollView horizontal showsHorizontalScrollIndicator={false} style={{ marginBottom: 16 }}>
              {[
                { label: 'Total PUs', value: data.election_overview.total_polling_units.toLocaleString(), color: '#2563eb' },
                { label: 'Reporting', value: data.election_overview.units_reporting.toLocaleString(), color: '#16a34a' },
                { label: 'Coverage', value: `${data.election_overview.coverage_pct.toFixed(1)}%`, color: '#d97706' },
                { label: 'Total Votes', value: data.election_overview.total_votes_cast.toLocaleString(), color: '#7c3aed' },
              ].map(s => (
                <View key={s.label} style={{ backgroundColor: '#fff', borderRadius: 10, padding: 12, marginRight: 8, minWidth: 90, alignItems: 'center', shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 }}>
                  <Text style={{ fontSize: 18, fontWeight: '700', color: s.color }}>{s.value}</Text>
                  <Text style={{ fontSize: 11, color: '#64748b', marginTop: 2 }}>{s.label}</Text>
                </View>
              ))}
            </ScrollView>

            <Text style={{ fontSize: 16, fontWeight: '700', color: '#0f172a', marginBottom: 10 }}>Security Assessment</Text>
            <View style={{ backgroundColor: '#fff', borderRadius: 12, padding: 14, marginBottom: 16 }}>
              <View style={{ flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
                <Text style={{ color: '#64748b' }}>Security Level</Text>
                <View style={{ backgroundColor: secColor(data.security_assessment.security_level) + '20', paddingHorizontal: 12, paddingVertical: 4, borderRadius: 12 }}>
                  <Text style={{ color: secColor(data.security_assessment.security_level), fontWeight: '700', textTransform: 'capitalize' }}>{data.security_assessment.security_level}</Text>
                </View>
              </View>
              <View style={{ flexDirection: 'row', justifyContent: 'space-around' }}>
                {[
                  { label: 'Incidents', value: data.security_assessment.total_incidents, icon: 'warning-outline' as const },
                  { label: 'Disputes', value: data.security_assessment.open_disputes, icon: 'document-text-outline' as const },
                  { label: 'Anomalies', value: data.security_assessment.unresolved_anomalies, icon: 'alert-circle-outline' as const },
                ].map(s => (
                  <View key={s.label} style={{ alignItems: 'center' }}>
                    <Ionicons name={s.icon} size={20} color="#64748b" />
                    <Text style={{ fontSize: 18, fontWeight: '700', color: '#0f172a', marginTop: 4 }}>{s.value}</Text>
                    <Text style={{ fontSize: 10, color: '#64748b' }}>{s.label}</Text>
                  </View>
                ))}
              </View>
            </View>

            <Text style={{ fontSize: 16, fontWeight: '700', color: '#0f172a', marginBottom: 10 }}>Recommendations</Text>
            {data.recommendations?.map((rec, i) => (
              <View key={i} style={{ backgroundColor: '#fffbeb', borderRadius: 10, padding: 12, marginBottom: 8, flexDirection: 'row', alignItems: 'flex-start', gap: 8 }}>
                <Ionicons name="bulb-outline" size={16} color="#d97706" style={{ marginTop: 2 }} />
                <Text style={{ flex: 1, fontSize: 13, color: '#92400e' }}>{rec}</Text>
              </View>
            ))}

            <Text style={{ fontSize: 16, fontWeight: '700', color: '#0f172a', marginTop: 16, marginBottom: 10 }}>Assessment Criteria</Text>
            {data.assessment_criteria?.map((c, i) => (
              <View key={i} style={{ flexDirection: 'row', alignItems: 'center', gap: 10, marginBottom: 8 }}>
                <View style={{ width: 24, height: 24, borderRadius: 12, backgroundColor: '#f0fdf4', alignItems: 'center', justifyContent: 'center' }}>
                  <Text style={{ fontSize: 11, fontWeight: '700', color: '#16a34a' }}>{i + 1}</Text>
                </View>
                <Text style={{ flex: 1, fontSize: 13, color: '#334155' }}>{c}</Text>
              </View>
            ))}
          </>
        )}
      </View>
    </ScrollView>
  );
}
