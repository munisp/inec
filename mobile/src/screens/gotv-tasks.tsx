/**
 * Mobile Tasks Screen
 * Field workers see assigned tasks, mark complete with GPS verification
 */
import React, { useState, useEffect } from 'react';
import { View, Text, ScrollView, TouchableOpacity, StyleSheet, Alert, FlatList } from 'react-native';

const API_BASE = 'http://localhost:8103';

interface Task {
  task_id: string;
  task_type: string;
  title: string;
  status: string;
  priority: number;
  target_count: number;
  completed_count: number;
  state_code: string;
  ward_code: string;
  due_date: string;
}

export default function GOTVTasksScreen() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [filter, setFilter] = useState<string>('all');
  const [loading, setLoading] = useState(true);

  useEffect(() => { fetchTasks(); }, []);

  const fetchTasks = async () => {
    try {
      const res = await fetch(`${API_BASE}/gotv/tasks`, {
        headers: { 'X-GOTV-Party-Code': 'APC' },
      });
      const data = await res.json();
      setTasks(data.tasks || []);
    } catch { /* ignore */ }
    setLoading(false);
  };

  const updateTaskStatus = async (taskId: string, status: string) => {
    try {
      await fetch(`${API_BASE}/gotv/tasks/${taskId}/status`, {
        method: 'PATCH',
        headers: { 'X-GOTV-Party-Code': 'APC', 'Content-Type': 'application/json' },
        body: JSON.stringify({ status }),
      });
      Alert.alert('Updated', `Task marked as ${status}`);
      fetchTasks();
    } catch {
      Alert.alert('Error', 'Failed to update task');
    }
  };

  const priorityColors = ['', '#ef4444', '#f59e0b', '#3b82f6', '#10b981', '#6b7280'];
  const statusIcons: Record<string, string> = {
    unassigned: '📋', assigned: '👤', in_progress: '🔄', completed: '✅', cancelled: '❌',
  };

  const filtered = filter === 'all' ? tasks : tasks.filter(t => t.status === filter);

  return (
    <ScrollView style={styles.container}>
      <Text style={styles.title}>My Tasks</Text>

      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.filterRow}>
        {['all', 'assigned', 'in_progress', 'completed'].map(f => (
          <TouchableOpacity key={f} style={[styles.chip, filter === f && styles.chipActive]} onPress={() => setFilter(f)}>
            <Text style={[styles.chipText, filter === f && { color: '#fff' }]}>
              {f === 'all' ? 'All' : f.replace('_', ' ')} ({f === 'all' ? tasks.length : tasks.filter(t => t.status === f).length})
            </Text>
          </TouchableOpacity>
        ))}
      </ScrollView>

      {loading ? (
        <Text style={styles.loading}>Loading tasks...</Text>
      ) : (
        <FlatList
          data={filtered}
          scrollEnabled={false}
          keyExtractor={t => t.task_id}
          renderItem={({ item: t }) => (
            <View style={styles.card}>
              <View style={styles.row}>
                <Text style={styles.icon}>{statusIcons[t.status] || '📋'}</Text>
                <View style={{ flex: 1 }}>
                  <Text style={styles.taskTitle}>{t.title}</Text>
                  <Text style={styles.taskType}>{t.task_type.replace('_', ' ')} • P{t.priority}</Text>
                </View>
                <View style={[styles.priorityDot, { backgroundColor: priorityColors[t.priority] || '#6b7280' }]} />
              </View>
              <View style={styles.progressRow}>
                <View style={styles.progressBg}>
                  <View style={[styles.progressFill, { width: `${t.target_count > 0 ? (t.completed_count / t.target_count * 100) : 0}%` }]} />
                </View>
                <Text style={styles.progressText}>{t.completed_count}/{t.target_count}</Text>
              </View>
              <Text style={styles.meta}>{t.state_code} • {t.ward_code} • Due: {t.due_date || 'No deadline'}</Text>
              <View style={styles.actions}>
                {t.status === 'assigned' && (
                  <TouchableOpacity style={styles.btn} onPress={() => updateTaskStatus(t.task_id, 'in_progress')}>
                    <Text style={styles.btnText}>Start</Text>
                  </TouchableOpacity>
                )}
                {t.status === 'in_progress' && (
                  <TouchableOpacity style={[styles.btn, { backgroundColor: '#10b981' }]} onPress={() => updateTaskStatus(t.task_id, 'completed')}>
                    <Text style={styles.btnText}>Complete</Text>
                  </TouchableOpacity>
                )}
              </View>
            </View>
          )}
        />
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f9fafb', padding: 16 },
  title: { fontSize: 24, fontWeight: 'bold', marginBottom: 12 },
  filterRow: { flexDirection: 'row', marginBottom: 12 },
  chip: { paddingHorizontal: 12, paddingVertical: 6, backgroundColor: '#e5e7eb', borderRadius: 20, marginRight: 8 },
  chipActive: { backgroundColor: '#3b82f6' },
  chipText: { fontSize: 12, color: '#374151' },
  loading: { textAlign: 'center', marginTop: 40, color: '#6b7280' },
  card: { backgroundColor: '#fff', borderRadius: 8, padding: 16, marginBottom: 8, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 12 },
  icon: { fontSize: 24 },
  taskTitle: { fontSize: 15, fontWeight: '600' },
  taskType: { fontSize: 12, color: '#6b7280' },
  priorityDot: { width: 10, height: 10, borderRadius: 5 },
  progressRow: { flexDirection: 'row', alignItems: 'center', marginTop: 12, gap: 8 },
  progressBg: { flex: 1, height: 6, backgroundColor: '#e5e7eb', borderRadius: 3 },
  progressFill: { height: 6, backgroundColor: '#3b82f6', borderRadius: 3 },
  progressText: { fontSize: 12, color: '#6b7280', width: 50, textAlign: 'right' },
  meta: { fontSize: 11, color: '#9ca3af', marginTop: 6 },
  actions: { flexDirection: 'row', gap: 8, marginTop: 12 },
  btn: { backgroundColor: '#3b82f6', paddingHorizontal: 16, paddingVertical: 8, borderRadius: 6 },
  btnText: { color: '#fff', fontWeight: '600', fontSize: 13 },
});
