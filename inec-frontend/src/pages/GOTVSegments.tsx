import { useEffect, useState } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Plus, Filter, Users, Trash2, Play } from 'lucide-react';

interface Segment {
  segment_id: string;
  name: string;
  filters: SegmentFilter[];
  created_at: string;
}

interface SegmentFilter {
  field: string;
  operator: string;
  value: string | string[];
}

const FILTER_FIELDS = [
  { value: 'state_code', label: 'State' },
  { value: 'lga_code', label: 'LGA' },
  { value: 'voter_status', label: 'Voter Status' },
  { value: 'last_contact_days', label: 'Days Since Last Contact' },
  { value: 'has_pledge', label: 'Has Pledge' },
  { value: 'no_pledge', label: 'No Pledge' },
];

const OPERATORS = [
  { value: 'eq', label: '=' },
  { value: 'neq', label: '≠' },
  { value: 'gt', label: '>' },
  { value: 'lt', label: '<' },
  { value: 'in', label: 'in' },
];

export default function GOTVSegments() {
  const [segments, setSegments] = useState<Segment[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [newName, setNewName] = useState('');
  const [newFilters, setNewFilters] = useState<SegmentFilter[]>([{ field: 'state_code', operator: 'eq', value: '' }]);
  const [evaluateResult, setEvaluateResult] = useState<Record<string, number>>({});

  const headers = { Authorization: `Bearer ${localStorage.getItem('auth_token')}`, 'X-Party-ID': localStorage.getItem('gotv_party_id') || '1', 'Content-Type': 'application/json' };

  useEffect(() => {
    fetch('/gotv/segments', { headers })
      .then(r => r.json())
      .then(data => setSegments(data.segments || []))
      .catch(() => setSegments([]))
      .finally(() => setLoading(false));
  }, []);

  const createSegment = async () => {
    await fetch('/gotv/segments', {
      method: 'POST', headers,
      body: JSON.stringify({ name: newName, filters: newFilters }),
    });
    setShowCreate(false);
    setNewName('');
    setNewFilters([{ field: 'state_code', operator: 'eq', value: '' }]);
    // Reload
    const data = await (await fetch('/gotv/segments', { headers })).json();
    setSegments(data.segments || []);
  };

  const evaluateSegment = async (segmentId: string) => {
    const res = await fetch(`/gotv/segments/${segmentId}/evaluate`, { headers });
    const data = await res.json();
    setEvaluateResult(prev => ({ ...prev, [segmentId]: data.count || data.contact_ids?.length || 0 }));
  };

  const addFilter = () => setNewFilters([...newFilters, { field: 'state_code', operator: 'eq', value: '' }]);
  const removeFilter = (idx: number) => setNewFilters(newFilters.filter((_, i) => i !== idx));
  const updateFilter = (idx: number, key: keyof SegmentFilter, val: string) =>
    setNewFilters(newFilters.map((f, i) => i === idx ? { ...f, [key]: val } : f));

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold flex items-center gap-2">
          <Filter className="h-5 w-5" /> Contact Segments
        </h2>
        <Button size="sm" onClick={() => setShowCreate(!showCreate)}>
          <Plus className="h-4 w-4 mr-1" /> New Segment
        </Button>
      </div>

      {showCreate && (
        <Card>
          <CardHeader><CardTitle className="text-sm">Create Segment</CardTitle></CardHeader>
          <CardContent className="space-y-3">
            <Input placeholder="Segment name" value={newName} onChange={e => setNewName(e.target.value)} />
            {newFilters.map((f, i) => (
              <div key={i} className="flex gap-2 items-center">
                <select className="border rounded px-2 py-1.5 text-sm" value={f.field}
                  onChange={e => updateFilter(i, 'field', e.target.value)}>
                  {FILTER_FIELDS.map(ff => <option key={ff.value} value={ff.value}>{ff.label}</option>)}
                </select>
                <select className="border rounded px-2 py-1.5 text-sm" value={f.operator}
                  onChange={e => updateFilter(i, 'operator', e.target.value)}>
                  {OPERATORS.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
                </select>
                <Input className="flex-1" placeholder="Value" value={typeof f.value === 'string' ? f.value : ''}
                  onChange={e => updateFilter(i, 'value', e.target.value)} />
                {newFilters.length > 1 && (
                  <Button size="sm" variant="ghost" onClick={() => removeFilter(i)}><Trash2 className="h-3 w-3" /></Button>
                )}
              </div>
            ))}
            <div className="flex gap-2">
              <Button size="sm" variant="outline" onClick={addFilter}>+ Add Filter</Button>
              <Button size="sm" onClick={createSegment} disabled={!newName}>Create</Button>
            </div>
          </CardContent>
        </Card>
      )}

      {loading ? (
        <div className="text-center py-8 text-muted-foreground">Loading segments...</div>
      ) : segments.length === 0 ? (
        <Card><CardContent className="py-8 text-center text-muted-foreground">No segments created yet</CardContent></Card>
      ) : (
        <div className="space-y-2">
          {segments.map(s => (
            <Card key={s.segment_id}>
              <CardContent className="flex items-center gap-4 py-3">
                <div className="flex-1">
                  <div className="font-medium">{s.name}</div>
                  <div className="flex gap-2 mt-1">
                    {s.filters?.map((f, i) => (
                      <Badge key={i} variant="secondary" className="text-xs">
                        {f.field} {f.operator} {typeof f.value === 'string' ? f.value : JSON.stringify(f.value)}
                      </Badge>
                    ))}
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  {evaluateResult[s.segment_id] !== undefined && (
                    <Badge className="bg-green-100 text-green-800">
                      <Users className="h-3 w-3 mr-1" /> {evaluateResult[s.segment_id]} contacts
                    </Badge>
                  )}
                  <Button size="sm" variant="outline" onClick={() => evaluateSegment(s.segment_id)}>
                    <Play className="h-3 w-3 mr-1" /> Evaluate
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
