import { Stack, useRouter } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { useEffect, useRef, useState } from 'react';
import { Platform, View } from 'react-native';
import * as Notifications from 'expo-notifications';
import Constants from 'expo-constants';
import type { EventSubscription } from 'expo-modules-core';
import { getDb } from '../src/lib/offline';
import { NetworkBanner } from '../src/components/NetworkBanner';

Notifications.setNotificationHandler({
  handleNotification: async () => ({
    shouldShowBanner: true,
    shouldShowList: true,
    shouldPlaySound: true,
    shouldSetBadge: true,
  }),
});

async function registerForPushNotificationsAsync(): Promise<string | undefined> {
  if (Platform.OS === 'android') {
    await Notifications.setNotificationChannelAsync('election-alerts', {
      name: 'Election Alerts',
      importance: Notifications.AndroidImportance.MAX,
      vibrationPattern: [0, 250, 250, 250],
      lightColor: '#166534',
    });
  }

  const { status: existingStatus } = await Notifications.getPermissionsAsync();
  let finalStatus = existingStatus;
  if (existingStatus !== 'granted') {
    const { status } = await Notifications.requestPermissionsAsync();
    finalStatus = status;
  }
  if (finalStatus !== 'granted') {
    return undefined;
  }

  const projectId = Constants.expoConfig?.extra?.eas?.projectId;
  const tokenData = await Notifications.getExpoPushTokenAsync({
    projectId: projectId ?? undefined,
  });
  return tokenData.data;
}

export default function RootLayout() {
  const [expoPushToken, setExpoPushToken] = useState<string | undefined>();
  const notificationListener = useRef<EventSubscription | null>(null);
  const responseListener = useRef<EventSubscription | null>(null);
  const router = useRouter();

  useEffect(() => {
    getDb();

    registerForPushNotificationsAsync().then(token => {
      if (token) setExpoPushToken(token);
    });

    notificationListener.current = Notifications.addNotificationReceivedListener(() => {
      // Notification received while app is in foreground
    });

    responseListener.current = Notifications.addNotificationResponseReceivedListener(response => {
      const data = response.notification.request.content.data;
      if (data?.screen) {
        router.push(data.screen as string);
      }
    });

    return () => {
      notificationListener.current?.remove();
      responseListener.current?.remove();
    };
  }, []);

  return (
    <View style={{ flex: 1 }}>
      <StatusBar style="light" />
      <NetworkBanner />
      <Stack
        screenOptions={{
          headerStyle: { backgroundColor: '#166534' },
          headerTintColor: '#fff',
          headerTitleStyle: { fontWeight: 'bold' },
          animation: 'slide_from_right',
        }}
      >
        <Stack.Screen name="index" options={{ headerShown: false }} />
        <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
        <Stack.Screen name="kyc" options={{ title: 'KYC Verification' }} />
        <Stack.Screen name="disputes" options={{ title: 'Disputes' }} />
        <Stack.Screen name="elections" options={{ title: 'Elections' }} />
        <Stack.Screen name="results" options={{ title: 'Results & Collation' }} />
        <Stack.Screen name="document-ai" options={{ title: 'Document AI' }} />
        <Stack.Screen name="scale-health" options={{ title: 'System Health' }} />
        <Stack.Screen name="geofencing" options={{ title: 'Geofencing' }} />
        <Stack.Screen name="voter-search" options={{ title: 'Voter Search' }} />
        <Stack.Screen name="middleware" options={{ title: 'Middleware' }} />
        <Stack.Screen name="biometrics" options={{ title: 'Biometrics' }} />
        <Stack.Screen name="integrity" options={{ title: 'Integrity Score' }} />
        <Stack.Screen name="compliance" options={{ title: 'Compliance Report' }} />
        <Stack.Screen name="tv-dashboard" options={{ title: 'Live Results' }} />
        <Stack.Screen name="bvas-sync" options={{ title: 'BVAS Sync' }} />
        <Stack.Screen name="blockchain" options={{ title: 'Blockchain Ledger' }} />
        <Stack.Screen name="citizen-portal" options={{ title: 'Citizen Portal' }} />
        <Stack.Screen name="observer-monitoring" options={{ title: 'Observer Monitoring' }} />
        <Stack.Screen name="voter-registration" options={{ title: 'Voter Registration' }} />
        <Stack.Screen name="sms-verification" options={{ title: 'SMS/USSD Verification' }} />
        <Stack.Screen name="export-center" options={{ title: 'Export Center' }} />
        <Stack.Screen name="predictive-analytics" options={{ title: 'Predictive Analytics' }} />
        <Stack.Screen name="data-validation" options={{ title: 'Data Validation' }} />
        <Stack.Screen name="duplicate-detection" options={{ title: 'Duplicate Detection' }} />
        <Stack.Screen name="enrollment-kiosk" options={{ title: 'Enrollment Kiosk' }} />
        <Stack.Screen name="hardware-biometric" options={{ title: 'Hardware Biometric' }} />
      </Stack>
    </View>
  );
}
