'use client';

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { browserSend, fetchSession, setCsrfToken } from '../api/browser';
import type { CurrentUser, SessionView } from '../api/types';

export interface SessionContextValue {
  user: CurrentUser | null;
  ready: boolean;
  /** 重新读取会话与 CSRF；登录、退出、改密后调用。 */
  refresh: () => Promise<SessionView | null>;
  logout: () => Promise<void>;
}

const SessionContext = createContext<SessionContextValue | null>(null);

export function SessionProvider({
  initial,
  children,
}: {
  initial?: SessionView | null;
  children: React.ReactNode;
}) {
  const [user, setUser] = useState<CurrentUser | null>(initial?.user ?? null);
  const [ready, setReady] = useState(false);

  const refresh = useCallback(async (): Promise<SessionView | null> => {
    try {
      const session = await fetchSession();
      setCsrfToken(session.csrfToken);
      setUser(session.user);
      return session;
    } catch {
      return null;
    } finally {
      setReady(true);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const logout = useCallback(async () => {
    try {
      await browserSend('/auth/logout', { method: 'POST' });
    } finally {
      setCsrfToken(null);
      setUser(null);
      await refresh();
    }
  }, [refresh]);

  const value = useMemo<SessionContextValue>(
    () => ({ user, ready, refresh, logout }),
    [user, ready, refresh, logout],
  );
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession(): SessionContextValue {
  const ctx = useContext(SessionContext);
  if (!ctx) {
    throw new Error('useSession 必须在 SessionProvider 内使用');
  }
  return ctx;
}
