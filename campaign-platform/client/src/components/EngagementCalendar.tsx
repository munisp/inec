/**
 * 90-Day Stakeholder Engagement Calendar
 * Generates a sequenced engagement schedule from scored stakeholders
 * Exports to: iCal (.ics) and printable PDF-ready HTML
 */
import { useState, useMemo } from "react";
import { motion } from "framer-motion";
import {
  Calendar, Download, FileText, Clock, Users,
  ChevronLeft, ChevronRight, Star, MapPin
} from "lucide-react";
import type { Stakeholder } from "./StakeholderTypes";
import { Bell, BellOff } from "lucide-react";
import type { ReminderEvent } from "../hooks/useNotificationReminders";

interface CalendarEvent {
  id: string;
  day: number;
  date: Date;
  week: number;
  stakeholder: Stakeholder;
  eventType: string;
  duration: string;
  location: string;
  notes: string;
  phase: "Legitimacy" | "Mobilisation" | "Consolidation";
}

const EVENT_TYPES: Record<string, string[]> = {
  "Traditional Leaders": ["Royal palace visit", "Chieftaincy title acceptance ceremony", "Community development pledge"],
  "Women":               ["Market visit & town hall", "Women's association meeting", "Cooperative loan pledge event"],
  "Religious":           ["Mosque/church visit", "Religious welfare donation", "Faith leaders roundtable"],
  "Youth":               ["Campus rally", "Skills acquisition workshop", "Town hall with youth leaders"],
  "Agriculture":         ["Farm visit & cooperative meeting", "Fertiliser distribution event", "Agricultural policy dialogue"],
  "Labour":              ["Union congress meeting", "Worker welfare pledge event", "May Day rally participation"],
  "Professional":        ["Bar/medical association dinner", "Policy dialogue forum", "Pro-bono pledge signing"],
  "Civil Society":       ["Transparency pledge signing", "Policy manifesto review session", "CSO roundtable"],
  "Commerce":            ["Market visit", "Trader cooperative meeting", "Business policy dialogue"],
  "Diaspora":            ["Virtual town hall (Zoom)", "Diaspora investment summit", "Remittance policy dialogue"],
  "Inclusion":           ["Disability rights pledge event", "Accessible venue town hall", "Assistive device donation"],
  "Ethnic/Regional":     ["Cultural summit attendance", "Ethnic leaders roundtable", "Heritage event participation"],
  "Pastoral":            ["Community security dialogue", "Farmer-herder mediation meeting", "Rural infrastructure pledge"],
};

const PHASE_COLORS = {
  "Legitimacy":    { bg: "oklch(0.25 0.08 280)", border: "oklch(0.45 0.18 280)", text: "oklch(0.75 0.18 280)" },
  "Mobilisation":  { bg: "oklch(0.22 0.08 145)", border: "oklch(0.45 0.18 145)", text: "oklch(0.70 0.18 145)" },
  "Consolidation": { bg: "oklch(0.25 0.08 50)",  border: "oklch(0.55 0.18 50)",  text: "oklch(0.80 0.18 50)"  },
};

function generateCalendar(stakeholders: Stakeholder[], startDate: Date, stateName: string): CalendarEvent[] {
  const events: CalendarEvent[] = [];
  const priority1 = stakeholders.filter(s => s.priority === 1).slice(0, 10);
  const priority2 = stakeholders.filter(s => s.priority === 2).slice(0, 8);
  const sorted = [...priority1, ...priority2];

  // Phase 1 (Days 1–30): Legitimacy — Traditional, Religious, Women
  // Phase 2 (Days 31–60): Mobilisation — Labour, Youth, Farmers, Commerce
  // Phase 3 (Days 61–90): Consolidation — Professional, Civil Society, Diaspora, Inclusion
  const phaseMap: Record<string, "Legitimacy" | "Mobilisation" | "Consolidation"> = {
    "Traditional Leaders": "Legitimacy",
    "Religious":           "Legitimacy",
    "Women":               "Legitimacy",
    "Labour":              "Mobilisation",
    "Youth":               "Mobilisation",
    "Agriculture":         "Mobilisation",
    "Commerce":            "Mobilisation",
    "Pastoral":            "Mobilisation",
    "Professional":        "Consolidation",
    "Civil Society":       "Consolidation",
    "Diaspora":            "Consolidation",
    "Inclusion":           "Consolidation",
    "Ethnic/Regional":     "Legitimacy",
  };

  const phaseOffsets: Record<string, number> = {
    "Legitimacy":    0,
    "Mobilisation":  30,
    "Consolidation": 60,
  };

  const phaseDayCounters: Record<string, number> = {
    "Legitimacy": 0, "Mobilisation": 0, "Consolidation": 0,
  };

  sorted.forEach((s, idx) => {
    const phase = phaseMap[s.category] ?? "Consolidation";
    const baseOffset = phaseOffsets[phase];
    const dayWithinPhase = phaseDayCounters[phase] * 3 + 1;
    phaseDayCounters[phase]++;
    const day = Math.min(baseOffset + dayWithinPhase, 88);

    const eventDate = new Date(startDate);
    eventDate.setDate(startDate.getDate() + day - 1);
    // Skip Sundays
    if (eventDate.getDay() === 0) eventDate.setDate(eventDate.getDate() + 1);

    const eventTypeOptions = EVENT_TYPES[s.category] ?? ["Meeting", "Town hall", "Dialogue"];
    const eventType = eventTypeOptions[idx % eventTypeOptions.length];

    events.push({
      id: `evt-${s.id}-${day}`,
      day,
      date: new Date(eventDate),
      week: Math.ceil(day / 7),
      stakeholder: s,
      eventType,
      duration: s.priority === 1 ? "2–3 hours" : "1–2 hours",
      location: `${stateName} — ${s.category === "Diaspora" ? "Virtual (Zoom)" : "TBD with group"}`,
      notes: s.cultural_protocol.split(".")[0] + ".",
      phase,
    });
  });

  return events.sort((a, b) => a.day - b.day);
}

function generateICS(events: CalendarEvent[], candidateName: string): string {
  const lines: string[] = [
    "BEGIN:VCALENDAR",
    "VERSION:2.0",
    "PRODID:-//INEC Campaign Intelligence//Stakeholder Calendar//EN",
    "CALSCALE:GREGORIAN",
    "METHOD:PUBLISH",
    `X-WR-CALNAME:${candidateName} — Stakeholder Engagement`,
    "X-WR-TIMEZONE:Africa/Lagos",
  ];

  events.forEach(evt => {
    const d = evt.date;
    const pad = (n: number) => String(n).padStart(2, "0");
    const dateStr = `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}`;
    const uid = `${evt.id}@inec-campaign-intelligence`;
    lines.push(
      "BEGIN:VEVENT",
      `UID:${uid}`,
      `DTSTART;VALUE=DATE:${dateStr}`,
      `DTEND;VALUE=DATE:${dateStr}`,
      `SUMMARY:[${evt.phase}] ${evt.eventType} — ${evt.stakeholder.name}`,
      `DESCRIPTION:Phase: ${evt.phase}\\nCategory: ${evt.stakeholder.category}\\nKey Ask: ${evt.stakeholder.key_ask}\\nCultural Protocol: ${evt.stakeholder.cultural_protocol}\\nDuration: ${evt.duration}`,
      `LOCATION:${evt.location}`,
      `CATEGORIES:${evt.phase},${evt.stakeholder.category}`,
      "STATUS:TENTATIVE",
      "END:VEVENT",
    );
  });

  lines.push("END:VCALENDAR");
  return lines.join("\r\n");
}

function downloadFile(content: string, filename: string, mimeType: string) {
  const blob = new Blob([content], { type: mimeType });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

function generatePrintHTML(events: CalendarEvent[], candidateName: string, stateName: string, office: string): string {
  const phases = ["Legitimacy", "Mobilisation", "Consolidation"] as const;
  const phaseDescs = {
    "Legitimacy":    "Days 1–30 · Establish credibility with traditional, religious, and women's leaders",
    "Mobilisation":  "Days 31–60 · Activate ground networks: labour, youth, farmers, and commerce",
    "Consolidation": "Days 61–90 · Lock in professional, civil society, diaspora, and inclusion endorsements",
  };

  const rows = events.map(e => `
    <tr>
      <td style="padding:6px 8px;border-bottom:1px solid #e5e7eb;font-size:12px;color:#6b7280;">Day ${e.day} · ${e.date.toLocaleDateString("en-NG", { weekday: "short", month: "short", day: "numeric" })}</td>
      <td style="padding:6px 8px;border-bottom:1px solid #e5e7eb;font-size:12px;font-weight:600;">${e.stakeholder.name}</td>
      <td style="padding:6px 8px;border-bottom:1px solid #e5e7eb;font-size:12px;">${e.eventType}</td>
      <td style="padding:6px 8px;border-bottom:1px solid #e5e7eb;font-size:12px;color:#6b7280;">${e.duration}</td>
      <td style="padding:6px 8px;border-bottom:1px solid #e5e7eb;font-size:12px;color:#374151;">${e.stakeholder.key_ask.substring(0, 60)}...</td>
    </tr>`).join("");

  return `<!DOCTYPE html><html><head><meta charset="UTF-8"><title>${candidateName} — 90-Day Stakeholder Calendar</title>
  <style>body{font-family:Georgia,serif;margin:40px;color:#111;}h1{font-size:22px;margin-bottom:4px;}h2{font-size:15px;color:#374151;margin-top:28px;border-bottom:2px solid #111;padding-bottom:4px;}
  .meta{color:#6b7280;font-size:13px;margin-bottom:24px;}table{width:100%;border-collapse:collapse;}th{text-align:left;padding:8px;background:#111;color:white;font-size:12px;}
  .phase-badge{display:inline-block;padding:2px 8px;border-radius:4px;font-size:11px;font-weight:bold;margin-bottom:6px;}
  .leg{background:#ede9fe;color:#5b21b6;}.mob{background:#dcfce7;color:#15803d;}.con{background:#fef3c7;color:#92400e;}
  @media print{body{margin:20px;}}</style></head><body>
  <h1>${candidateName} — 90-Day Stakeholder Engagement Calendar</h1>
  <div class="meta">${office} · ${stateName} · Generated ${new Date().toLocaleDateString("en-NG", { year: "numeric", month: "long", day: "numeric" })}</div>
  ${phases.map(phase => {
    const phaseEvents = events.filter(e => e.phase === phase);
    const cls = phase === "Legitimacy" ? "leg" : phase === "Mobilisation" ? "mob" : "con";
    return `<h2><span class="phase-badge ${cls}">${phase}</span> — ${phaseDescs[phase]}</h2>
    <table><tr><th>Date</th><th>Stakeholder Group</th><th>Event Type</th><th>Duration</th><th>Key Ask</th></tr>
    ${phaseEvents.map(e => `<tr><td style="padding:6px 8px;border-bottom:1px solid #e5e7eb;font-size:12px;color:#6b7280;">Day ${e.day} · ${e.date.toLocaleDateString("en-NG", { weekday: "short", month: "short", day: "numeric" })}</td><td style="padding:6px 8px;border-bottom:1px solid #e5e7eb;font-size:12px;font-weight:600;">${e.stakeholder.name}</td><td style="padding:6px 8px;border-bottom:1px solid #e5e7eb;font-size:12px;">${e.eventType}</td><td style="padding:6px 8px;border-bottom:1px solid #e5e7eb;font-size:12px;color:#6b7280;">${e.duration}</td><td style="padding:6px 8px;border-bottom:1px solid #e5e7eb;font-size:12px;color:#374151;">${e.stakeholder.key_ask.substring(0, 70)}...</td></tr>`).join("")}
    </table>`;
  }).join("")}
  <p style="margin-top:32px;font-size:11px;color:#9ca3af;">Generated by INEC Campaign Intelligence Platform · Stakeholder Engagement Engine v2.0</p>
  </body></html>`;
}

interface Props {
  stakeholders: Stakeholder[];
  candidateName: string;
  stateName: string;
  office: string;
  scheduleReminder?: (event: ReminderEvent) => Promise<boolean>;
  cancelReminder?: (eventId: string) => void;
  hasReminder?: (eventId: string) => boolean;
  notificationPermission?: NotificationPermission;
  onRequestPermission?: () => Promise<NotificationPermission>;
}

export default function EngagementCalendar({ stakeholders, candidateName, stateName, office, scheduleReminder, cancelReminder, hasReminder }: Props) {
  const [startDate] = useState(() => {
    const d = new Date();
    d.setDate(d.getDate() + 7); // Start 1 week from now
    return d;
  });
  const [viewWeek, setViewWeek] = useState(1);

  const events = useMemo(
    () => generateCalendar(stakeholders, startDate, stateName),
    [stakeholders, startDate, stateName]
  );

  const totalWeeks = 13;
  const weekEvents = events.filter(e => e.week === viewWeek);
  const phaseCounts = {
    Legitimacy:    events.filter(e => e.phase === "Legitimacy").length,
    Mobilisation:  events.filter(e => e.phase === "Mobilisation").length,
    Consolidation: events.filter(e => e.phase === "Consolidation").length,
  };

  function handleICSDownload() {
    const ics = generateICS(events, candidateName);
    downloadFile(ics, `${candidateName.replace(/\s+/g, "_")}_stakeholder_calendar.ics`, "text/calendar;charset=utf-8");
  }

  function handlePDFDownload() {
    const html = generatePrintHTML(events, candidateName, stateName, office);
    const win = window.open("", "_blank");
    if (win) {
      win.document.write(html);
      win.document.close();
      win.focus();
      setTimeout(() => win.print(), 500);
    }
  }

  return (
    <div className="flex flex-col gap-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <div className="text-sm font-bold" style={{ color: "oklch(0.88 0.005 240)" }}>
            90-Day Engagement Calendar
          </div>
          <div className="text-xs mt-0.5" style={{ color: "oklch(0.55 0.01 240)" }}>
            {events.length} events · Starting {startDate.toLocaleDateString("en-NG", { month: "long", day: "numeric", year: "numeric" })}
          </div>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={handleICSDownload}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs rounded border transition-all"
            style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.75 0.18 280)" }}
          >
            <Calendar className="w-3.5 h-3.5" />
            Export iCal
          </button>
          <button
            onClick={handlePDFDownload}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs rounded border transition-all"
            style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.75 0.18 50)" }}
          >
            <FileText className="w-3.5 h-3.5" />
            Print / PDF
          </button>
        </div>
      </div>

      {/* Phase summary */}
      <div className="grid grid-cols-3 gap-3">
        {(["Legitimacy", "Mobilisation", "Consolidation"] as const).map(phase => {
          const c = PHASE_COLORS[phase];
          return (
            <div key={phase} className="rounded border p-3" style={{ background: c.bg, borderColor: c.border }}>
              <div className="text-xs font-bold tracking-wider mb-1" style={{ color: c.text }}>{phase.toUpperCase()}</div>
              <div className="text-lg font-bold" style={{ color: c.text }}>{phaseCounts[phase]} events</div>
              <div className="text-xs mt-0.5" style={{ color: "oklch(0.65 0.01 240)" }}>
                {phase === "Legitimacy" ? "Days 1–30" : phase === "Mobilisation" ? "Days 31–60" : "Days 61–90"}
              </div>
            </div>
          );
        })}
      </div>

      {/* Week navigator */}
      <div className="flex items-center justify-between rounded border px-3 py-2" style={{ background: "oklch(0.155 0.008 240)", borderColor: "oklch(0.22 0.01 240)" }}>
        <button
          onClick={() => setViewWeek(w => Math.max(1, w - 1))}
          disabled={viewWeek === 1}
          className="p-1 rounded transition-all disabled:opacity-30"
          style={{ color: "oklch(0.65 0.01 240)" }}
        >
          <ChevronLeft className="w-4 h-4" />
        </button>
        <div className="text-xs font-bold" style={{ color: "oklch(0.88 0.005 240)" }}>
          Week {viewWeek} of {totalWeeks} — Days {(viewWeek - 1) * 7 + 1}–{Math.min(viewWeek * 7, 90)}
        </div>
        <button
          onClick={() => setViewWeek(w => Math.min(totalWeeks, w + 1))}
          disabled={viewWeek === totalWeeks}
          className="p-1 rounded transition-all disabled:opacity-30"
          style={{ color: "oklch(0.65 0.01 240)" }}
        >
          <ChevronRight className="w-4 h-4" />
        </button>
      </div>

      {/* Week events */}
      <div className="space-y-2">
        {weekEvents.length === 0 ? (
          <div className="text-center py-6 text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>
            No events scheduled this week
          </div>
        ) : (
          weekEvents.map((evt, i) => {
            const c = PHASE_COLORS[evt.phase];
            return (
              <motion.div
                key={evt.id}
                initial={{ opacity: 0, x: -8 }}
                animate={{ opacity: 1, x: 0 }}
                transition={{ delay: i * 0.05 }}
                className="rounded border p-3 flex items-start gap-3"
                style={{ background: "oklch(0.155 0.008 240)", borderColor: "oklch(0.22 0.01 240)" }}
              >
                <div className="flex-shrink-0 text-center w-12">
                  <div className="text-xs font-bold" style={{ color: c.text }}>
                    {evt.date.toLocaleDateString("en-NG", { weekday: "short" })}
                  </div>
                  <div className="text-lg font-bold leading-tight" style={{ color: "oklch(0.88 0.005 240)" }}>
                    {evt.date.getDate()}
                  </div>
                  <div className="text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>
                    {evt.date.toLocaleDateString("en-NG", { month: "short" })}
                  </div>
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    {evt.stakeholder.priority === 1 && <Star className="w-3 h-3 flex-shrink-0" style={{ color: "oklch(0.75 0.18 80)" }} fill="oklch(0.75 0.18 80)" />}
                    <span className="text-xs font-bold truncate" style={{ color: "oklch(0.88 0.005 240)" }}>{evt.stakeholder.name}</span>
                    <span className="text-xs px-1.5 py-0.5 rounded flex-shrink-0" style={{ background: c.bg, color: c.text, border: `1px solid ${c.border}` }}>
                      {evt.phase}
                    </span>
                    {scheduleReminder && (
                      <button
                        title={hasReminder?.(evt.id) ? "Cancel reminder" : "Set 24h reminder"}
                        onClick={async () => {
                          if (hasReminder?.(evt.id)) {
                            cancelReminder?.(evt.id);
                          } else {
                            const reminderDate = new Date(evt.date.getTime() - 86400000);
                            await scheduleReminder({
                              id: evt.id,
                              title: evt.eventType,
                              stakeholderName: evt.stakeholder.name,
                              eventDate: evt.date,
                              reminderDate,
                              category: evt.stakeholder.category,
                            });
                          }
                        }}
                        className="ml-auto flex-shrink-0 p-1 rounded transition-colors"
                        style={{
                          color: hasReminder?.(evt.id) ? "oklch(0.65 0.18 145)" : "oklch(0.45 0.01 240)",
                          background: hasReminder?.(evt.id) ? "oklch(0.22 0.12 145)" : "transparent",
                        }}
                      >
                        {hasReminder?.(evt.id) ? <Bell className="w-3.5 h-3.5" fill="currentColor" /> : <BellOff className="w-3.5 h-3.5" />}
                      </button>
                    )}
                  </div>
                  <div className="text-xs mb-1" style={{ color: "oklch(0.72 0.01 240)" }}>{evt.eventType}</div>
                  <div className="flex items-center gap-3 text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>
                    <span className="flex items-center gap-1"><Clock className="w-3 h-3" />{evt.duration}</span>
                    <span className="flex items-center gap-1"><MapPin className="w-3 h-3" />{evt.location}</span>
                  </div>
                  <div className="text-xs mt-1.5 italic" style={{ color: "oklch(0.55 0.01 240)" }}>{evt.notes}</div>
                </div>
              </motion.div>
            );
          })
        )}
      </div>

      {/* Full timeline strip */}
      <div className="rounded border p-3" style={{ background: "oklch(0.155 0.008 240)", borderColor: "oklch(0.22 0.01 240)" }}>
        <div className="text-xs font-bold tracking-wider mb-2" style={{ color: "oklch(0.55 0.01 240)" }}>90-DAY TIMELINE</div>
        <div className="flex gap-0.5">
          {Array.from({ length: 90 }, (_, i) => {
            const day = i + 1;
            const evt = events.find(e => e.day === day);
            const isCurrentWeek = Math.ceil(day / 7) === viewWeek;
            const c = evt ? PHASE_COLORS[evt.phase] : null;
            return (
              <div
                key={day}
                title={evt ? `Day ${day}: ${evt.stakeholder.name}` : `Day ${day}`}
                className="flex-1 h-4 rounded-sm cursor-pointer transition-all"
                style={{
                  background: c ? c.border : "oklch(0.22 0.01 240)",
                  opacity: isCurrentWeek ? 1 : 0.5,
                  outline: isCurrentWeek ? `1px solid oklch(0.88 0.005 240)` : "none",
                }}
                onClick={() => setViewWeek(Math.ceil(day / 7))}
              />
            );
          })}
        </div>
        <div className="flex justify-between text-xs mt-1" style={{ color: "oklch(0.45 0.01 240)" }}>
          <span>Day 1</span><span>Day 30</span><span>Day 60</span><span>Day 90</span>
        </div>
      </div>
    </div>
  );
}
