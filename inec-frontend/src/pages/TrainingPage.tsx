import { useState, useEffect } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { GraduationCap, Award, Headset, BookOpen, Users, TrendingUp } from 'lucide-react';

export default function TrainingPage() {
  const [stats, setStats] = useState<any>(null);
  const [courses, setCourses] = useState<any>(null);
  const [certs, setCerts] = useState<any>(null);
  const [scenarios, setScenarios] = useState<any>(null);
  const [enrollments, setEnrollments] = useState<any>(null);
  const [tab, setTab] = useState('courses');

  useEffect(() => {
    api.getTrainingStats().then(setStats).catch(() => {});
    api.getTrainingCourses().then(setCourses).catch(() => {});
    api.getTrainingCertificates().then(setCerts).catch(() => {});
    api.getVRScenarios().then(setScenarios).catch(() => {});
    api.getTrainingEnrollments().then(setEnrollments).catch(() => {});
  }, []);

  const typeColors: Record<string, string> = {
    vr_simulation: 'bg-purple-100 text-purple-700',
    gamified: 'bg-blue-100 text-blue-700',
    video: 'bg-amber-100 text-amber-700',
    interactive: 'bg-green-100 text-green-700',
    assessment: 'bg-red-100 text-red-700',
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-bold">Training & Capacity Building</h2>
        <p className="text-zinc-500 text-sm">VR simulations, gamified learning, and blockchain-verified credentials</p>
      </div>

      {stats && (
        <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4">
          {[
            { label: 'Active Courses', value: stats.total_courses, icon: BookOpen, color: 'blue' },
            { label: 'Enrollments', value: stats.total_enrollments, icon: Users, color: 'purple' },
            { label: 'Completed', value: stats.completed, icon: GraduationCap, color: 'green' },
            { label: 'Completion Rate', value: `${stats.completion_rate?.toFixed(1)}%`, icon: TrendingUp, color: 'emerald' },
            { label: 'Certificates', value: stats.certificates_issued, icon: Award, color: 'amber' },
            { label: 'VR Scenarios', value: stats.vr_scenarios, icon: Headset, color: 'violet' },
          ].map((s, i) => (
            <Card key={i}>
              <CardContent className="pt-4 pb-3">
                <div className="flex items-center gap-2 mb-1">
                  <s.icon className={`w-4 h-4 text-${s.color}-600`} />
                  <span className="text-xs text-zinc-500">{s.label}</span>
                </div>
                <p className="text-xl font-bold">{s.value}</p>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="courses">Courses</TabsTrigger>
          <TabsTrigger value="vr">VR Simulations</TabsTrigger>
          <TabsTrigger value="enrollments">Enrollments</TabsTrigger>
          <TabsTrigger value="certificates">Certificates</TabsTrigger>
        </TabsList>

        <TabsContent value="courses">
          <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-4">
            {courses?.courses?.map((c: any) => (
              <Card key={c.id} className="relative">
                {c.is_mandatory && <Badge className="absolute top-2 right-2 bg-red-500 text-xs">Mandatory</Badge>}
                <CardContent className="pt-4">
                  <div className="flex items-start gap-3 mb-3">
                    <div className="w-10 h-10 rounded-lg bg-zinc-100 flex items-center justify-center shrink-0">
                      {c.course_type === 'vr_simulation' ? <Headset className="w-5 h-5 text-purple-600" /> :
                       c.course_type === 'gamified' ? <GraduationCap className="w-5 h-5 text-blue-600" /> :
                       <BookOpen className="w-5 h-5 text-green-600" />}
                    </div>
                    <div>
                      <h3 className="font-semibold text-sm">{c.title}</h3>
                      <p className="text-xs text-zinc-500 mt-0.5">{c.target_role?.replace('_', ' ')}</p>
                    </div>
                  </div>
                  <div className="flex flex-wrap gap-1.5 mb-3">
                    <Badge className={`text-xs ${typeColors[c.course_type] || 'bg-zinc-100 text-zinc-700'}`}>{c.course_type?.replace('_', ' ')}</Badge>
                    <Badge variant="outline" className="text-xs capitalize">{c.difficulty}</Badge>
                    <Badge variant="outline" className="text-xs">{c.duration_minutes} min</Badge>
                  </div>
                  <div className="grid grid-cols-3 gap-2 text-center border-t pt-3">
                    <div><p className="text-lg font-bold">{c.enrolled}</p><p className="text-xs text-zinc-500">Enrolled</p></div>
                    <div><p className="text-lg font-bold text-green-600">{c.completed}</p><p className="text-xs text-zinc-500">Completed</p></div>
                    <div><p className="text-lg font-bold">{c.avg_score}</p><p className="text-xs text-zinc-500">Avg Score</p></div>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        </TabsContent>

        <TabsContent value="vr">
          <Card>
            <CardHeader><CardTitle className="text-sm">VR Election Scenarios</CardTitle></CardHeader>
            <CardContent>
              <div className="grid md:grid-cols-2 gap-4">
                {scenarios?.scenarios?.map((s: any) => (
                  <div key={s.id} className="p-4 border rounded-lg">
                    <div className="flex items-center gap-3 mb-2">
                      <div className="w-10 h-10 rounded-lg bg-purple-50 flex items-center justify-center">
                        <Headset className="w-5 h-5 text-purple-600" />
                      </div>
                      <div>
                        <h4 className="font-semibold text-sm">{s.name}</h4>
                        <p className="text-xs text-zinc-500">{s.course_title}</p>
                      </div>
                    </div>
                    <div className="flex gap-2">
                      <Badge variant="outline" className="text-xs capitalize">{s.type?.replace('_', ' ')}</Badge>
                      <Badge variant="outline" className="text-xs capitalize">{s.difficulty}</Badge>
                      <Badge variant="outline" className="text-xs">~{s.avg_completion_minutes} min</Badge>
                      <Badge variant="outline" className="text-xs">Max: {s.max_score}</Badge>
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="enrollments">
          <Card>
            <CardContent className="pt-4">
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead><tr className="border-b text-left text-zinc-500">
                    <th className="pb-2 pr-4">User ID</th><th className="pb-2 pr-4">Course</th><th className="pb-2 pr-4">Progress</th><th className="pb-2 pr-4">Score</th><th className="pb-2">Status</th>
                  </tr></thead>
                  <tbody>
                    {enrollments?.enrollments?.map((e: any) => (
                      <tr key={e.id} className="border-b border-zinc-100">
                        <td className="py-2 pr-4">#{e.user_id}</td>
                        <td className="py-2 pr-4 text-xs">{e.course_title}</td>
                        <td className="py-2 pr-4">
                          <div className="flex items-center gap-2">
                            <div className="w-20 h-2 bg-zinc-100 rounded-full overflow-hidden">
                              <div className="h-full bg-blue-500 rounded-full" style={{ width: `${e.progress}%` }} />
                            </div>
                            <span className="text-xs">{e.progress?.toFixed(0)}%</span>
                          </div>
                        </td>
                        <td className="py-2 pr-4">{e.score || '-'}</td>
                        <td className="py-2">
                          <Badge variant={e.status === 'completed' ? 'default' : e.status === 'failed' ? 'destructive' : 'outline'} className="text-xs">{e.status}</Badge>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="certificates">
          <Card>
            <CardHeader><CardTitle className="text-sm">Blockchain-Verified Certificates</CardTitle></CardHeader>
            <CardContent>
              <div className="space-y-3">
                {certs?.certificates?.map((c: any) => (
                  <div key={c.id} className="flex items-center justify-between p-3 border rounded-lg">
                    <div className="flex items-center gap-3">
                      <div className="w-10 h-10 rounded-full bg-amber-50 flex items-center justify-center">
                        <Award className="w-5 h-5 text-amber-600" />
                      </div>
                      <div>
                        <p className="text-sm font-medium">{c.course_title}</p>
                        <p className="text-xs text-zinc-500">User #{c.user_id} &middot; Score: {c.score}</p>
                      </div>
                    </div>
                    <div className="text-right">
                      <p className="font-mono text-xs text-zinc-400">{c.blockchain_hash}</p>
                      <p className="text-xs text-zinc-500">{new Date(c.issued_at).toLocaleDateString()}</p>
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
