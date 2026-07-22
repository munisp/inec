import { useState } from 'react';
import { useAuth } from '@/lib/auth';
import { api } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Vote, Shield, Eye } from 'lucide-react';

export default function LoginPage() {
  const { login } = useAuth();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      const res = await api.login(username, password);
      login(res.access_token, res.user);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Login failed');
    } finally {
      setLoading(false);
    }
  };

  const quickLogin = async (user: string, pass: string) => {
    setUsername(user);
    setPassword(pass);
    setError('');
    setLoading(true);
    try {
      const res = await api.login(user, pass);
      login(res.access_token, res.user);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Login failed');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen bg-gradient-to-br from-green-900 via-green-800 to-green-950 flex items-center justify-center p-4">
      <div className="w-full max-w-md space-y-6">
        <div className="text-center space-y-2">
          <div className="inline-flex items-center justify-center w-16 h-16 rounded-2xl bg-white/10 backdrop-blur mb-2">
            <Vote className="w-8 h-8 text-white" />
          </div>
          <h1 className="text-2xl font-bold text-white">INEC Election Platform</h1>
          <p className="text-green-200 text-sm">Blockchain-Based Election Results System v4.0</p>
        </div>

        <Card className="border-0 shadow-2xl">
          <CardHeader className="pb-4">
            <CardTitle className="text-lg">Sign In</CardTitle>
            <CardDescription>Access the election management platform</CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleLogin} className="space-y-4">
              {error && (
                <div className="p-3 text-sm text-red-700 bg-red-50 rounded-lg border border-red-200">{error}</div>
              )}
              <div className="space-y-2">
                <Label htmlFor="username">Username</Label>
                <Input id="username" name="username" value={username} onChange={(e) => setUsername(e.target.value)} placeholder="Enter username" />
              </div>
              <div className="space-y-2">
                <Label htmlFor="password">Password</Label>
                <Input id="password" name="password" type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="Enter password" />
              </div>
              <Button type="submit" className="w-full bg-green-700 hover:bg-green-800" disabled={loading}>
                {loading ? 'Signing in...' : 'Sign In'}
              </Button>
            </form>

            <div className="mt-6 pt-4 border-t border-zinc-200">
              <p className="text-xs text-zinc-500 mb-3">Quick access (demo accounts):</p>
              <div className="space-y-2">
                <button onClick={() => quickLogin('admin', 'admin123')}
                  className="w-full flex items-center gap-3 p-2.5 rounded-lg border border-zinc-200 hover:bg-zinc-50 transition-colors text-left">
                  <div className="w-8 h-8 rounded-lg bg-green-100 flex items-center justify-center">
                    <Shield className="w-4 h-4 text-green-700" />
                  </div>
                  <div>
                    <p className="text-sm font-medium text-zinc-900">Administrator</p>
                    <p className="text-xs text-zinc-500">Full system access</p>
                  </div>
                </button>
                <button onClick={() => quickLogin('officer1', 'officer123')}
                  className="w-full flex items-center gap-3 p-2.5 rounded-lg border border-zinc-200 hover:bg-zinc-50 transition-colors text-left">
                  <div className="w-8 h-8 rounded-lg bg-blue-100 flex items-center justify-center">
                    <Vote className="w-4 h-4 text-blue-700" />
                  </div>
                  <div>
                    <p className="text-sm font-medium text-zinc-900">Presiding Officer</p>
                    <p className="text-xs text-zinc-500">Upload & manage results</p>
                  </div>
                </button>
                <button onClick={() => quickLogin('observer', 'observer123')}
                  className="w-full flex items-center gap-3 p-2.5 rounded-lg border border-zinc-200 hover:bg-zinc-50 transition-colors text-left">
                  <div className="w-8 h-8 rounded-lg bg-amber-100 flex items-center justify-center">
                    <Eye className="w-4 h-4 text-amber-700" />
                  </div>
                  <div>
                    <p className="text-sm font-medium text-zinc-900">Election Observer</p>
                    <p className="text-xs text-zinc-500">View & verify results</p>
                  </div>
                </button>
              </div>
            </div>
          </CardContent>
        </Card>

        <div className="text-center space-y-1">
          <p className="text-green-300 text-xs">Independent National Electoral Commission</p>
          <p className="text-green-400/60 text-xs">Federal Republic of Nigeria</p>
        </div>
      </div>
    </div>
  );
}
