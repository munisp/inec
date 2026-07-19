/**
 * Stakeholder Contact CRM
 * Log contacts, track meeting outcomes, and manage engagement status
 */
import { useState, useMemo } from "react";
import { motion, AnimatePresence } from "framer-motion";
import {
  UserCheck, Plus, Phone, Mail, Edit3, Trash2, CheckCircle2,
  Clock, XCircle, Calendar, MessageSquare, ChevronDown, Search, Filter
} from "lucide-react";
import { Download, Upload, BadgeCheck, BadgeMinus } from "lucide-react";
import type { CRMContact, Stakeholder } from "./StakeholderTypes";

const STATUS_CONFIG = {
  "Not Started":        { color: "oklch(0.55 0.01 240)",  bg: "oklch(0.22 0.01 240)",  icon: <Clock className="w-3 h-3" /> },
  "Contacted":          { color: "oklch(0.75 0.18 280)",  bg: "oklch(0.22 0.08 280)",  icon: <Phone className="w-3 h-3" /> },
  "Meeting Scheduled":  { color: "oklch(0.75 0.18 50)",   bg: "oklch(0.22 0.08 50)",   icon: <Calendar className="w-3 h-3" /> },
  "Met":                { color: "oklch(0.75 0.18 200)",  bg: "oklch(0.22 0.08 200)",  icon: <MessageSquare className="w-3 h-3" /> },
  "Endorsed":           { color: "oklch(0.70 0.18 145)",  bg: "oklch(0.20 0.08 145)",  icon: <CheckCircle2 className="w-3 h-3" /> },
  "Declined":           { color: "oklch(0.65 0.18 25)",   bg: "oklch(0.22 0.08 25)",   icon: <XCircle className="w-3 h-3" /> },
};

const STATUSES = Object.keys(STATUS_CONFIG) as CRMContact["status"][];

function newContact(stakeholder: Stakeholder): CRMContact {
  return {
    id: `crm-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
    stakeholderId: stakeholder.id,
    stakeholderName: stakeholder.name,
    contactName: "",
    role: "",
    phone: "",
    email: "",
    status: "Not Started",
    lastContact: "",
    nextAction: "",
    notes: "",
    createdAt: new Date().toISOString(),
  };
}

interface ContactFormProps {
  contact: CRMContact;
  stakeholders: Stakeholder[];
  onSave: (c: CRMContact) => void;
  onCancel: () => void;
}

function ContactForm({ contact, stakeholders, onSave, onCancel }: ContactFormProps) {
  const [form, setForm] = useState<CRMContact>({ ...contact });
  const set = (field: keyof CRMContact, value: string) => setForm(f => ({ ...f, [field]: value }));

  return (
    <motion.div
      initial={{ opacity: 0, scale: 0.97 }}
      animate={{ opacity: 1, scale: 1 }}
      className="rounded border p-4 space-y-3"
      style={{ background: "oklch(0.18 0.012 240)", borderColor: "oklch(0.35 0.12 145)" }}
    >
      <div className="text-xs font-bold tracking-wider" style={{ color: "oklch(0.65 0.18 145)" }}>
        {contact.contactName ? "EDIT CONTACT" : "NEW CONTACT"} — {contact.stakeholderName}
      </div>
      <div className="grid grid-cols-2 gap-3">
        {[
          { label: "Contact Name", field: "contactName" as const, placeholder: "e.g. Alhaji Musa Ibrahim" },
          { label: "Role / Title", field: "role" as const, placeholder: "e.g. State Chairman" },
          { label: "Phone Number", field: "phone" as const, placeholder: "+234 800 000 0000" },
          { label: "Email Address", field: "email" as const, placeholder: "contact@example.com" },
          { label: "Last Contact Date", field: "lastContact" as const, placeholder: "e.g. 2025-03-15" },
          { label: "Next Action", field: "nextAction" as const, placeholder: "e.g. Follow-up call on Friday" },
        ].map(({ label, field, placeholder }) => (
          <div key={field}>
            <label className="text-xs tracking-wider mb-1 block" style={{ color: "oklch(0.55 0.01 240)" }}>{label.toUpperCase()}</label>
            <input
              value={form[field] as string}
              onChange={e => set(field, e.target.value)}
              placeholder={placeholder}
              className="w-full px-2 py-1.5 text-xs rounded border"
              style={{ background: "oklch(0.14 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.88 0.005 240)", outline: "none" }}
            />
          </div>
        ))}
      </div>
      <div>
        <label className="text-xs tracking-wider mb-1 block" style={{ color: "oklch(0.55 0.01 240)" }}>STATUS</label>
        <div className="flex flex-wrap gap-1">
          {STATUSES.map(s => {
            const cfg = STATUS_CONFIG[s];
            return (
              <button
                key={s}
                onClick={() => set("status", s)}
                className="flex items-center gap-1 px-2 py-1 text-xs rounded border transition-all"
                style={{
                  background: form.status === s ? cfg.bg : "oklch(0.14 0.008 240)",
                  color: form.status === s ? cfg.color : "oklch(0.55 0.01 240)",
                  borderColor: form.status === s ? cfg.color : "oklch(0.28 0.01 240)",
                }}
              >
                {cfg.icon}{s}
              </button>
            );
          })}
        </div>
      </div>
      <div>
        <label className="text-xs tracking-wider mb-1 block" style={{ color: "oklch(0.55 0.01 240)" }}>MEETING NOTES</label>
        <textarea
          value={form.notes}
          onChange={e => set("notes", e.target.value)}
          placeholder="Record meeting outcomes, commitments made, follow-up actions..."
          rows={3}
          className="w-full px-2 py-1.5 text-xs rounded border resize-none"
          style={{ background: "oklch(0.14 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.88 0.005 240)", outline: "none" }}
        />
      </div>
      <div className="flex gap-2 justify-end">
        <button
          onClick={onCancel}
          className="px-3 py-1.5 text-xs rounded border"
          style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.65 0.01 240)" }}
        >Cancel</button>
        <button
          onClick={() => onSave(form)}
          disabled={!form.contactName.trim()}
          className="px-3 py-1.5 text-xs rounded font-bold transition-all disabled:opacity-40"
          style={{ background: "oklch(0.55 0.18 145)", color: "white" }}
        >Save Contact</button>
      </div>
    </motion.div>
  );
}

interface Props {
  stakeholders: Stakeholder[];
  onContactsChange?: (contacts: CRMContact[]) => void;
}

export default function StakeholderCRM({ stakeholders, onContactsChange }: Props) {
  const [contacts, setContacts] = useState<CRMContact[]>([]);

  function updateContacts(next: CRMContact[]) {
    setContacts(next);
    onContactsChange?.(next);
  }
  const [editingContact, setEditingContact] = useState<CRMContact | null>(null);
  const [addingFor, setAddingFor] = useState<Stakeholder | null>(null);
  const [searchQuery, setSearchQuery] = useState("");
  const [filterStatus, setFilterStatus] = useState<string>("All");
  const [selectedStakeholderFilter, setSelectedStakeholderFilter] = useState("All");

  const stakeholderOptions = useMemo(() => ["All", ...stakeholders.map(s => s.name)], [stakeholders]);

  const filtered = contacts.filter(c => {
    const matchStatus = filterStatus === "All" || c.status === filterStatus;
    const matchStakeholder = selectedStakeholderFilter === "All" || c.stakeholderName === selectedStakeholderFilter;
    const matchSearch = searchQuery === "" ||
      c.contactName.toLowerCase().includes(searchQuery.toLowerCase()) ||
      c.stakeholderName.toLowerCase().includes(searchQuery.toLowerCase()) ||
      c.role.toLowerCase().includes(searchQuery.toLowerCase());
    return matchStatus && matchStakeholder && matchSearch;
  });

  const statusCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    STATUSES.forEach(s => { counts[s] = contacts.filter(c => c.status === s).length; });
    return counts;
  }, [contacts]);

  function handleSave(contact: CRMContact) {
    setContacts(prev => {
      const idx = prev.findIndex(c => c.id === contact.id);
      if (idx >= 0) {
        const next = [...prev];
        next[idx] = contact;
        updateContacts(next);
        return next; // state update handled by updateContacts
      }
      const next = [contact, ...prev];
      updateContacts(next);
      return next;
    });
    setEditingContact(null);
    setAddingFor(null);
  }

  function handleDelete(id: string) {
    setContacts(prev => {
      const next = prev.filter(c => c.id !== id);
      onContactsChange?.(next);
      return next;
    });
  }

  const showForm = editingContact || addingFor;

  // ── CSV Export ────────────────────────────────────────────────────────────────
  function handleCSVExport() {
    const headers = ["ID","Stakeholder","Contact Name","Role","Phone","Email","Status","Last Contact","Next Action","Notes","Created At"];
    const rows = contacts.map(c => [
      c.id, c.stakeholderName, c.contactName, c.role, c.phone, c.email,
      c.status, c.lastContact, c.nextAction,
      `"${c.notes.replace(/"/g, '""')}"`, c.createdAt,
    ]);
    const csv = [headers.join(","), ...rows.map(r => r.join(","))].join("\n");
    const blob = new Blob([csv], { type: "text/csv;charset=utf-8;" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `stakeholder_contacts_${new Date().toISOString().slice(0,10)}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  }

  // ── CSV Import ────────────────────────────────────────────────────────────────
  function handleCSVImport(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (ev) => {
      const text = ev.target?.result as string;
      const lines = text.trim().split("\n");
      if (lines.length < 2) return;
      const imported: CRMContact[] = lines.slice(1).map((line, i) => {
        // Simple CSV parse — handles quoted notes field
        const parts = line.match(/(".*?"|[^,]+)(?=,|$)/g) ?? [];
        const clean = (s?: string) => (s ?? "").replace(/^"|"$/g, "").replace(/""/g, '"').trim();
        return {
          id: clean(parts[0]) || `import-${Date.now()}-${i}`,
          stakeholderId: "",
          stakeholderName: clean(parts[1]),
          contactName: clean(parts[2]),
          role: clean(parts[3]),
          phone: clean(parts[4]),
          email: clean(parts[5]),
          status: (clean(parts[6]) as CRMContact["status"]) || "Not Started",
          lastContact: clean(parts[7]),
          nextAction: clean(parts[8]),
          notes: clean(parts[9]),
          createdAt: clean(parts[10]) || new Date().toISOString(),
        };
      });
      setContacts(prev => {
        // Merge: imported contacts replace existing ones with same ID, new ones are prepended
        const existingIds = new Set(prev.map(c => c.id));
        const newContacts = imported.filter(c => !existingIds.has(c.id));
        const updated = prev.map(c => imported.find(i => i.id === c.id) ?? c);
        const merged = [...newContacts, ...updated];
        onContactsChange?.(merged);
        return merged;
      });
    };
    reader.readAsText(file);
    e.target.value = ""; // reset input
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <div>
          <div className="text-sm font-bold" style={{ color: "oklch(0.88 0.005 240)" }}>Stakeholder Contact CRM</div>
          <div className="text-xs mt-0.5" style={{ color: "oklch(0.55 0.01 240)" }}>{contacts.length} contacts logged · {statusCounts["Endorsed"] ?? 0} endorsed</div>
        </div>
        <div className="flex items-center gap-2">
          {/* CSV Export */}
          {contacts.length > 0 && (
            <button
              onClick={handleCSVExport}
              className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs rounded border transition-all"
              style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.65 0.18 280)" }}
            >
              <Download className="w-3 h-3" />
              Export CSV
            </button>
          )}
          {/* CSV Import */}
          <label
            className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs rounded border transition-all cursor-pointer"
            style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.65 0.18 50)" }}
          >
            <Upload className="w-3 h-3" />
            Import CSV
            <input type="file" accept=".csv" className="hidden" onChange={handleCSVImport} />
          </label>
          {/* Add Contact */}
          {!showForm && (
            <select
              onChange={e => {
                const s = stakeholders.find(st => st.id === e.target.value);
                if (s) setAddingFor(s);
                e.target.value = "";
              }}
              defaultValue=""
              className="appearance-none pl-3 pr-8 py-1.5 text-xs rounded border"
              style={{ background: "oklch(0.55 0.18 145)", borderColor: "oklch(0.55 0.18 145)", color: "white", outline: "none" }}
            >
              <option value="" disabled>+ Add Contact</option>
              {stakeholders.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
            </select>
          )}
        </div>
      </div>

      {/* Status pipeline */}
      <div className="grid grid-cols-6 gap-1">
        {STATUSES.map(s => {
          const cfg = STATUS_CONFIG[s];
          return (
            <button
              key={s}
              onClick={() => setFilterStatus(filterStatus === s ? "All" : s)}
              className="rounded border p-2 text-center transition-all"
              style={{
                background: filterStatus === s ? cfg.bg : "oklch(0.155 0.008 240)",
                borderColor: filterStatus === s ? cfg.color : "oklch(0.22 0.01 240)",
              }}
            >
              <div className="flex justify-center mb-1" style={{ color: cfg.color }}>{cfg.icon}</div>
              <div className="text-sm font-bold" style={{ color: cfg.color }}>{statusCounts[s] ?? 0}</div>
              <div className="text-xs leading-tight" style={{ color: "oklch(0.45 0.01 240)" }}>{s}</div>
            </button>
          );
        })}
      </div>

      {/* Form */}
      <AnimatePresence>
        {showForm && (
          <ContactForm
            contact={editingContact ?? newContact(addingFor!)}
            stakeholders={stakeholders}
            onSave={handleSave}
            onCancel={() => { setEditingContact(null); setAddingFor(null); }}
          />
        )}
      </AnimatePresence>

      {/* Filters */}
      {contacts.length > 0 && (
        <div className="flex gap-2">
          <div className="flex items-center gap-2 flex-1 px-3 py-1.5 rounded border" style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)" }}>
            <Search className="w-3.5 h-3.5" style={{ color: "oklch(0.55 0.01 240)" }} />
            <input
              value={searchQuery}
              onChange={e => setSearchQuery(e.target.value)}
              placeholder="Search contacts..."
              className="flex-1 text-xs bg-transparent outline-none"
              style={{ color: "oklch(0.88 0.005 240)" }}
            />
          </div>
          <div className="relative">
            <select
              value={selectedStakeholderFilter}
              onChange={e => setSelectedStakeholderFilter(e.target.value)}
              className="appearance-none pl-3 pr-7 py-1.5 text-xs rounded border"
              style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.88 0.005 240)", outline: "none" }}
            >
              {stakeholderOptions.map(o => <option key={o} value={o}>{o.length > 30 ? o.slice(0, 30) + "…" : o}</option>)}
            </select>
            <Filter className="absolute right-2 top-1/2 -translate-y-1/2 w-3 h-3 pointer-events-none" style={{ color: "oklch(0.55 0.01 240)" }} />
          </div>
        </div>
      )}

      {/* Contact list */}
      {contacts.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-12 gap-3" style={{ color: "oklch(0.45 0.01 240)" }}>
          <UserCheck className="w-10 h-10 opacity-30" />
          <div className="text-center">
            <div className="text-sm font-bold mb-1">No contacts logged yet</div>
            <div className="text-xs">Use the "Add Contact" button to log your first stakeholder contact</div>
          </div>
        </div>
      ) : (
        <div className="space-y-2">
          <AnimatePresence>
            {filtered.map((contact, i) => {
              const cfg = STATUS_CONFIG[contact.status];
              return (
                <motion.div
                  key={contact.id}
                  initial={{ opacity: 0, y: 8 }}
                  animate={{ opacity: 1, y: 0 }}
                  exit={{ opacity: 0, x: -20 }}
                  transition={{ delay: i * 0.03 }}
                  className="rounded border p-3"
                  style={{ background: "oklch(0.155 0.008 240)", borderColor: "oklch(0.22 0.01 240)" }}
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 mb-1">
                        <span className="text-sm font-bold truncate" style={{ color: "oklch(0.88 0.005 240)" }}>{contact.contactName}</span>
                        <span className="flex items-center gap-1 text-xs px-1.5 py-0.5 rounded flex-shrink-0" style={{ background: cfg.bg, color: cfg.color }}>
                          {cfg.icon}{contact.status}
                        </span>
                      </div>
                      <div className="text-xs mb-1" style={{ color: "oklch(0.65 0.18 145)" }}>{contact.stakeholderName}</div>
                      <div className="text-xs" style={{ color: "oklch(0.55 0.01 240)" }}>{contact.role}</div>
                      <div className="flex items-center gap-3 mt-1.5 text-xs" style={{ color: "oklch(0.45 0.01 240)" }}>
                        {contact.phone && <span className="flex items-center gap-1"><Phone className="w-3 h-3" />{contact.phone}</span>}
                        {contact.email && <span className="flex items-center gap-1"><Mail className="w-3 h-3" />{contact.email}</span>}
                      </div>
                      {contact.nextAction && (
                        <div className="text-xs mt-1.5 px-2 py-1 rounded" style={{ background: "oklch(0.22 0.01 240)", color: "oklch(0.72 0.01 240)" }}>
                          <span className="font-bold">Next: </span>{contact.nextAction}
                        </div>
                      )}
                      {contact.notes && (
                        <div className="text-xs mt-1 italic" style={{ color: "oklch(0.55 0.01 240)" }}>{contact.notes.slice(0, 120)}{contact.notes.length > 120 ? "…" : ""}</div>
                      )}
                    </div>
                    <div className="flex flex-col gap-1 flex-shrink-0">
                      <button
                        onClick={() => setEditingContact(contact)}
                        className="p-1.5 rounded border transition-all"
                        style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.65 0.01 240)" }}
                      ><Edit3 className="w-3 h-3" /></button>
                      <button
                        onClick={() => handleDelete(contact.id)}
                        className="p-1.5 rounded border transition-all"
                        style={{ background: "oklch(0.18 0.008 240)", borderColor: "oklch(0.28 0.01 240)", color: "oklch(0.65 0.18 25)" }}
                      ><Trash2 className="w-3 h-3" /></button>
                      {contact.status === "Endorsed" && (
                        <button
                          title={contact.verified ? "Remove verification" : "Mark as officially verified"}
                          onClick={() => {
                            const next = contacts.map(c => c.id === contact.id ? { ...c, verified: !c.verified } : c);
                            setContacts(next);
                            localStorage.setItem("inec_crm_contacts", JSON.stringify(next));
                            onContactsChange?.(next);
                          }}
                          className="p-1.5 rounded border transition-all"
                          style={{
                            background: contact.verified ? "oklch(0.20 0.08 240)" : "oklch(0.18 0.008 240)",
                            borderColor: contact.verified ? "oklch(0.55 0.18 240)" : "oklch(0.28 0.01 240)",
                            color: contact.verified ? "oklch(0.65 0.18 240)" : "oklch(0.45 0.01 240)",
                          }}
                        >{contact.verified ? <BadgeCheck className="w-3 h-3" /> : <BadgeMinus className="w-3 h-3" />}</button>
                      )}
                    </div>
                  </div>
                </motion.div>
              );
            })}
          </AnimatePresence>
        </div>
      )}
    </div>
  );
}
