import { useState, useCallback, useRef } from 'react';
import {
  View, Text, TextInput, TouchableOpacity, StyleSheet,
  FlatList, Image, Alert, ActivityIndicator,
} from 'react-native';
import { CameraView, useCameraPermissions } from 'expo-camera';
import { Ionicons } from '@expo/vector-icons';
import { useFocusEffect } from 'expo-router';
import { observerApi, ObserverReport, API_URL } from '../../src/lib/api';
import { queueReport, syncPendingData, getPendingReportCount } from '../../src/lib/offline';
import { getCurrentLocation } from '../../src/lib/location';

export default function ReportsScreen() {
  const [reports, setReports] = useState<ObserverReport[]>([]);
  const [showCamera, setShowCamera] = useState(false);
  const [photoUri, setPhotoUri] = useState<string | null>(null);
  const [puCode, setPuCode] = useState('');
  const [electionId, setElectionId] = useState('1');
  const [description, setDescription] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [pendingCount, setPendingCount] = useState(0);
  const [permission, requestPermission] = useCameraPermissions();
  const cameraRef = useRef<CameraView>(null);

  const loadReports = useCallback(async () => {
    try {
      const data = await observerApi.reports();
      setReports(data);
    } catch { /* ignore when offline */ }
    setPendingCount(await getPendingReportCount());
  }, []);

  useFocusEffect(useCallback(() => { loadReports(); }, [loadReports]));

  const takePhoto = async () => {
    if (!permission?.granted) {
      await requestPermission();
      return;
    }
    setShowCamera(true);
  };

  const capturePhoto = async () => {
    if (!cameraRef.current) return;
    const photo = await cameraRef.current.takePictureAsync({ quality: 0.8 });
    if (photo) {
      setPhotoUri(photo.uri);
      setShowCamera(false);
    }
  };

  const submitReport = async () => {
    if (!puCode.trim()) {
      Alert.alert('Required', 'Enter a Polling Unit Code');
      return;
    }

    setSubmitting(true);
    const location = await getCurrentLocation();

    try {
      // Try online submission first
      const form = new FormData();
      form.append('polling_unit_code', puCode);
      form.append('election_id', electionId || '1');
      form.append('notes', description);
      if (photoUri) {
        form.append('photo', {
          uri: photoUri,
          name: 'result_sheet.jpg',
          type: 'image/jpeg',
        } as unknown as Blob);
      }

      await observerApi.submitReport(form);
      Alert.alert('Success', 'Report submitted successfully');
      resetForm();
      loadReports();
    } catch {
      // Queue for offline sync
      await queueReport({
        polling_unit_code: puCode,
        election_id: parseInt(electionId || '1', 10),
        report_type: 'result_photo',
        photo_uri: photoUri,
        description,
        latitude: location?.latitude ?? 0,
        longitude: location?.longitude ?? 0,
      });
      Alert.alert('Queued', 'Report saved offline. Will sync when connected.');
      resetForm();
      setPendingCount(await getPendingReportCount());
    } finally {
      setSubmitting(false);
    }
  };

  const syncNow = async () => {
    const result = await syncPendingData();
    if (result.reports > 0) {
      Alert.alert('Synced', `${result.reports} report(s) uploaded`);
      loadReports();
    } else {
      Alert.alert('Sync', 'No pending reports to sync');
    }
    setPendingCount(await getPendingReportCount());
  };

  const resetForm = () => {
    setPhotoUri(null);
    setPuCode('');
    setDescription('');
  };

  if (showCamera) {
    return (
      <View style={styles.cameraContainer}>
        <CameraView ref={cameraRef} style={styles.camera} facing="back">
          <View style={styles.cameraOverlay}>
            <Text style={styles.cameraHint}>
              Position the EC8A result sheet within the frame
            </Text>
          </View>
          <View style={styles.cameraControls}>
            <TouchableOpacity style={styles.cancelButton} onPress={() => setShowCamera(false)}>
              <Ionicons name="close" size={28} color="#fff" />
            </TouchableOpacity>
            <TouchableOpacity style={styles.captureButton} onPress={capturePhoto}>
              <View style={styles.captureInner} />
            </TouchableOpacity>
            <View style={{ width: 44 }} />
          </View>
        </CameraView>
      </View>
    );
  }

  const renderReport = ({ item }: { item: ObserverReport }) => (
    <View style={styles.reportCard}>
      {item.photo_url ? (
        <Image source={{ uri: `${API_URL}${item.photo_url}` }} style={styles.reportThumb} />
      ) : (
        <View style={[styles.reportThumb, styles.noPhoto]}>
          <Ionicons name="document-text" size={24} color="#9ca3af" />
        </View>
      )}
      <View style={styles.reportInfo}>
        <Text style={styles.reportPU}>{item.polling_unit_code}</Text>
        <Text style={styles.reportType}>{item.report_type}</Text>
        <Text style={styles.reportTime}>{new Date(item.created_at).toLocaleString()}</Text>
      </View>
      <View style={[styles.statusBadge, { backgroundColor: item.status === 'verified' ? '#dcfce7' : '#fef3c7' }]}>
        <Text style={[styles.statusText, { color: item.status === 'verified' ? '#166534' : '#92400e' }]}>
          {item.status}
        </Text>
      </View>
    </View>
  );

  return (
    <View style={styles.container}>
      {/* Submission Form */}
      <View style={styles.form}>
        <View style={styles.formRow}>
          <TextInput
            style={[styles.input, { flex: 2 }]}
            placeholder="Polling Unit Code (e.g. PU-001)"
            value={puCode}
            onChangeText={setPuCode}
          />
          <TextInput
            style={[styles.input, { flex: 1 }]}
            placeholder="Election ID"
            value={electionId}
            onChangeText={setElectionId}
            keyboardType="number-pad"
          />
        </View>

        <TextInput
          style={[styles.input, styles.descInput]}
          placeholder="Notes / observations..."
          value={description}
          onChangeText={setDescription}
          multiline
        />

        <View style={styles.photoRow}>
          <TouchableOpacity style={styles.photoButton} onPress={takePhoto}>
            <Ionicons name="camera" size={20} color="#166534" />
            <Text style={styles.photoButtonText}>
              {photoUri ? 'Retake Photo' : 'Take Photo'}
            </Text>
          </TouchableOpacity>
          {photoUri && (
            <Image source={{ uri: photoUri }} style={styles.preview} />
          )}
        </View>

        <TouchableOpacity
          style={[styles.submitButton, submitting && styles.disabledButton]}
          onPress={submitReport}
          disabled={submitting}
        >
          {submitting ? (
            <ActivityIndicator color="#fff" size="small" />
          ) : (
            <Text style={styles.submitText}>Submit Report</Text>
          )}
        </TouchableOpacity>
      </View>

      {/* Pending sync indicator */}
      {pendingCount > 0 && (
        <TouchableOpacity style={styles.pendingBar} onPress={syncNow}>
          <Ionicons name="cloud-upload-outline" size={16} color="#92400e" />
          <Text style={styles.pendingBarText}>{pendingCount} report(s) pending sync</Text>
          <Text style={styles.syncLink}>Sync Now</Text>
        </TouchableOpacity>
      )}

      {/* Reports List */}
      <FlatList
        data={reports}
        keyExtractor={(item) => String(item.id)}
        renderItem={renderReport}
        ListEmptyComponent={<Text style={styles.emptyText}>No reports yet</Text>}
        contentContainerStyle={reports.length === 0 ? styles.emptyContainer : { paddingBottom: 20 }}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb' },
  form: { backgroundColor: '#fff', padding: 16, borderBottomWidth: 1, borderBottomColor: '#e5e7eb' },
  formRow: { flexDirection: 'row', gap: 8 },
  input: {
    borderWidth: 1, borderColor: '#d1d5db', borderRadius: 8,
    padding: 12, fontSize: 14, backgroundColor: '#f9fafb', marginBottom: 8,
  },
  descInput: { minHeight: 60, textAlignVertical: 'top' },
  photoRow: { flexDirection: 'row', alignItems: 'center', gap: 12, marginBottom: 12 },
  photoButton: {
    flexDirection: 'row', alignItems: 'center', gap: 6,
    backgroundColor: '#f0fdf4', padding: 12, borderRadius: 8,
    borderWidth: 1, borderColor: '#bbf7d0',
  },
  photoButtonText: { color: '#166534', fontWeight: '500' },
  preview: { width: 60, height: 60, borderRadius: 8 },
  submitButton: { backgroundColor: '#166534', padding: 14, borderRadius: 8, alignItems: 'center' },
  disabledButton: { opacity: 0.6 },
  submitText: { color: '#fff', fontWeight: '600', fontSize: 15 },
  pendingBar: {
    flexDirection: 'row', alignItems: 'center', gap: 6,
    backgroundColor: '#fef3c7', paddingHorizontal: 16, paddingVertical: 10,
  },
  pendingBarText: { fontSize: 13, color: '#92400e' },
  syncLink: { marginLeft: 'auto', color: '#166534', fontWeight: '600', fontSize: 13 },
  reportCard: {
    flexDirection: 'row', alignItems: 'center', backgroundColor: '#fff',
    marginHorizontal: 12, marginVertical: 4, padding: 12, borderRadius: 8,
    borderWidth: 1, borderColor: '#e5e7eb',
  },
  reportThumb: { width: 48, height: 48, borderRadius: 6, marginRight: 12 },
  noPhoto: { backgroundColor: '#f3f4f6', alignItems: 'center', justifyContent: 'center' },
  reportInfo: { flex: 1 },
  reportPU: { fontSize: 14, fontWeight: '600', color: '#1f2937' },
  reportType: { fontSize: 12, color: '#6b7280' },
  reportTime: { fontSize: 11, color: '#9ca3af', marginTop: 2 },
  statusBadge: { paddingHorizontal: 8, paddingVertical: 4, borderRadius: 10 },
  statusText: { fontSize: 11, fontWeight: '500' },
  emptyText: { fontSize: 14, color: '#9ca3af', textAlign: 'center', marginTop: 40 },
  emptyContainer: { flex: 1, justifyContent: 'center' },
  cameraContainer: { flex: 1 },
  camera: { flex: 1 },
  cameraOverlay: { flex: 1, justifyContent: 'flex-start', alignItems: 'center', paddingTop: 60 },
  cameraHint: { color: '#fff', backgroundColor: 'rgba(0,0,0,0.5)', padding: 10, borderRadius: 8, fontSize: 14 },
  cameraControls: {
    flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center',
    paddingHorizontal: 30, paddingBottom: 40,
  },
  cancelButton: { padding: 8 },
  captureButton: {
    width: 72, height: 72, borderRadius: 36,
    backgroundColor: 'rgba(255,255,255,0.3)', alignItems: 'center', justifyContent: 'center',
  },
  captureInner: { width: 56, height: 56, borderRadius: 28, backgroundColor: '#fff' },
});
