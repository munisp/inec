import React, { useState } from 'react';
import { View, Text, StyleSheet, TextInput, TouchableOpacity, Alert, ScrollView } from 'react-native';
import { API_URL as API } from '../src/lib/api';

export default function SMSVerificationScreen() {
  const [phone, setPhone] = useState('');
  const [code, setCode] = useState('');
  const [sent, setSent] = useState(false);
  const [loading, setLoading] = useState(false);

  const sendCode = async () => {
    if (!phone.trim()) { Alert.alert('Error', 'Enter phone number'); return; }
    setLoading(true);
    try {
      const res = await fetch(`${API}/sms/send-otp`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ phone_number: phone }),
      });
      if (res.ok) { setSent(true); Alert.alert('Sent', 'Verification code sent to your phone'); }
      else Alert.alert('Error', 'Failed to send code');
    } catch (e) { Alert.alert('Error', 'Network error'); }
    setLoading(false);
  };

  const verifyCode = async () => {
    if (!code.trim()) { Alert.alert('Error', 'Enter verification code'); return; }
    setLoading(true);
    try {
      const res = await fetch(`${API}/sms/verify-otp`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ phone_number: phone, code }),
      });
      if (res.ok) Alert.alert('Verified', 'Phone number verified successfully');
      else Alert.alert('Error', 'Invalid or expired code');
    } catch (e) { Alert.alert('Error', 'Network error'); }
    setLoading(false);
  };

  return (
    <ScrollView style={s.container}>
      <Text style={s.title}>SMS Verification</Text>
      <Text style={s.subtitle}>Verify your identity via SMS one-time password</Text>

      <View style={s.card}>
        <Text style={s.label}>Phone Number</Text>
        <TextInput style={s.input} placeholder="+234 xxx xxx xxxx" value={phone} onChangeText={setPhone} keyboardType="phone-pad" />
        {!sent && (
          <TouchableOpacity style={s.btn} onPress={sendCode} disabled={loading}>
            <Text style={s.btnText}>{loading ? 'Sending...' : 'Send Verification Code'}</Text>
          </TouchableOpacity>
        )}
      </View>

      {sent && (
        <View style={s.card}>
          <Text style={s.label}>Verification Code</Text>
          <TextInput style={s.input} placeholder="Enter 6-digit code" value={code} onChangeText={setCode} keyboardType="number-pad" maxLength={6} />
          <TouchableOpacity style={s.btn} onPress={verifyCode} disabled={loading}>
            <Text style={s.btnText}>{loading ? 'Verifying...' : 'Verify Code'}</Text>
          </TouchableOpacity>
          <TouchableOpacity onPress={sendCode} style={s.resend}>
            <Text style={s.resendText}>Resend Code</Text>
          </TouchableOpacity>
        </View>
      )}
    </ScrollView>
  );
}

const s = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#f8fafc', padding: 16 },
  title: { fontSize: 22, fontWeight: '700', color: '#1e293b', marginBottom: 4 },
  subtitle: { fontSize: 14, color: '#64748b', marginBottom: 16 },
  card: { backgroundColor: '#fff', borderRadius: 10, padding: 16, marginBottom: 12, shadowColor: '#000', shadowOpacity: 0.05, shadowRadius: 4 },
  label: { fontSize: 13, fontWeight: '600', color: '#475569', marginBottom: 6 },
  input: { borderWidth: 1, borderColor: '#e2e8f0', borderRadius: 8, padding: 12, fontSize: 16, marginBottom: 12, letterSpacing: 2 },
  btn: { backgroundColor: '#16a34a', paddingVertical: 12, borderRadius: 8, alignItems: 'center' },
  btnText: { color: '#fff', fontSize: 15, fontWeight: '600' },
  resend: { marginTop: 12, alignItems: 'center' },
  resendText: { color: '#3b82f6', fontSize: 14 },
});
