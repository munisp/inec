import { useState, useMemo } from "react";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { trpc } from "@/lib/trpc";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { toast } from "sonner";
import { Link } from "wouter";
import { ArrowLeft, Globe, Plus, Loader2, MessageSquare, Mail, Copy, Check, X, Sparkles } from "lucide-react";

export default function DiasporaOutreach() {
  const { profileId, profile, canEdit, canDelete } = useCandidateProfile();
  const utils = trpc.useUtils();
  const { data: contacts = [], isLoading } = trpc.diaspora.list.useQuery(
    { profileId: profileId! }, { enabled: !!profileId }
  );
  const addMut = trpc.diaspora.add.useMutation({
    onSuccess: () => { utils.diaspora.list.invalidate(); toast.success("Contact added"); setOpen(false); },
    onError: (e) => toast.error(e.message),
  });
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ name: "", country: "", city: "", email: "", phone: "", organization: "", pledgedAmount: "", notes: "" });
  const [selectedContact, setSelectedContact] = useState<typeof contacts[number] | null>(null);
  const [templateType, setTemplateType] = useState<"whatsapp" | "email">("whatsapp");
  const [aiMessage, setAiMessage] = useState<string | null>(null);
  const aiDraftMut = trpc.diaspora.aiDraft.useMutation({
    onSuccess: (data) => { const msg = typeof data.content === "string" ? data.content : ""; setAiMessage(msg); toast.success("AI message generated!"); },
    onError: (e) => toast.error(e.message),
  });
  const [copied, setCopied] = useState(false);

  const messageTemplate = useMemo(() => {
    if (!selectedContact) return "";
    const name = selectedContact.name ?? "Friend";
    const country = selectedContact.country ?? "";
    if (templateType === "whatsapp") {
      return `Dear ${name},\n\nGreetings from Nigeria! We hope this message finds you well in ${country}.\n\nAs a valued member of our diaspora community, your support and voice matter greatly in shaping the future of our nation. We are reaching out to update you on our campaign progress and to invite your participation.\n\nYour pledge and advocacy from abroad make a real difference. Please share this message with fellow Nigerians in ${country} who share our vision for a better Nigeria.\n\nWith gratitude,\nThe Campaign Team`;
    }
    return `Subject: Campaign Update — Your Support Makes a Difference\n\nDear ${name},\n\nI hope this email finds you well in ${country}.\n\nOn behalf of our campaign team, I am writing to personally thank you for your continued support and to share an important update on our progress.\n\nOur campaign has reached significant milestones, and the diaspora community has been instrumental in amplifying our message globally. Your involvement — whether through financial support, social media advocacy, or mobilising fellow Nigerians abroad — is deeply valued.\n\nWe would be honoured if you could:\n• Share our campaign materials with your network in ${country}\n• Encourage eligible voters to register and participate\n• Consider increasing your pledge if circumstances allow\n\nWarm regards,\nThe Campaign Team`;
  }, [selectedContact, templateType]);

  function handleCopy() {
    navigator.clipboard.writeText(messageTemplate).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
      toast.success("Message copied to clipboard");
    });
  }

  const byCountry = contacts.reduce<Record<string, number>>((acc, c) => {
    const key = c.country ?? "Unknown";
    acc[key] = (acc[key] || 0) + 1;
    return acc;
  }, {});

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14}/> Home</Button></Link>
          <Globe size={18} className="text-white"/>
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Diaspora Outreach</h1>
        </div>
        <div className="flex items-center gap-4">
          <div className="text-right"><p className="text-xs text-white/60">CONTACTS</p><p className="font-mono font-bold text-white">{contacts.length}</p></div>
          <div className="text-right"><p className="text-xs text-white/60">COUNTRIES</p><p className="font-mono font-bold text-white">{Object.keys(byCountry).length}</p></div>
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild><Button size="sm" style={{ background: "#008751", color: "white" }} className="gap-1.5"><Plus size={14}/> Add Contact</Button></DialogTrigger>
            <DialogContent>
              <DialogHeader><DialogTitle>Add Diaspora Contact</DialogTitle></DialogHeader>
              <div className="grid gap-3 py-2">
                <Input placeholder="Full Name *" value={form.name} onChange={e => setForm(f => ({ ...f, name: e.target.value }))}/>
                <Input placeholder="Country *" value={form.country} onChange={e => setForm(f => ({ ...f, country: e.target.value }))}/>
                <Input placeholder="City" value={form.city} onChange={e => setForm(f => ({ ...f, city: e.target.value }))}/>
                <Input placeholder="Email" value={form.email} onChange={e => setForm(f => ({ ...f, email: e.target.value }))}/>
                <Input placeholder="Phone / WhatsApp" value={form.phone} onChange={e => setForm(f => ({ ...f, phone: e.target.value }))}/>
                <Input placeholder="Organization / Role" value={form.organization} onChange={e => setForm(f => ({ ...f, organization: e.target.value }))}/>
                <Input type="number" placeholder="Pledged Amount (₦)" value={form.pledgedAmount} onChange={e => setForm(f => ({ ...f, pledgedAmount: e.target.value }))}/>
                <Input placeholder="Notes" value={form.notes} onChange={e => setForm(f => ({ ...f, notes: e.target.value }))}/>
                <Button onClick={() => {
                  if (!profileId || !form.name || !form.country) return toast.error("Name and country required");
                  addMut.mutate({ profileId, fullName: form.name, country: form.country, city: form.city || undefined, email: form.email || undefined, phone: form.phone || undefined, organization: form.organization || undefined, notes: form.notes || undefined });
                }} disabled={addMut.isPending} style={{ background: "#4A1525", color: "white" }}>
                  {addMut.isPending ? <Loader2 size={14} className="animate-spin"/> : "Add Contact"}
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        </div>
      </header>
      <div className="max-w-5xl mx-auto px-6 py-8">
        {Object.keys(byCountry).length > 0 && (
          <div className="flex flex-wrap gap-2 mb-6">
            {Object.entries(byCountry).sort((a, b) => b[1] - a[1]).map(([country, count]) => (
              <div key={country} className="bg-white border border-gray-200 rounded px-3 py-2 flex items-center gap-2">
                <Globe size={12} className="text-gray-400"/>
                <span className="text-sm font-medium text-gray-700">{country}</span>
                <Badge variant="outline">{count}</Badge>
              </div>
            ))}
          </div>
        )}
        {isLoading ? <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400"/></div>
        : contacts.length === 0 ? <div className="text-center py-20 text-gray-500"><Globe size={48} className="mx-auto mb-4 opacity-30"/><p>No diaspora contacts yet</p></div>
        : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {contacts.map(c => (
              <div key={c.id} className="bg-white border border-gray-200 rounded p-4" style={{ borderTop: "3px solid #4A1525" }}>
                <p className="font-semibold text-gray-900 mb-1">{c.name}</p>
                <p className="text-xs text-gray-500">{c.city ? `${c.city}, ` : ""}{c.country}</p>
                {c.organization && <p className="text-xs text-gray-400 mt-1">{c.organization}</p>}
                {c.pledgedAmount && <p className="text-xs font-mono text-green-700 mt-1">Pledged: ₦{c.pledgedAmount.toLocaleString()}</p>}
                <div className="mt-2 text-xs text-gray-500 space-y-0.5">
                  {c.email && <p>{c.email}</p>}
                  {c.phone && <p>{c.phone}</p>}
                </div>
                <div className="mt-3 flex gap-1.5 flex-wrap">
                  {c.phone && (
                    <button
                      className="flex items-center gap-1 px-2 py-1 text-xs rounded bg-green-50 text-green-700 border border-green-200 hover:bg-green-100 transition-colors"
                      onClick={() => { setSelectedContact(c); setTemplateType("whatsapp"); setAiMessage(null); }}
                    >
                      <MessageSquare size={10} /> WhatsApp
                    </button>
                  )}
                  {c.email && (
                    <button
                      className="flex items-center gap-1 px-2 py-1 text-xs rounded bg-blue-50 text-blue-700 border border-blue-200 hover:bg-blue-100 transition-colors"
                      onClick={() => { setSelectedContact(c); setTemplateType("email"); setAiMessage(null); }}
                    >
                      <Mail size={10} /> Email
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
      {/* Message template modal */}
      {selectedContact && (
        <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" onClick={() => setSelectedContact(null)}>
          <div className="bg-white rounded-lg shadow-xl w-full max-w-lg" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between px-5 py-4 border-b" style={{ borderTop: "3px solid #4A1525" }}>
              <div>
                <p className="font-bold text-gray-900">{selectedContact.name}</p>
                <p className="text-xs text-gray-500">{selectedContact.country}</p>
              </div>
              <button onClick={() => { setSelectedContact(null); setAiMessage(null); }} className="text-gray-400 hover:text-gray-600"><X size={18} /></button>
            </div>
            <div className="px-5 pt-4">
              <div className="flex gap-2 mb-3">
                <button
                  className={`flex items-center gap-1.5 px-3 py-1.5 text-xs rounded font-medium transition-colors ${templateType === "whatsapp" ? "bg-green-600 text-white" : "bg-gray-100 text-gray-600 hover:bg-gray-200"}`}
                  onClick={() => setTemplateType("whatsapp")}
                ><MessageSquare size={11} /> WhatsApp</button>
                <button
                  className={`flex items-center gap-1.5 px-3 py-1.5 text-xs rounded font-medium transition-colors ${templateType === "email" ? "bg-blue-600 text-white" : "bg-gray-100 text-gray-600 hover:bg-gray-200"}`}
                  onClick={() => setTemplateType("email")}
                ><Mail size={11} /> Email</button>
              </div>
              <textarea
                className="w-full text-xs font-mono leading-relaxed border border-gray-200 rounded p-3 bg-gray-50 resize-none focus:outline-none focus:ring-1 focus:ring-gray-300"
                rows={12}
                value={aiMessage ?? messageTemplate}
                onChange={e => setAiMessage(e.target.value)}
              />
            </div>
            <div className="flex gap-2 px-5 py-4 border-t flex-wrap">
              <Button size="sm" variant="outline" className="gap-1.5 border-purple-300 text-purple-700 hover:bg-purple-50"
                disabled={aiDraftMut.isPending || !profileId}
                onClick={() => profileId && selectedContact && aiDraftMut.mutate({
                  profileId,
                  contactName: selectedContact.name ?? "Friend",
                  country: selectedContact.country ?? "Nigeria",
                  city: selectedContact.city ?? undefined,
                  messageType: templateType,
                  candidateName: profile?.candidateName ?? undefined,
                  partyName: profile?.partyName ?? undefined,
                  keyMessage: undefined,
                })}>
                {aiDraftMut.isPending ? <Loader2 size={12} className="animate-spin" /> : <Sparkles size={12} />}
                AI Draft
              </Button>
              <Button size="sm" className="gap-1.5 flex-1" style={{ background: "#4A1525", color: "white" }} onClick={handleCopy}>
                {copied ? <><Check size={12} /> Copied!</> : <><Copy size={12} /> Copy Message</>}
              </Button>
              {templateType === "whatsapp" && selectedContact.phone && (
                <Button size="sm" variant="outline" className="gap-1.5 flex-1 border-green-300 text-green-700 hover:bg-green-50" asChild>
                  <a href={`https://wa.me/${selectedContact.phone.replace(/\D/g, "")}?text=${encodeURIComponent(messageTemplate)}`} target="_blank" rel="noopener noreferrer">
                    <MessageSquare size={12} /> Open in WhatsApp
                  </a>
                </Button>
              )}
              {templateType === "email" && selectedContact.email && (
                <Button size="sm" variant="outline" className="gap-1.5 flex-1 border-blue-300 text-blue-700 hover:bg-blue-50" asChild>
                  <a href={`mailto:${selectedContact.email}?subject=Campaign Update — Your Support Makes a Difference&body=${encodeURIComponent(messageTemplate)}`}>
                    <Mail size={12} /> Open in Email
                  </a>
                </Button>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
