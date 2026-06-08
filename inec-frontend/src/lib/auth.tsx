import { createContext, useContext, useState, useEffect, ReactNode } from 'react';

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
  // Token is stored in httpOnly cookie by server — NOT in localStorage (XSS prevention).
  // We keep a non-null sentinel in React state to track auth status; actual auth is via cookie.
  const [token, setToken] = useState<string | null>(() => {
    return localStorage.getItem('user') ? 'httponly-cookie' : null;
  });

  const login = (_newToken: string, newUser: User) => {
    // Token is in httpOnly cookie set by server — do NOT store in localStorage
    localStorage.setItem('user', JSON.stringify(newUser));
    setToken('httponly-cookie');
    setUser(newUser);
  };

  const logout = () => {
    localStorage.removeItem('user');
    // Clear httpOnly cookie via server call
    const apiUrl = import.meta.env.VITE_API_URL ?? '';
    fetch(`${apiUrl}/auth/logout`, { method: 'POST', credentials: 'include' }).catch(() => {});
    setToken(null);
    setUser(null);
  };

  // Verify session is still valid on mount by checking httpOnly cookie via /auth/me
  useEffect(() => {
    if (token && !user) {
      logout();
      return;
    }
    if (user) {
      const apiUrl = import.meta.env.VITE_API_URL ?? '';
      fetch(`${apiUrl}/auth/me`, { credentials: 'include' })
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
