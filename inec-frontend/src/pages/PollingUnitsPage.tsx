import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Input } from '@/components/ui/input';
import { MapPin, Activity, Search } from 'lucide-react';

interface State { code: string; name: string; }
interface LGA { code: string; name: string; }
interface PU {
  code: string; name: string; ward_code: string; registered_voters: number;
  ward_name: string; lga_name: string; state_name: string; latitude: number; longitude: number;
}

export default function PollingUnitsPage() {
  const [states, setStates] = useState<State[]>([]);
  const [lgas, setLgas] = useState<LGA[]>([]);
  const [pus, setPus] = useState<PU[]>([]);
  const [selectedState, setSelectedState] = useState('');
  const [selectedLga, setSelectedLga] = useState('');
  const [loading, setLoading] = useState(false);
  const [search, setSearch] = useState('');

  useEffect(() => { api.getStates().then(setStates); }, []);
  useEffect(() => {
    if (selectedState) {
      api.getLgas(selectedState).then(setLgas);
      setSelectedLga('');
      setPus([]);
    }
  }, [selectedState]);
  useEffect(() => {
    if (selectedLga) {
      setLoading(true);
      api.getPollingUnits({ lga_code: selectedLga, limit: '100' }).then(setPus).finally(() => setLoading(false));
    }
  }, [selectedLga]);

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3 flex-wrap">
        <Select value={selectedState} onValueChange={setSelectedState}>
          <SelectTrigger className="w-48"><SelectValue placeholder="Select State" /></SelectTrigger>
          <SelectContent>
            {states.map(s => <SelectItem key={s.code} value={s.code}>{s.name}</SelectItem>)}
          </SelectContent>
        </Select>
        {lgas.length > 0 && (
          <Select value={selectedLga} onValueChange={setSelectedLga}>
            <SelectTrigger className="w-48"><SelectValue placeholder="Select LGA" /></SelectTrigger>
            <SelectContent>
              {lgas.map(l => <SelectItem key={l.code} value={l.code}>{l.name}</SelectItem>)}
            </SelectContent>
          </Select>
        )}
        {pus.length > 0 && (
          <div className="relative">
            <Search className="w-4 h-4 absolute left-2.5 top-1/2 -translate-y-1/2 text-zinc-400" />
            <Input placeholder="Search PU name/code..." value={search} onChange={e => setSearch(e.target.value)} className="pl-8 w-52" />
          </div>
        )}
        {pus.length > 0 && <Badge variant="outline">{pus.filter(pu => {
          if (!search) return true;
          const q = search.toLowerCase();
          return pu.name?.toLowerCase().includes(q) || pu.code?.toLowerCase().includes(q) || pu.ward_name?.toLowerCase().includes(q);
        }).length} of {pus.length} polling units</Badge>}
      </div>

      {!selectedState && (
        <Card>
          <CardContent className="py-16 text-center">
            <MapPin className="w-12 h-12 mx-auto text-zinc-300 mb-3" />
            <p className="text-zinc-500">Select a state and LGA to view polling units</p>
            <p className="text-xs text-zinc-400 mt-1">37 states + FCT | 774 LGAs | 176,000+ polling units</p>
          </CardContent>
        </Card>
      )}

      {loading && <div className="flex items-center justify-center h-32"><Activity className="w-5 h-5 animate-spin text-green-700" /></div>}

      {pus.length > 0 && (
        <Card>
          <CardContent className="overflow-x-auto p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Code</TableHead>
                  <TableHead>Name</TableHead>
                  <TableHead>Ward</TableHead>
                  <TableHead>LGA</TableHead>
                  <TableHead className="text-right">Registered Voters</TableHead>
                  <TableHead>Coordinates</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {pus.filter(pu => {
                  if (!search) return true;
                  const q = search.toLowerCase();
                  return pu.name?.toLowerCase().includes(q) || pu.code?.toLowerCase().includes(q) || pu.ward_name?.toLowerCase().includes(q);
                }).map(pu => (
                  <TableRow key={pu.code}>
                    <TableCell className="font-mono text-xs">{pu.code}</TableCell>
                    <TableCell className="font-medium">{pu.name}</TableCell>
                    <TableCell className="text-sm">{pu.ward_name}</TableCell>
                    <TableCell className="text-sm">{pu.lga_name}</TableCell>
                    <TableCell className="text-right">{new Intl.NumberFormat().format(pu.registered_voters)}</TableCell>
                    <TableCell className="text-xs text-zinc-400">{pu.latitude?.toFixed(4)}, {pu.longitude?.toFixed(4)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
