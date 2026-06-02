import { useState, useCallback } from 'react';
import {
  View, Text, StyleSheet, ScrollView, TouchableOpacity,
  Platform, ActivityIndicator, Alert,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { useFocusEffect } from 'expo-router';
import { documentAIApi, observerApi, ObserverReport, DocumentAnalysis } from '../src/lib/api';
import { EmptyState } from '../src/components/EmptyState';
import { CardSkeleton } from '../src/components/SkeletonLoader';

export default function DocumentAIScreen() {
  const [reports, setReports] = useState<ObserverReport[]>([]);
  const [loading, setLoading] = useState(true);
  const [analyzing, setAnalyzing] = useState<number | null>(null);
  const [analysisResults, setAnalysisResults] = useState<Record<number, DocumentAnalysis>>({});

  const loadReports = useCallback(async () => {
    try {
      const data = await observerApi.reports();
      setReports(data);
    } catch { /* */ }
    setLoading(false);
  }, []);

  useFocusEffect(useCallback(() => { loadReports(); }, [loadReports]));

  const analyzeReport = async (reportId: number) => {
    setAnalyzing(reportId);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
    try {
      const result = await documentAIApi.analyze(reportId);
      setAnalysisResults(prev => ({ ...prev, [reportId]: result }));
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
    } catch (e) {
      Alert.alert('Analysis Failed', e instanceof Error ? e.message : 'Could not analyze document');
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error);
    }
    setAnalyzing(null);
  };

  if (loading) return <View style={styles.container}><CardSkeleton /><CardSkeleton /></View>;

  return (
    <ScrollView style={styles.container} contentContainerStyle={{ paddingBottom: Platform.OS === 'ios' ? 100 : 80 }}>
      <View style={styles.headerCard}>
        <View style={styles.headerIcon}>
          <Ionicons name="scan" size={28} color="#0891b2" />
        </View>
        <Text style={styles.headerTitle}>Document AI Analysis</Text>
        <Text style={styles.headerSubtitle}>
          Analyze EC8A result sheet photos with PaddleOCR, VLM validation, and DocLing table extraction
        </Text>
      </View>

      {reports.length === 0 ? (
        <EmptyState icon="document-text-outline" title="No Reports" description="Submit observer reports with photos to analyze" />
      ) : (
        reports.map((r) => {
          const analysis = analysisResults[r.id];
          const isAnalyzing = analyzing === r.id;

          return (
            <View key={r.id} style={styles.reportCard}>
              <View style={styles.reportHeader}>
                <View style={styles.reportInfo}>
                  <Text style={styles.reportTitle}>Report #{r.id}</Text>
                  <Text style={styles.reportPU}>{r.polling_unit_code}</Text>
                </View>
                <View style={[styles.reportStatus, { backgroundColor: r.status === 'reviewed' ? '#f0fdf4' : '#fffbeb' }]}>
                  <Text style={[styles.reportStatusText, { color: r.status === 'reviewed' ? '#22c55e' : '#f59e0b' }]}>
                    {r.status}
                  </Text>
                </View>
              </View>

              {r.description && <Text style={styles.reportDesc} numberOfLines={2}>{r.description}</Text>}

              {!analysis && (
                <TouchableOpacity
                  style={styles.analyzeButton}
                  onPress={() => analyzeReport(r.id)}
                  disabled={isAnalyzing}
                  activeOpacity={0.8}
                >
                  {isAnalyzing ? (
                    <ActivityIndicator size="small" color="#fff" />
                  ) : (
                    <Ionicons name="scan-outline" size={16} color="#fff" />
                  )}
                  <Text style={styles.analyzeButtonText}>{isAnalyzing ? 'Analyzing...' : 'Analyze with AI'}</Text>
                </TouchableOpacity>
              )}

              {analysis && (
                <View style={styles.analysisContainer}>
                  {/* OCR Results */}
                  <View style={styles.analysisSection}>
                    <View style={styles.analysisSectionHeader}>
                      <Ionicons name="text" size={14} color="#0891b2" />
                      <Text style={styles.analysisSectionTitle}>OCR Extraction</Text>
                      <Text style={styles.confidenceBadge}>{(analysis.ocr.confidence_score * 100).toFixed(0)}%</Text>
                    </View>
                    {analysis.ocr.serial_number && (
                      <Text style={styles.ocrField}>Serial: <Text style={styles.ocrValue}>{analysis.ocr.serial_number}</Text></Text>
                    )}
                    {analysis.ocr.party_results.slice(0, 4).map((pr, i) => (
                      <View key={i} style={styles.ocrPartyRow}>
                        <Text style={styles.ocrPartyCode}>{pr.party_code}</Text>
                        <Text style={styles.ocrPartyVotes}>{pr.votes}</Text>
                        <Text style={styles.ocrConfidence}>{(pr.confidence * 100).toFixed(0)}%</Text>
                      </View>
                    ))}
                    {analysis.ocr.extraction_warnings.length > 0 && (
                      <View style={styles.warningsBox}>
                        {analysis.ocr.extraction_warnings.map((w, i) => (
                          <View key={i} style={styles.warningRow}>
                            <Ionicons name="warning" size={12} color="#f59e0b" />
                            <Text style={styles.warningText}>{w}</Text>
                          </View>
                        ))}
                      </View>
                    )}
                  </View>

                  {/* VLM Validation */}
                  <View style={styles.analysisSection}>
                    <View style={styles.analysisSectionHeader}>
                      <Ionicons name="eye" size={14} color="#7c3aed" />
                      <Text style={styles.analysisSectionTitle}>VLM Validation</Text>
                    </View>
                    <View style={styles.vlmRow}>
                      <Text style={styles.vlmLabel}>Valid EC8A</Text>
                      <Ionicons name={analysis.vlm.is_valid_ec8a ? 'checkmark-circle' : 'close-circle'} size={16} color={analysis.vlm.is_valid_ec8a ? '#22c55e' : '#ef4444'} />
                    </View>
                    <View style={styles.vlmRow}>
                      <Text style={styles.vlmLabel}>Tampering</Text>
                      <Ionicons name={!analysis.vlm.tampering_detected ? 'checkmark-circle' : 'alert-circle'} size={16} color={!analysis.vlm.tampering_detected ? '#22c55e' : '#ef4444'} />
                    </View>
                    <View style={styles.vlmRow}>
                      <Text style={styles.vlmLabel}>Quality</Text>
                      <Text style={styles.vlmValue}>{analysis.vlm.document_quality}</Text>
                    </View>
                    <View style={styles.vlmRow}>
                      <Text style={styles.vlmLabel}>Completeness</Text>
                      <Text style={styles.vlmValue}>{(analysis.vlm.completeness_score * 100).toFixed(0)}%</Text>
                    </View>
                  </View>

                  <View style={[styles.overallConfidence, { borderColor: analysis.combined_confidence > 0.8 ? '#22c55e' : analysis.combined_confidence > 0.5 ? '#f59e0b' : '#ef4444' }]}>
                    <Text style={styles.overallLabel}>Overall Confidence</Text>
                    <Text style={[styles.overallValue, { color: analysis.combined_confidence > 0.8 ? '#22c55e' : analysis.combined_confidence > 0.5 ? '#f59e0b' : '#ef4444' }]}>
                      {(analysis.combined_confidence * 100).toFixed(1)}%
                    </Text>
                  </View>
                </View>
              )}
            </View>
          );
        })
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  headerCard: { margin: 16, marginBottom: 8, backgroundColor: '#fff', borderRadius: 16, padding: 20, alignItems: 'center', shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  headerIcon: { width: 56, height: 56, borderRadius: 16, backgroundColor: '#cffafe', justifyContent: 'center', alignItems: 'center', marginBottom: 12 },
  headerTitle: { fontSize: 18, fontWeight: '700', color: '#111827', marginBottom: 4 },
  headerSubtitle: { fontSize: 13, color: '#6b7280', textAlign: 'center', lineHeight: 18 },
  reportCard: { marginHorizontal: 16, marginBottom: 10, backgroundColor: '#fff', borderRadius: 14, padding: 16, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 4, elevation: 2 },
  reportHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 },
  reportInfo: {},
  reportTitle: { fontSize: 14, fontWeight: '700', color: '#111827' },
  reportPU: { fontSize: 12, color: '#9ca3af', marginTop: 2 },
  reportStatus: { paddingHorizontal: 8, paddingVertical: 4, borderRadius: 8 },
  reportStatusText: { fontSize: 10, fontWeight: '700', textTransform: 'uppercase' },
  reportDesc: { fontSize: 13, color: '#6b7280', marginBottom: 12 },
  analyzeButton: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 8, backgroundColor: '#0891b2', paddingVertical: 10, borderRadius: 10 },
  analyzeButtonText: { fontSize: 14, fontWeight: '600', color: '#fff' },
  analysisContainer: { marginTop: 12, gap: 10 },
  analysisSection: { backgroundColor: '#f9fafb', borderRadius: 10, padding: 12 },
  analysisSectionHeader: { flexDirection: 'row', alignItems: 'center', gap: 6, marginBottom: 8 },
  analysisSectionTitle: { fontSize: 13, fontWeight: '700', color: '#374151', flex: 1 },
  confidenceBadge: { fontSize: 11, fontWeight: '700', color: '#0891b2', backgroundColor: '#cffafe', paddingHorizontal: 6, paddingVertical: 2, borderRadius: 6 },
  ocrField: { fontSize: 12, color: '#6b7280', marginBottom: 4 },
  ocrValue: { fontWeight: '700', color: '#111827' },
  ocrPartyRow: { flexDirection: 'row', alignItems: 'center', paddingVertical: 3 },
  ocrPartyCode: { width: 40, fontSize: 12, fontWeight: '700', color: '#374151' },
  ocrPartyVotes: { flex: 1, fontSize: 12, fontWeight: '600', color: '#111827' },
  ocrConfidence: { fontSize: 11, color: '#9ca3af' },
  warningsBox: { marginTop: 8, gap: 4 },
  warningRow: { flexDirection: 'row', alignItems: 'center', gap: 4 },
  warningText: { fontSize: 11, color: '#f59e0b', flex: 1 },
  vlmRow: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', paddingVertical: 4 },
  vlmLabel: { fontSize: 12, color: '#6b7280' },
  vlmValue: { fontSize: 12, fontWeight: '700', color: '#111827' },
  overallConfidence: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', padding: 12, borderRadius: 10, borderWidth: 2, backgroundColor: '#fff' },
  overallLabel: { fontSize: 13, fontWeight: '600', color: '#374151' },
  overallValue: { fontSize: 18, fontWeight: '800' },
});
