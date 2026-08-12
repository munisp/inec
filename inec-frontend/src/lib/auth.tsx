import { createContext, useContext, useState, useEffect, ReactNode } from 'react';
import { SESSION_EXPIRED_EVENT } from '@/lib/api';
import { logger } from '@/lib/utils';

interface User {
  id: number;
  username: string;
  full_name: string;
  role: string;
  staff_id?: string;
  state_code?: string;
}

interface AuthContextType {
  user: User | null;
  token: string | null;
  login: (token: string, user: User) => void;
  logout: () => void;
  isAuthenticated: boolean;
}

const AuthContext = createContext<AuthContextType>({
  user: null,
  token: null,
  login: () => {},
  logout: () => {},
  isAuthenticated: false,
});

export function AuthProvider({ children }: { children: ReactNode }) {
  // User info is NOT sensitive — safe in localStorage for display
  const [user, setUser] = useState<User | null>(() => {
    const stored = localStorage.getItem('user');
    return stored ? JSON.parse(stored) : null;
  });
  // Token storage note: the primary session is the httpOnly cookie set by the
  // backend (marker 'httponly-cookie'). The localStorage 'auth_token' JWT is
  // kept ONLY as a documented fallback for non-browser clients and for Bearer
  // headers on cross-origin dev setups — it is never written to IndexedDB or
  // the service-worker offline queue.
  const [token, setToken] = useState<string | null>(() => {
    return localStorage.getItem('auth_token') || (localStorage.getItem('user') ? 'httponly-cookie' : null);
  });

  const login = (newToken: string, newUser: User) => {
    localStorage.setItem('user', JSON.stringify(newUser));
    localStorage.setItem('auth_token', newToken);
    setToken(newToken);
    setUser(newUser);
  };

  const logout = () => {
    localStorage.removeItem('user');
    localStorage.removeItem('auth_token');
    const apiUrl = import.meta.env.VITE_API_URL ?? '';
    fetch(`${apiUrl}/auth/logout`, { method: 'POST', credentials: 'include' }).catch(err => logger.error("logout request failed:", err));
    setToken(null);
    setUser(null);
  };

  // End the session when the API layer reports an unrecoverable 401:
  // clear state and route to /login carrying the current path for return.
  useEffect(() => {
    const onSessionExpired = () => {
      const returnTo = window.location.hash.replace(/^#\/?/, '') || 'dashboard';
      logout();
      window.location.hash = `/login?returnTo=${encodeURIComponent(returnTo)}`;
    };
    window.addEventListener(SESSION_EXPIRED_EVENT, onSessionExpired);
    return () => window.removeEventListener(SESSION_EXPIRED_EVENT, onSessionExpired);
  }, []);  // eslint-disable-line react-hooks/exhaustive-deps

  // Verify session is still valid on mount
  useEffect(() => {
    if (token && !user) {
      logout();
      return;
    }
    if (user && token) {
      const apiUrl = import.meta.env.VITE_API_URL ?? '';
      const headers: Record<string, string> = {};
      if (token !== 'httponly-cookie') headers['Authorization'] = `Bearer ${token}`;
      fetch(`${apiUrl}/auth/me`, { credentials: 'include', headers })
        .then(res => { if (!res.ok) logout(); })
        .catch(() => { /* network error — keep session, will retry on next request */ });
    }
  }, []);  // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <AuthContext.Provider value={{ user, token, login, logout, isAuthenticated: !!token }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  return useContext(AuthContext);
}
