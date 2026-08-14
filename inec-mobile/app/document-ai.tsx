import React, { useState } from 'react';
import { View, Text, StyleSheet, TextInput, TouchableOpacity, ScrollView } from 'react-native';
import { api } from '../src/lib/api';

// R4-52: GET /document-ai/analyses never existed. Real routes are
// POST /document-ai/analyze (upload from web console) and
// GET /document-ai/status?report_id=N. This screen honestly looks up the
// analysis status of a known report ID instead of showing a phantom list.
interface DocStatus {
  report_id: number;
  status: string;
  analysis_type?: string;
  ocr_confidence?: number;
  combined_confidence?: number;
  assessment_status?: string;
  decision?: string;
  requires_review?: number;
}

export default function DocumentAIScreen() {
  const [reportId, setReportId] = useState('');
  const [result, setResult] = useState<DocStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const lookup = async () => {
    const id = reportId.trim();
    if (!id) return;
    setLoading(true);
    setError(null);
    try {
      setResult(await api<DocStatus>(`/document-ai/status?report_id=${encodeURIComponent(id)}`));
    } catch (e) {
      setResult(null);
      setError(e instanceof Error ? e.message : 'Lookup failed');
    }
    setLoading(false);
  };

  return (
    <ScrollView style={s.container} contentContainerStyle={{ paddingBottom: 40 }}>
      <Text style={s.title}>Document AI</Text>
      <Text style={s.subtitle}>Look up the AI analysis status of a submitted result form. New document uploads are handled from the web console (Document AI page).</Text>
      <View style={s.card}>
        <TextInput
          style={s.input}
          value={reportId}
          onChangeText={setReportId}
          placeholder="Report ID"
          keyboardType="number-pad"
        />
        <TouchableOpacity style={s.button} onPress={lookup} disabled={loading || !reportId.trim()} activeOpacity={0.8}>
          <Text style={s.buttonText}>{loading ? 'Checking...' : 'Check Analysis Status'}</Text>
        </TouchableOpacity>
        {error && <Text style={s.error}>{error}</Text>}
        {result && (
          <View style={s.resultBox}>
            <Text style={s.type}>Report #{result.report_id} — {result.status}</Text>
            {result.analysis_type ? <Text style={s.sub}>Type: {result.analysis_type}</Text> : null}
            {result.combined_confidence !== undefined && <Text style={s.sub}>Confidence: {(result.combined_confidence * 100).toFixed(1)}%</Text>}
            {result.assessment_status ? <Text style={s.sub}>Assessment: {result.assessment_status}{result.decision ? ` · ${result.decision}` : ''}</Text> : null}
            {result.requires_review ? <Text style={s.sub}>Flagged for manual review</Text> : null}
          </View>
        )}
      </View>
    </ScrollView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc', padding: 16 },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b', marginBottom: 4, marginTop: 8 },
  subtitle: { fontSize: 14, color: '#64748b', marginBottom: 12 },
  card: { backgroundColor: '#fff', borderRadius: 10, padding: 14, marginBottom: 10, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  input: { borderWidth: 1, borderColor: '#e2e8f0', borderRadius: 8, padding: 10, fontSize: 15, marginBottom: 10 },
  button: { backgroundColor: '#16a34a', borderRadius: 10, padding: 12, alignItems: 'center' },
  buttonText: { color: '#fff', fontWeight: '600' },
  error: { color: '#dc2626', marginTop: 10 },
  resultBox: { marginTop: 12, borderTopWidth: 1, borderTopColor: '#e5e7eb', paddingTop: 10 },
  type: { fontSize: 15, fontWeight: '600', color: '#1e293b' },
  sub: { fontSize: 13, color: '#64748b', marginTop: 6 },
});
