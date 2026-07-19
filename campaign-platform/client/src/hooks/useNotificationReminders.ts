/**
 * useNotificationReminders — Browser Notification API hook
 * Handles permission requests, scheduling reminders 24h before events,
 * and persisting scheduled reminders in localStorage.
 */
import { useState, useEffect, useCallback } from "react";

export interface ReminderEvent {
  id: string;
  title: string;
  stakeholderName: string;
  eventDate: Date;
  reminderDate: Date; // 24h before eventDate
  category: string;
}

export interface ScheduledReminder {
  id: string;
  eventId: string;
  title: string;
  stakeholderName: string;
  eventDate: string; // ISO string
  reminderDate: string; // ISO string
  category: string;
  fired: boolean;
}

const STORAGE_KEY = "inec_stakeholder_reminders";

function loadReminders(): ScheduledReminder[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    return raw ? JSON.parse(raw) : [];
  } catch {
    return [];
  }
}

function saveReminders(reminders: ScheduledReminder[]) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(reminders));
}

export function useNotificationReminders() {
  const [permission, setPermission] = useState<NotificationPermission>(
    typeof Notification !== "undefined" ? Notification.permission : "default"
  );
  const [reminders, setReminders] = useState<ScheduledReminder[]>(loadReminders);
  const [activeTimers, setActiveTimers] = useState<Map<string, ReturnType<typeof setTimeout>>>(new Map());

  // Request notification permission
  const requestPermission = useCallback(async () => {
    if (typeof Notification === "undefined") return "denied" as NotificationPermission;
    const result = await Notification.requestPermission();
    setPermission(result);
    return result;
  }, []);

  // Fire a notification immediately (used when timer triggers)
  const fireNotification = useCallback((reminder: ScheduledReminder) => {
    if (permission !== "granted") return;
    try {
      const n = new Notification(`⏰ Stakeholder Meeting Tomorrow`, {
        body: `${reminder.stakeholderName} — ${reminder.title}\nScheduled: ${new Date(reminder.eventDate).toLocaleDateString("en-NG", { weekday: "long", day: "numeric", month: "long" })}`,
        icon: "/favicon.ico",
        tag: reminder.id,
        requireInteraction: true,
      });
      n.onclick = () => { window.focus(); n.close(); };
    } catch {
      // Notification API not available in this context
    }
    // Mark as fired
    setReminders(prev => {
      const updated = prev.map(r => r.id === reminder.id ? { ...r, fired: true } : r);
      saveReminders(updated);
      return updated;
    });
  }, [permission]);

  // Schedule a reminder for an event
  const scheduleReminder = useCallback(async (event: ReminderEvent): Promise<boolean> => {
    let perm = permission;
    if (perm !== "granted") {
      perm = await requestPermission();
    }
    if (perm !== "granted") return false;

    const existing = reminders.find(r => r.eventId === event.id);
    if (existing && !existing.fired) return true; // already scheduled

    const reminder: ScheduledReminder = {
      id: `reminder_${event.id}_${Date.now()}`,
      eventId: event.id,
      title: event.title,
      stakeholderName: event.stakeholderName,
      eventDate: event.eventDate.toISOString(),
      reminderDate: event.reminderDate.toISOString(),
      category: event.category,
      fired: false,
    };

    const msUntilReminder = event.reminderDate.getTime() - Date.now();

    if (msUntilReminder > 0) {
      // Schedule for the future
      const timer = setTimeout(() => fireNotification(reminder), msUntilReminder);
      setActiveTimers(prev => new Map(prev).set(reminder.id, timer));
    } else if (msUntilReminder > -86400000) {
      // Event is within the next 24h — fire immediately as a "happening soon" alert
      fireNotification({ ...reminder, title: `[TODAY] ${reminder.title}` });
    }
    // else: event is in the past, skip

    setReminders(prev => {
      const filtered = prev.filter(r => r.eventId !== event.id);
      const updated = [...filtered, reminder];
      saveReminders(updated);
      return updated;
    });
    return true;
  }, [permission, reminders, requestPermission, fireNotification]);

  // Cancel a reminder
  const cancelReminder = useCallback((eventId: string) => {
    setReminders(prev => {
      const target = prev.find(r => r.eventId === eventId);
      if (target) {
        const timer = activeTimers.get(target.id);
        if (timer) clearTimeout(timer);
        setActiveTimers(m => { const nm = new Map(m); nm.delete(target.id); return nm; });
      }
      const updated = prev.filter(r => r.eventId !== eventId);
      saveReminders(updated);
      return updated;
    });
  }, [activeTimers]);

  // Check if a specific event has a reminder
  const hasReminder = useCallback((eventId: string) => {
    return reminders.some(r => r.eventId === eventId && !r.fired);
  }, [reminders]);

  // Re-schedule any persisted unfired reminders on mount
  useEffect(() => {
    if (permission !== "granted") return;
    reminders.filter(r => !r.fired).forEach(r => {
      const ms = new Date(r.reminderDate).getTime() - Date.now();
      if (ms > 0) {
        const timer = setTimeout(() => fireNotification(r), ms);
        setActiveTimers(prev => new Map(prev).set(r.id, timer));
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [permission]);

  // Cleanup timers on unmount
  useEffect(() => {
    return () => {
      activeTimers.forEach(t => clearTimeout(t));
    };
  }, [activeTimers]);

  return {
    permission,
    reminders,
    scheduleReminder,
    cancelReminder,
    hasReminder,
    requestPermission,
  };
}
