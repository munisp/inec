import { Stack, useRouter, usePathname } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { useEffect, useRef, useState } from 'react';
import { Platform, View } from 'react-native';
import * as Notifications from 'expo-notifications';
import Constants from 'expo-constants';
import type { EventSubscription } from 'expo-modules-core';
import { getDb } from '../src/lib/offline';
import { NetworkBanner } from '../src/components/NetworkBanner';
import { getAuthMode, isRouteAllowed, type AuthMode } from '../lib/auth-context';

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
  const [authMode, setAuthMode] = useState<AuthMode>('none');
  const notificationListener = useRef<EventSubscription | null>(null);
  const responseListener = useRef<EventSubscription | null>(null);
  const router = useRouter();
  const pathname = usePathname();

  useEffect(() => {
    getDb();

    registerForPushNotificationsAsync().then(token => {
      if (token) setExpoPushToken(token);
    });

    // Load auth mode on startup
    getAuthMode().then(setAuthMode);

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

  // Route guard: redirect unauthorized navigation
  useEffect(() => {
    if (authMode === 'none') return;

    const routeName = pathname.replace(/^\//, '');
    if (!routeName) return;

    if (!isRouteAllowed(routeName, authMode)) {
      // GOTV user trying to access INEC screen → redirect to canvasser
      if (authMode === 'gotv') {
        router.replace('/gotv-canvasser');
      }
      // INEC user trying to access GOTV screen → redirect to feed
      if (authMode === 'inec') {
        router.replace('/(tabs)/feed');
      }
    }
  }, [pathname, authMode]);

  return (
    <View style={{ flex: 1 }}>
      <StatusBar style="light" />
      <NetworkBanner />
      <Stack
        screenOptions={{
          headerStyle: { backgroundColor: authMode === 'gotv' ? '#006b3f' : '#166534' },
          headerTintColor: '#fff',
          headerTitleStyle: { fontWeight: 'bold' },
          animation: 'slide_from_right',
        }}
      >
        <Stack.Screen name="index" options={{ headerShown: false }} />
        <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
        {/* GOTV screens */}
        <Stack.Screen name="gotv-login" options={{ headerShown: false }} />
        <Stack.Screen name="gotv-canvasser" options={{ title: 'GOTV Canvasser' }} />
        <Stack.Screen name="gotv" options={{ title: 'GOTV Portal' }} />
        {/* INEC screens */}
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
      </Stack>
    </View>
  );
}
