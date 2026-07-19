import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { trpc } from "@/lib/trpc";
import { Button } from "@/components/ui/button";
import { Link } from "wouter";
import { ArrowLeft, TrendingUp, Loader2, Download, FileText } from "lucide-react";
import { exportToCSV, exportToPDF } from "@/hooks/useExport";
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Cell, PieChart, Pie, Legend } from "recharts";

const COLORS = ["#4A1525","#008751","#1A3A5C","#C0392B","#F59E0B","#6366F1"];

export default function PostElectionAnalytics() {
  const { profileId , canEdit, canDelete } = useCandidateProfile();
  const { data: results = [], isLoading } = trpc.results.list.useQuery({ profileId: profileId! }, { enabled: !!profileId });
  const { data: volunteers = [] } = trpc.volunteers.list.useQuery({ profileId: profileId! }, { enabled: !!profileId });
  const { data: voters = [] } = trpc.voters.list.useQuery({ profileId: profileId! }, { enabled: !!profileId });
  const { data: petitionList = [] } = trpc.petitions.list.useQuery({ profileId: profileId! }, { enabled: !!profileId });

  const totalVotes = results.reduce((s,r)=>s+(r.votes??0),0);
  const lgaBreakdown = results.reduce<Record<string,number>>((acc,r)=>{ acc[r.lga]=(acc[r.lga]||0)+(r.votes??0); return acc; },{});
  const lgaData = Object.entries(lgaBreakdown).map(([lga,votes])=>({lga,votes})).sort((a,b)=>b.votes-a.votes).slice(0,10);
  const pieData = results.map(r=>({ name: r.candidateName, value: r.votes??0 }));

  const EXPORT_COLS_P = [
    { header: "LGA", key: "lga" },
    { header: "Party", key: "party" },
    { header: "Votes", key: "votes" },
    { header: "Ward", key: "ward" },
    { header: "Reported At", key: "reportedAt" },
  ];
  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14}/> Home</Button></Link>
          <TrendingUp size={18} className="text-white"/>
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Post-Election Analytics</h1>
        </div>
        <div className="flex gap-2">
          <Button size="sm" variant="outline" className="gap-1.5 text-white border-white/40 hover:bg-white/10" onClick={() => exportToCSV("post-election", EXPORT_COLS_P, (results ?? []) as Record<string, unknown>[])}><Download size={13}/> CSV</Button>
          <Button size="sm" variant="outline" className="gap-1.5 text-white border-white/40 hover:bg-white/10" onClick={() => exportToPDF("post-election", "Post-Election Analytics Report", "INEC Campaign Intelligence Platform", EXPORT_COLS_P, (results ?? []) as Record<string, unknown>[])}><FileText size={13}/> PDF</Button>
        </div>
      </header>
      <div className="max-w-6xl mx-auto px-4 sm:px-6 py-6 sm:py-8">
        {isLoading ? <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400"/></div>
        : (
          <>
            {/* KPI row */}
            <div className="grid grid-cols-4 gap-4 mb-6">
              {[
                { label: "Total Votes", value: totalVotes.toLocaleString(), color: "#4A1525" },
                { label: "Registered Voters", value: voters.length.toLocaleString(), color: "#1A3A5C" },
                { label: "Volunteers", value: volunteers.length.toLocaleString(), color: "#008751" },
                { label: "Petitions Created", value: petitionList.length.toLocaleString(), color: "#C0392B" },
              ].map(k=>(
                <div key={k.label} className="bg-white border border-gray-200 rounded p-4" style={{ borderTop: `3px solid ${k.color}` }}>
                  <p className="text-xs font-semibold uppercase tracking-widest text-gray-500 mb-1">{k.label}</p>
                  <p className="font-mono text-2xl font-bold" style={{ color: k.color }}>{k.value}</p>
                </div>
              ))}
            </div>
            {results.length === 0 ? (
              <div className="text-center py-20 text-gray-500"><TrendingUp size={48} className="mx-auto mb-4 opacity-30"/><p>Add vote results in the Results Projection page to see analytics here.</p></div>
            ) : (
              <div className="grid grid-cols-2 gap-6">
                <div className="bg-white border border-gray-200 rounded p-5" style={{ borderTop: "3px solid #4A1525" }}>
                  <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-4">Votes by LGA (Top 10)</p>
                  <ResponsiveContainer width="100%" height={260}>
                    <BarChart data={lgaData} margin={{top:5,right:10,left:10,bottom:30}}>
                      <CartesianGrid strokeDasharray="3 3" stroke="#F0EBE8"/>
                      <XAxis dataKey="lga" tick={{fontSize:10}} angle={-30} textAnchor="end" height={60}/>
                      <YAxis tickFormatter={v=>v.toLocaleString()} tick={{fontSize:10}}/>
                      <Tooltip formatter={(v:number)=>[v.toLocaleString(),"Votes"]}/>
                      <Bar dataKey="votes" radius={[3,3,0,0]}>
                        {lgaData.map((_,i)=><Cell key={i} fill={COLORS[i%COLORS.length]}/>)}
                      </Bar>
                    </BarChart>
                  </ResponsiveContainer>
                </div>
                <div className="bg-white border border-gray-200 rounded p-5" style={{ borderTop: "3px solid #1A3A5C" }}>
                  <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-4">Vote Share by Candidate</p>
                  <ResponsiveContainer width="100%" height={260}>
                    <PieChart>
                      <Pie data={pieData} dataKey="value" nameKey="name" cx="50%" cy="50%" outerRadius={90} label={({name,percent})=>`${name} ${(percent*100).toFixed(0)}%`} labelLine={false}>
                        {pieData.map((_,i)=><Cell key={i} fill={COLORS[i%COLORS.length]}/>)}
                      </Pie>
                      <Legend/>
                      <Tooltip formatter={(v:number)=>[v.toLocaleString(),"Votes"]}/>
                    </PieChart>
                  </ResponsiveContainer>
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
