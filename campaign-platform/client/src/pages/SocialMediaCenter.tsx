import { useState } from "react";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { trpc } from "@/lib/trpc";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { toast } from "sonner";
import { Link } from "wouter";
import { ArrowLeft, Share2, Loader2, Plus } from "lucide-react";

const PLATFORMS = ["Twitter","Facebook","WhatsApp","Instagram"];
const PLATFORM_COLORS: Record<string,string> = { Twitter:"#1DA1F2", Facebook:"#1877F2", WhatsApp:"#25D366", Instagram:"#E1306C" };

export default function SocialMediaCenter() {
  const { profileId, profile , canEdit, canDelete } = useCandidateProfile();
  const utils = trpc.useUtils();
  const { data: posts = [], isLoading } = trpc.socialMedia.list.useQuery({ profileId: profileId! }, { enabled: !!profileId });
  const saveMut = trpc.socialMedia.save.useMutation({
    onSuccess: () => { utils.socialMedia.list.invalidate(); toast.success("Post saved"); setContent(""); },
    onError: e => toast.error(e.message),
  });

  const [platform, setPlatform] = useState("Twitter");
  const [content, setContent] = useState("");
  const [filter, setFilter] = useState("All");

  const limits: Record<string,number> = { Twitter:280, Facebook:63206, WhatsApp:65536, Instagram:2200 };
  const limit = limits[platform] ?? 280;

  const filtered = filter === "All" ? posts : posts.filter(p => p.platform === filter);

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14}/> Home</Button></Link>
          <Share2 size={18} className="text-white"/>
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Social Media Center</h1>
        </div>
        <div className="flex gap-2">
          {["All",...PLATFORMS].map(p=>(
            <button key={p} onClick={()=>setFilter(p)} className="px-3 py-1 text-xs font-semibold rounded-full transition-all"
              style={{ background: filter===p?"white":"transparent", color: filter===p?"#4A1525":"white", border:"1px solid white" }}>
              {p}
            </button>
          ))}
        </div>
      </header>
      <div className="max-w-4xl mx-auto px-6 py-8">
        {/* Composer */}
        <div className="bg-white border border-gray-200 rounded p-5 mb-6" style={{ borderTop: "3px solid #4A1525" }}>
          <div className="flex items-center gap-3 mb-3">
            <Select value={platform} onValueChange={setPlatform}>
              <SelectTrigger className="w-40"><SelectValue/></SelectTrigger>
              <SelectContent>{PLATFORMS.map(p=><SelectItem key={p} value={p}>{p}</SelectItem>)}</SelectContent>
            </Select>
            <span className="text-xs text-gray-400">{content.length}/{limit} chars</span>
          </div>
          <textarea
            value={content}
            onChange={e=>setContent(e.target.value)}
            rows={4}
            placeholder={`Write a ${platform} post for ${profile?.candidateName ?? "the campaign"}…`}
            className="w-full text-sm text-gray-700 border border-gray-200 rounded p-3 resize-none outline-none focus:border-gray-400"
            maxLength={limit}
          />
          <div className="flex justify-end mt-3">
            <Button onClick={()=>{ if(!profileId||!content.trim()) return toast.error("Content required"); saveMut.mutate({profileId,platform,content,status:"pending"}); }}
              disabled={saveMut.isPending||!content.trim()} style={{background:"#4A1525",color:"white"}} className="gap-1.5">
              {saveMut.isPending?<Loader2 size={14} className="animate-spin"/>:<><Plus size={14}/> Save Post</>}
            </Button>
          </div>
        </div>
        {/* Feed */}
        {isLoading ? <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400"/></div>
        : filtered.length === 0 ? <div className="text-center py-20 text-gray-500"><Share2 size={48} className="mx-auto mb-4 opacity-30"/><p>No posts yet</p></div>
        : (
          <div className="space-y-3">
            {filtered.map(p=>(
              <div key={p.id} className="bg-white border border-gray-200 rounded p-4">
                <div className="flex items-center gap-2 mb-2">
                  <Badge style={{ background: PLATFORM_COLORS[p.platform]+"22", color: PLATFORM_COLORS[p.platform] }}>{p.platform}</Badge>
                  <span className="text-xs text-gray-400">{new Date(p.createdAt).toLocaleString()}</span>
                  <Badge variant="outline" className="ml-auto">{p.status}</Badge>
                </div>
                <p className="text-sm text-gray-700 whitespace-pre-wrap">{p.content}</p>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
