// GOTV KOH Indicators — CPI gauge, surveys, endorsements, sentiment, reports.

import { useState, useCallback } from 'react';
import {
  View, Text, StyleSheet, ScrollView, TouchableOpacity,
  RefreshControl, Platform, Alert,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { useFocusEffect } from 'expo-router';
import { CardSkeleton } from '../src/components/SkeletonLoader';
import { gotvFetch } from '../lib/gotv-auth';

interface CPIData {
  cpi_score: number;
  interpretation: string;
  components: {
    voting_intention: number;
    favourability: number;
    digital_sentiment: number;
    ground_mobilisation: number;
    endorsement_index: number;
    share_of_voice: number;
  };
}

interface Survey {
  id: number;
  survey_name: string;
  wave_number: number;
  sample_size: number;
  status: string;
}

interface Endorsement {
  id: number;
  endorser_name: string;
  endorser_type: string;
  verified: boolean;
  date_endorsed: string;
}

interface SentimentSummary {
  total_mentions: number;
  positive_pct: number;
  negative_pct: number;
  neutral_pct: number;
  sentiment_score: number;
}

type SubTab = 'cpi' | 'surveys' | 'endorsements' | 'sentiment' | 'reports';

export default function GOTVIndicatorsScreen() {
  const [subTab, setSubTab] = useState<SubTab>('cpi');
  const [cpi, setCpi] = useState<CPIData | null>(null);
  const [surveys, setSurveys] = useState<Survey[]>([]);
  const [endorsements, setEndorsements] = useState<Endorsement[]>([]);
  const [sentiment, setSentiment] = useState<SentimentSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const load = useCallback(async () => {
    try {
      const [cpiRes, survRes, endRes, sentRes] = await Promise.all([
        gotvFetch<CPIData>('/gotv/koh/cpi/compute').catch(() => null),
        gotvFetch<{ surveys: Survey[] }>('/gotv/koh/surveys').catch(() => ({ surveys: [] })),
        gotvFetch<{ endorsements: Endorsement[] }>('/gotv/koh/endorsements').catch(() => ({ endorsements: [] })),
        gotvFetch<SentimentSummary>('/gotv/koh/social/sentiment').catch(() => null),
      ]);
      setCpi(cpiRes);
      setSurveys(survRes.surveys || []);
      setEndorsements(endRes.endorsements || []);
      setSentiment(sentRes);
    } catch { /* empty */ }
    setLoading(false);
  }, []);

  useFocusEffect(useCallback(() => { load(); }, [load]));

  const onRefresh = useCallback(async () => {
    setRefreshing(true);
    if (Platform.OS !== 'web') Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    await load();
    setRefreshing(false);
  }, [load]);

  const generateReport = async (type: string) => {
    try {
      await gotvFetch(`/gotv/koh/reports/generate/${type}`, { method: 'POST' });
      Alert.alert('Success', `${type} report generated`);
    } catch (e) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed to generate report');
    }
  };

  if (loading) return <CardSkeleton />;

  const getCPIColor = (score: number) => {
    if (score >= 70) return '#22c55e';
    if (score >= 50) return '#f59e0b';
    if (score >= 30) return '#f97316';
    return '#ef4444';
  };

  return (
    <ScrollView
      style={styles.container}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor="#006b3f" />}
    >
      <View style={styles.header}>
        <Text style={styles.title}>KOH Indicators</Text>
        <Text style={styles.subtitle}>Campaign Performance Framework</Text>
      </View>

      {/* Sub-tab selector */}
      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.tabScroll}>
        {([
          { key: 'cpi', label: 'CPI', icon: 'speedometer' },
          { key: 'surveys', label: 'Surveys', icon: 'clipboard' },
          { key: 'endorsements', label: 'Endorsements', icon: 'ribbon' },
          { key: 'sentiment', label: 'Sentiment', icon: 'chatbubbles' },
          { key: 'reports', label: 'Reports', icon: 'document-text' },
        ] as const).map(t => (
          <TouchableOpacity
            key={t.key}
            style={[styles.tabChip, subTab === t.key && styles.tabChipActive]}
            onPress={() => setSubTab(t.key)}
          >
            <Ionicons name={t.icon} size={14} color={subTab === t.key ? '#fff' : '#6b7280'} />
            <Text style={[styles.tabChipText, subTab === t.key && styles.tabChipTextActive]}>{t.label}</Text>
          </TouchableOpacity>
        ))}
      </ScrollView>

      {/* CPI Tab */}
      {subTab === 'cpi' && (
        <>
          {cpi ? (
            <>
              <View style={styles.cpiCard}>
                <Text style={styles.cpiLabel}>Composite Popularity Index</Text>
                <Text style={[styles.cpiScore, { color: getCPIColor(cpi.cpi_score) }]}>
                  {cpi.cpi_score.toFixed(1)}
                </Text>
                <Text style={styles.cpiInterpretation}>{cpi.interpretation}</Text>
              </View>

              <View style={styles.card}>
                <Text style={styles.cardTitle}>Components</Text>
                {([
                  { label: 'Voting Intention (30%)', value: cpi.components.voting_intention },
                  { label: 'Favourability (25%)', value: cpi.components.favourability },
                  { label: 'Sentiment (15%)', value: cpi.components.digital_sentiment },
                  { label: 'Ground Mobilisation (15%)', value: cpi.components.ground_mobilisation },
                  { label: 'Endorsements (10%)', value: cpi.components.endorsement_index },
                  { label: 'Share of Voice (5%)', value: cpi.components.share_of_voice },
                ]).map(c => (
                  <View key={c.label} style={styles.compRow}>
                    <Text style={styles.compLabel}>{c.label}</Text>
                    <View style={styles.compBarBg}>
                      <View style={[styles.compBarFill, { width: `${Math.min(c.value || 0, 100)}%` }]} />
                    </View>
                    <Text style={styles.compValue}>{(c.value || 0).toFixed(0)}%</Text>
                  </View>
                ))}
              </View>
            </>
          ) : (
            <View style={styles.emptyCard}>
              <Ionicons name="speedometer-outline" size={32} color="#9ca3af" />
              <Text style={styles.emptyText}>CPI not yet computed</Text>
            </View>
          )}
        </>
      )}

      {/* Surveys Tab */}
      {subTab === 'surveys' && (
        <>
          {surveys.length === 0 ? (
            <View style={styles.emptyCard}>
              <Ionicons name="clipboard-outline" size={32} color="#9ca3af" />
              <Text style={styles.emptyText}>No surveys yet</Text>
            </View>
          ) : (
            surveys.map(s => (
              <View key={s.id} style={styles.card}>
                <View style={styles.surveyRow}>
                  <View style={styles.waveBadge}>
                    <Text style={styles.waveText}>W{s.wave_number}</Text>
                  </View>
                  <View style={{ flex: 1 }}>
                    <Text style={styles.surveyName}>{s.survey_name}</Text>
                    <Text style={styles.surveyMeta}>n={s.sample_size} · {s.status}</Text>
                  </View>
                </View>
              </View>
            ))
          )}
        </>
      )}

      {/* Endorsements Tab */}
      {subTab === 'endorsements' && (
        <>
          <View style={styles.endorseStats}>
            <Text style={styles.endorseCount}>{endorsements.length}</Text>
            <Text style={styles.endorseLabel}>Total Endorsements</Text>
            <Text style={styles.endorseVerified}>
              {endorsements.filter(e => e.verified).length} verified
            </Text>
          </View>
          {endorsements.slice(0, 15).map(e => (
            <View key={e.id} style={styles.card}>
              <View style={styles.endorseRow}>
                <Ionicons
                  name={e.verified ? 'checkmark-circle' : 'ellipse-outline'}
                  size={18}
                  color={e.verified ? '#22c55e' : '#9ca3af'}
                />
                <View style={{ flex: 1, marginLeft: 10 }}>
                  <Text style={styles.endorseName}>{e.endorser_name}</Text>
                  <Text style={styles.endorseType}>{e.endorser_type.replace('_', ' ')}</Text>
                </View>
              </View>
            </View>
          ))}
        </>
      )}

      {/* Sentiment Tab */}
      {subTab === 'sentiment' && (
        <>
          {sentiment ? (
            <>
              <View style={styles.sentCard}>
                <Text style={styles.sentLabel}>Sentiment Score</Text>
                <Text style={[styles.sentScore, {
                  color: sentiment.sentiment_score >= 0.6 ? '#22c55e' : sentiment.sentiment_score >= 0.4 ? '#f59e0b' : '#ef4444'
                }]}>
                  {(sentiment.sentiment_score * 100).toFixed(0)}%
                </Text>
                <Text style={styles.sentMeta}>{sentiment.total_mentions} mentions analyzed</Text>
              </View>
              <View style={styles.card}>
                <View style={styles.sentRow}>
                  <View style={[styles.sentDot, { backgroundColor: '#22c55e' }]} />
                  <Text style={styles.sentPctLabel}>Positive</Text>
                  <Text style={styles.sentPctValue}>{sentiment.positive_pct}%</Text>
                </View>
                <View style={styles.sentRow}>
                  <View style={[styles.sentDot, { backgroundColor: '#ef4444' }]} />
                  <Text style={styles.sentPctLabel}>Negative</Text>
                  <Text style={styles.sentPctValue}>{sentiment.negative_pct}%</Text>
                </View>
                <View style={styles.sentRow}>
                  <View style={[styles.sentDot, { backgroundColor: '#9ca3af' }]} />
                  <Text style={styles.sentPctLabel}>Neutral</Text>
                  <Text style={styles.sentPctValue}>{sentiment.neutral_pct}%</Text>
                </View>
              </View>
            </>
          ) : (
            <View style={styles.emptyCard}>
              <Ionicons name="chatbubbles-outline" size={32} color="#9ca3af" />
              <Text style={styles.emptyText}>No sentiment data yet</Text>
            </View>
          )}
        </>
      )}

      {/* Reports Tab */}
      {subTab === 'reports' && (
        <View style={styles.reportsGrid}>
          {[
            { type: 'digital_performance', label: 'Digital Performance', icon: 'phone-portrait' },
            { type: 'full_indicators', label: 'Full Indicators', icon: 'document' },
            { type: 'cpi_brief', label: 'CPI Brief', icon: 'speedometer' },
            { type: 'demographic_sentiment', label: 'Demographics', icon: 'people' },
            { type: 'crisis_alert', label: 'Crisis Alert', icon: 'warning' },
          ].map(r => (
            <TouchableOpacity key={r.type} style={styles.reportBtn} onPress={() => generateReport(r.type)}>
              <Ionicons name={r.icon as any} size={24} color="#006b3f" />
              <Text style={styles.reportLabel}>{r.label}</Text>
              <Text style={styles.reportAction}>Generate</Text>
            </TouchableOpacity>
          ))}
        </View>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc' },
  header: { padding: 16, paddingBottom: 8 },
  title: { fontSize: 22, fontWeight: '700', color: '#111827' },
  subtitle: { fontSize: 13, color: '#6b7280', marginTop: 2 },
  tabScroll: { paddingHorizontal: 16, marginBottom: 14, maxHeight: 38 },
  tabChip: { flexDirection: 'row', alignItems: 'center', paddingHorizontal: 12, paddingVertical: 8, borderRadius: 16, backgroundColor: '#f3f4f6', marginRight: 8, gap: 4 },
  tabChipActive: { backgroundColor: '#006b3f' },
  tabChipText: { fontSize: 12, color: '#6b7280', fontWeight: '500' },
  tabChipTextActive: { color: '#fff', fontWeight: '600' },
  cpiCard: { backgroundColor: '#fff', marginHorizontal: 16, marginBottom: 12, borderRadius: 16, padding: 24, alignItems: 'center', shadowColor: '#000', shadowOffset: { width: 0, height: 2 }, shadowOpacity: 0.06, shadowRadius: 4, elevation: 3 },
  cpiLabel: { fontSize: 13, color: '#6b7280' },
  cpiScore: { fontSize: 56, fontWeight: '900', marginVertical: 4 },
  cpiInterpretation: { fontSize: 14, color: '#374151', fontWeight: '500', textTransform: 'capitalize' },
  card: { backgroundColor: '#fff', marginHorizontal: 16, marginBottom: 12, borderRadius: 12, padding: 14, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.05, shadowRadius: 3, elevation: 2 },
  cardTitle: { fontSize: 15, fontWeight: '600', color: '#111827', marginBottom: 12 },
  compRow: { flexDirection: 'row', alignItems: 'center', marginBottom: 10 },
  compLabel: { width: 140, fontSize: 12, color: '#374151' },
  compBarBg: { flex: 1, height: 6, backgroundColor: '#e5e7eb', borderRadius: 3 },
  compBarFill: { height: 6, backgroundColor: '#006b3f', borderRadius: 3 },
  compValue: { width: 36, fontSize: 12, color: '#6b7280', textAlign: 'right', marginLeft: 6 },
  emptyCard: { backgroundColor: '#fff', marginHorizontal: 16, borderRadius: 12, padding: 40, alignItems: 'center', gap: 8 },
  emptyText: { fontSize: 14, color: '#9ca3af' },
  surveyRow: { flexDirection: 'row', alignItems: 'center' },
  waveBadge: { width: 32, height: 32, borderRadius: 16, backgroundColor: '#dbeafe', alignItems: 'center', justifyContent: 'center', marginRight: 12 },
  waveText: { fontSize: 12, fontWeight: '700', color: '#1d4ed8' },
  surveyName: { fontSize: 14, fontWeight: '600', color: '#111827' },
  surveyMeta: { fontSize: 12, color: '#6b7280', marginTop: 2 },
  endorseStats: { backgroundColor: '#fff', marginHorizontal: 16, marginBottom: 12, borderRadius: 12, padding: 20, alignItems: 'center' },
  endorseCount: { fontSize: 36, fontWeight: '800', color: '#006b3f' },
  endorseLabel: { fontSize: 13, color: '#6b7280' },
  endorseVerified: { fontSize: 12, color: '#22c55e', fontWeight: '500', marginTop: 4 },
  endorseRow: { flexDirection: 'row', alignItems: 'center' },
  endorseName: { fontSize: 14, fontWeight: '600', color: '#111827' },
  endorseType: { fontSize: 12, color: '#6b7280', textTransform: 'capitalize' },
  sentCard: { backgroundColor: '#fff', marginHorizontal: 16, marginBottom: 12, borderRadius: 16, padding: 24, alignItems: 'center' },
  sentLabel: { fontSize: 13, color: '#6b7280' },
  sentScore: { fontSize: 48, fontWeight: '800', marginVertical: 4 },
  sentMeta: { fontSize: 12, color: '#6b7280' },
  sentRow: { flexDirection: 'row', alignItems: 'center', marginBottom: 10, gap: 8 },
  sentDot: { width: 10, height: 10, borderRadius: 5 },
  sentPctLabel: { flex: 1, fontSize: 14, color: '#374151' },
  sentPctValue: { fontSize: 14, fontWeight: '600', color: '#111827' },
  reportsGrid: { paddingHorizontal: 16, gap: 10 },
  reportBtn: { backgroundColor: '#fff', borderRadius: 12, padding: 16, flexDirection: 'row', alignItems: 'center', gap: 12, shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: 0.04, shadowRadius: 2, elevation: 1 },
  reportLabel: { flex: 1, fontSize: 14, fontWeight: '600', color: '#111827' },
  reportAction: { fontSize: 12, fontWeight: '600', color: '#006b3f' },
});
