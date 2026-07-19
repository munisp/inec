import { useState, useEffect } from "react";
import { useCandidateProfile } from "@/contexts/CandidateProfileContext";
import { trpc } from "@/lib/trpc";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { toast } from "sonner";
import { Link } from "wouter";
import { ArrowLeft, BarChart2, Plus, Loader2, RefreshCw, Radio } from "lucide-react";
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Cell, PieChart, Pie, Legend } from "recharts";

const COLORS = ["#4A1525","#008751","#1A3A5C","#C0392B","#F59E0B","#6366F1","#EC4899","#14B8A6"];
const REFRESH_INTERVAL = 30_000;

export default function ResultsProjection() {
  const { profileId } = useCandidateProfile();
  const utils = trpc.useUtils();
  const { data: results = [], isLoading, dataUpdatedAt } = trpc.results.list.useQuery(
    { profileId: profileId! },
    { enabled: !!profileId, refetchInterval: REFRESH_INTERVAL }
  );
  const addMut = trpc.results.add.useMutation({
    onSuccess: () => { utils.results.list.invalidate(); toast.success("Result added"); setOpen(false); },
    onError: e => toast.error(e.message),
  });
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ lga:"", candidateName:"", partyName:"", votes:"" });
  const [lastRefresh, setLastRefresh] = useState<Date | null>(null);
  const [countdown, setCountdown] = useState(REFRESH_INTERVAL / 1000);

  useEffect(() => {
    if (dataUpdatedAt) setLastRefresh(new Date(dataUpdatedAt));
  }, [dataUpdatedAt]);

  useEffect(() => {
    setCountdown(REFRESH_INTERVAL / 1000);
    const interval = setInterval(() => setCountdown(c => c <= 1 ? REFRESH_INTERVAL / 1000 : c - 1), 1000);
    return () => clearInterval(interval);
  }, [dataUpdatedAt]);

  const total = results.reduce((s,r)=>s+(r.votes??0),0);
  const chartData = results.map(r=>({ name: r.candidateName, votes: r.votes??0, lga: r.lga }));
  const pieData = results.reduce<Record<string,number>>((acc,r)=>{ acc[r.party??r.candidateName]=(acc[r.party??r.candidateName]||0)+(r.votes??0); return acc; },{});
  const pieChartData = Object.entries(pieData).map(([name,value])=>({name,value}));

  return (
    <div className="min-h-screen" style={{ background: "#F5F0EB" }}>
      <header style={{ background: "#4A1525" }} className="px-6 py-4 flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Link href="/"><Button variant="ghost" size="sm" className="text-white gap-1 hover:bg-white/10"><ArrowLeft size={14}/> Home</Button></Link>
          <BarChart2 size={18} className="text-white"/>
          <h1 className="text-white font-bold text-lg" style={{ fontFamily: "'Playfair Display', serif" }}>Results Projection</h1>
          {/* LIVE badge */}
          <span className="flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-bold" style={{ background: "#C0392B", color: "white" }}>
            <Radio size={10} className="animate-pulse"/> LIVE
          </span>
        </div>
        <div className="flex items-center gap-3 flex-wrap">
          <div className="text-right">
            <p className="text-xs text-white/60">TOTAL VOTES</p>
            <p className="font-mono font-bold text-white">{total.toLocaleString()}</p>
          </div>
          {/* Refresh indicator */}
          <div className="text-right">
            <p className="text-xs text-white/60">NEXT REFRESH</p>
            <p className="font-mono text-xs text-white">{countdown}s</p>
          </div>
          <Button size="sm" variant="outline" className="gap-1.5 text-white border-white/40 hover:bg-white/10"
            onClick={() => utils.results.list.invalidate()}>
            <RefreshCw size={13}/> Refresh
          </Button>
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild>
              <Button size="sm" style={{ background: "#008751", color: "white" }} className="gap-1.5">
                <Plus size={14}/> Add Result
              </Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader><DialogTitle>Add Vote Result</DialogTitle></DialogHeader>
              <div className="grid gap-3 py-2">
                <Input placeholder="LGA *" value={form.lga} onChange={e=>setForm(f=>({...f,lga:e.target.value}))}/>
                <Input placeholder="Candidate Name *" value={form.candidateName} onChange={e=>setForm(f=>({...f,candidateName:e.target.value}))}/>
                <Input placeholder="Party" value={form.partyName} onChange={e=>setForm(f=>({...f,partyName:e.target.value}))}/>
                <Input type="number" placeholder="Votes *" value={form.votes} onChange={e=>setForm(f=>({...f,votes:e.target.value}))}/>
                <Button onClick={()=>{ if(!profileId||!form.lga||!form.candidateName||!form.votes) return toast.error("LGA, candidate and votes required"); addMut.mutate({profileId,lga:form.lga,party:form.partyName||form.candidateName,votes:parseInt(form.votes)}); }} disabled={addMut.isPending} style={{background:"#4A1525",color:"white"}}>
                  {addMut.isPending?<Loader2 size={14} className="animate-spin"/>:"Add"}
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        </div>
      </header>

      <div className="max-w-5xl mx-auto px-6 py-8">
        {lastRefresh && (
          <p className="text-xs text-gray-400 mb-4">Last updated: {lastRefresh.toLocaleTimeString()} · Auto-refreshes every {REFRESH_INTERVAL/1000}s</p>
        )}
        {isLoading
          ? <div className="flex justify-center py-20"><Loader2 size={32} className="animate-spin text-gray-400"/></div>
          : results.length === 0
            ? <div className="text-center py-20 text-gray-500"><BarChart2 size={48} className="mx-auto mb-4 opacity-30"/><p>No results yet. Add vote tallies above.</p></div>
            : (
              <>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-6 mb-6">
                  <div className="bg-white border border-gray-200 rounded p-5" style={{ borderTop: "3px solid #4A1525" }}>
                    <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-4">Vote Distribution by Candidate</p>
                    <ResponsiveContainer width="100%" height={260}>
                      <BarChart data={chartData} margin={{top:5,right:20,left:10,bottom:40}}>
                        <CartesianGrid strokeDasharray="3 3" stroke="#F0EBE8"/>
                        <XAxis dataKey="name" tick={{fontSize:10}} angle={-25} textAnchor="end" height={60}/>
                        <YAxis tickFormatter={v=>v.toLocaleString()} tick={{fontSize:11}}/>
                        <Tooltip formatter={(v:number)=>[v.toLocaleString(),"Votes"]}/>
                        <Bar dataKey="votes" radius={[3,3,0,0]}>
                          {chartData.map((_,i)=><Cell key={i} fill={COLORS[i%COLORS.length]}/>)}
                        </Bar>
                      </BarChart>
                    </ResponsiveContainer>
                  </div>
                  <div className="bg-white border border-gray-200 rounded p-5" style={{ borderTop: "3px solid #1A3A5C" }}>
                    <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-4">Vote Share by Party</p>
                    <ResponsiveContainer width="100%" height={260}>
                      <PieChart>
                        <Pie data={pieChartData} dataKey="value" nameKey="name" cx="50%" cy="50%" outerRadius={90}
                          label={({name,percent})=>`${name} ${(percent*100).toFixed(0)}%`} labelLine={false}>
                          {pieChartData.map((_,i)=><Cell key={i} fill={COLORS[i%COLORS.length]}/>)}
                        </Pie>
                        <Legend/>
                        <Tooltip formatter={(v:number)=>[v.toLocaleString(),"Votes"]}/>
                      </PieChart>
                    </ResponsiveContainer>
                  </div>
                </div>
                {/* Percentage bar chart per candidate */}
                <div className="bg-white border border-gray-200 rounded p-5 mb-6" style={{ borderTop: "3px solid #008751" }}>
                  <p className="text-xs font-bold uppercase tracking-widest text-gray-500 mb-4">Vote Share % per Candidate</p>
                  <div className="space-y-3">
                    {results.sort((a,b)=>(b.votes??0)-(a.votes??0)).map((r,i) => {
                      const pct = total > 0 ? ((r.votes??0)/total*100) : 0;
                      return (
                        <div key={r.id}>
                          <div className="flex justify-between text-xs mb-1">
                            <span className="font-medium text-gray-900">{r.candidateName} <span className="text-gray-400">({r.party ?? r.lga})</span></span>
                            <span className="font-mono font-bold" style={{ color: COLORS[i%COLORS.length] }}>{pct.toFixed(1)}%</span>
                          </div>
                          <div className="h-3 rounded-full bg-gray-100 overflow-hidden">
                            <div className="h-full rounded-full transition-all duration-700"
                              style={{ width: `${pct}%`, background: COLORS[i%COLORS.length] }}/>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </div>
                <div className="bg-white border border-gray-200 rounded overflow-hidden">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="bg-gray-50 border-b">
                        {["LGA","Candidate","Party","Votes","% Share"].map(h=>(
                          <th key={h} className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-gray-500">{h}</th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {results.map((r,i)=>(
                        <tr key={r.id} className={i%2===0?"bg-white":"bg-gray-50/50"}>
                          <td className="px-4 py-3 text-gray-600">{r.lga}</td>
                          <td className="px-4 py-3 font-medium text-gray-900">{r.candidateName}</td>
                          <td className="px-4 py-3 text-gray-600">{r.party ?? "—"}</td>
                          <td className="px-4 py-3 font-mono font-bold">{(r.votes??0).toLocaleString()}</td>
                          <td className="px-4 py-3 font-mono">{total>0?((r.votes??0)/total*100).toFixed(1):0}%</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </>
            )}
      </div>
    </div>
  );
}
