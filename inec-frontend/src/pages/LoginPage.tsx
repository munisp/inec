import { useState } from 'react';
import { useAuth } from '@/lib/auth';
import { api } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Vote } from 'lucide-react';
import DemoQuickAccess from '@/components/DemoQuickAccess';

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
                <Input id="username" name="username" autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} placeholder="Enter username" />
              </div>
              <div className="space-y-2">
                <Label htmlFor="password">Password</Label>
                <Input id="password" name="password" autoComplete="current-password" type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="Enter password" />
              </div>
              <Button type="submit" className="w-full bg-green-700 hover:bg-green-800" disabled={loading}>
                {loading ? 'Signing in...' : 'Sign In'}
              </Button>
            </form>

            {/* Demo quick-login accounts are a DEV-only convenience; they must
                never render in production builds (R4-41). */}
            <DemoQuickAccess enabled={import.meta.env.DEV} onQuickLogin={quickLogin} />
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
