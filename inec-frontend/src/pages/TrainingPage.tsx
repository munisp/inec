import { useState, useEffect } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Input } from '@/components/ui/input';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { GraduationCap, Award, Headset, BookOpen, Users, TrendingUp, UserPlus, Search, Plus } from 'lucide-react';

export default function TrainingPage() {
  const [stats, setStats] = useState<any>(null);
  const [courses, setCourses] = useState<any>(null);
  const [certs, setCerts] = useState<any>(null);
  const [scenarios, setScenarios] = useState<any>(null);
  const [enrollments, setEnrollments] = useState<any>(null);
  const [tab, setTab] = useState('courses');
  const [search, setSearch] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [courseForm, setCourseForm] = useState({ title: '', course_type: 'interactive', target_role: 'presiding_officer', duration_hours: 4, is_mandatory: false });
  const [loadError, setLoadError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    const load = async () => {
      const [statsResult, coursesResult, certsResult, scenariosResult, enrollmentsResult] = await Promise.allSettled([
        api.getTrainingStats(), api.getTrainingCourses(), api.getTrainingCertificates(), api.getVRScenarios(), api.getTrainingEnrollments(),
      ]);
      if (!active) return;
      if (statsResult.status === 'fulfilled') setStats(statsResult.value);
      if (coursesResult.status === 'fulfilled') setCourses(coursesResult.value);
      if (certsResult.status === 'fulfilled') setCerts(certsResult.value);
      if (scenariosResult.status === 'fulfilled') setScenarios(scenariosResult.value);
      if (enrollmentsResult.status === 'fulfilled') setEnrollments(enrollmentsResult.value);
      if ([statsResult, coursesResult, certsResult, scenariosResult, enrollmentsResult].some(result => result.status === 'rejected')) {
        setLoadError('Some training integrations are temporarily unavailable. Available course and enrollment information remains visible.');
      }
    };
    void load();
    return () => { active = false; };
  }, []);

  const [enrolling, setEnrolling] = useState<number | null>(null);

  const handleCreateCourse = async () => {
    if (!courseForm.title) return;
    try {
      await api.createCourse(courseForm);
      setShowCreate(false);
      setCourseForm({ title: '', course_type: 'interactive', target_role: 'presiding_officer', duration_hours: 4, is_mandatory: false });
      api.getTrainingCourses().then(setCourses);
      api.getTrainingStats().then(setStats);
    } catch { setLoadError('The course could not be created. Please check the form and try again.'); }
  };

  const handleEnroll = async (courseId: number) => {
    setEnrolling(courseId);
    try {
      await api.enrollInCourse(courseId);
      api.getTrainingEnrollments().then(setEnrollments);
      api.getTrainingStats().then(setStats);
    } catch { setLoadError('Enrollment could not be completed. Please try again when the training service is available.'); }
    setEnrolling(null);
  };

  const typeColors: Record<string, string> = {
    vr_simulation: 'bg-purple-100 text-purple-700',
    gamified: 'bg-blue-100 text-blue-700',
    video: 'bg-amber-100 text-amber-700',
    interactive: 'bg-green-100 text-green-700',
    assessment: 'bg-red-100 text-red-700',
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div>
          <h2 className="text-2xl font-bold">Training & Capacity Building</h2>
          <p className="text-zinc-500 text-sm">VR simulations, gamified learning, and blockchain-verified credentials</p>
        </div>
        <div className="flex items-center gap-2">
          <div className="relative">
            <Search className="w-4 h-4 absolute left-2.5 top-1/2 -translate-y-1/2 text-zinc-400" />
            <Input placeholder="Search courses..." value={search} onChange={e => setSearch(e.target.value)} className="pl-8 w-48" />
          </div>
          <Dialog open={showCreate} onOpenChange={setShowCreate}>
            <DialogTrigger asChild>
              <Button className="bg-green-700 hover:bg-green-800 gap-1"><Plus className="w-4 h-4" /> New Course</Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader><DialogTitle>Create Training Course</DialogTitle></DialogHeader>
              <div className="space-y-3">
                <Input placeholder="Course Title" value={courseForm.title} onChange={e => setCourseForm({ ...courseForm, title: e.target.value })} />
                <Select value={courseForm.course_type} onValueChange={v => setCourseForm({ ...courseForm, course_type: v })}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="interactive">Interactive</SelectItem>
                    <SelectItem value="video">Video</SelectItem>
                    <SelectItem value="vr_simulation">VR Simulation</SelectItem>
                    <SelectItem value="gamified">Gamified</SelectItem>
                    <SelectItem value="assessment">Assessment</SelectItem>
                  </SelectContent>
                </Select>
                <Select value={courseForm.target_role} onValueChange={v => setCourseForm({ ...courseForm, target_role: v })}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="presiding_officer">Presiding Officer</SelectItem>
                    <SelectItem value="collation_officer">Collation Officer</SelectItem>
                    <SelectItem value="observer">Observer</SelectItem>
                    <SelectItem value="all">All Roles</SelectItem>
                  </SelectContent>
                </Select>
                <Input type="number" placeholder="Duration (hours)" value={courseForm.duration_hours} onChange={e => setCourseForm({ ...courseForm, duration_hours: parseInt(e.target.value) || 0 })} />
                <label className="flex items-center gap-2 text-sm">
                  <input type="checkbox" checked={courseForm.is_mandatory} onChange={e => setCourseForm({ ...courseForm, is_mandatory: e.target.checked })} />
                  Mandatory course
                </label>
                <Button onClick={handleCreateCourse} className="w-full bg-green-700 hover:bg-green-800">Create Course</Button>
              </div>
            </DialogContent>
          </Dialog>
        </div>
      </div>

      {loadError && <div role="alert" className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900">{loadError}</div>}

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
            {courses?.courses?.filter((c: any) => {
              if (!search) return true;
              const q = search.toLowerCase();
              return c.title?.toLowerCase().includes(q) || c.course_type?.toLowerCase().includes(q) || c.target_role?.toLowerCase().includes(q);
            }).map((c: any) => (
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
                  <Button size="sm" className="w-full mt-3" onClick={() => handleEnroll(c.id)} disabled={enrolling === c.id}>
                    <UserPlus className="w-3.5 h-3.5 mr-1" /> {enrolling === c.id ? 'Enrolling...' : 'Enroll'}
                  </Button>
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
