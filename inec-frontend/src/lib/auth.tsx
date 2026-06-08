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

function isTokenExpired(token: string): boolean {
  try {
    const payload = JSON.parse(atob(token.split('.')[1]));
    if (!payload.exp) return false;
    return payload.exp * 1000 < Date.now();
  } catch {
    return true;
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  // User info is NOT sensitive — safe in localStorage for display
  const [user, setUser] = useState<User | null>(() => {
    const stored = localStorage.getItem('user');
    return stored ? JSON.parse(stored) : null;
  });
  // Token is stored in httpOnly cookie by server; we keep a copy for display/API-header use
  // but the cookie is the authoritative source for auth
  const [token, setToken] = useState<string | null>(() => {
    const stored = localStorage.getItem('token');
    if (stored && isTokenExpired(stored)) {
      localStorage.removeItem('token');
      localStorage.removeItem('user');
      return null;
    }
    return stored;
  });

  const login = (newToken: string, newUser: User) => {
    // Token also set as httpOnly cookie by server; localStorage copy for API headers
    localStorage.setItem('token', newToken);
    localStorage.setItem('user', JSON.stringify(newUser));
    setToken(newToken);
    setUser(newUser);
  };

  const logout = () => {
    localStorage.removeItem('token');
    localStorage.removeItem('user');
    // Clear httpOnly cookie via server call
    fetch('/auth/logout', { method: 'POST', credentials: 'include' }).catch(() => {});
    setToken(null);
    setUser(null);
  };

  useEffect(() => {
    if (token && !user) {
      logout();
    }
    if (token && isTokenExpired(token)) {
      logout();
    }
  }, [token, user]);

  return (
    <AuthContext.Provider value={{ user, token, login, logout, isAuthenticated: !!token }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  return useContext(AuthContext);
}
