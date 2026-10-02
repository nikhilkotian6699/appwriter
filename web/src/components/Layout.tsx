import { useEffect } from "react";
import { NavLink, Outlet } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, call } from "../api/client";
import { keys, useMe } from "../api/hooks";
import { LoginPage } from "../pages/LoginPage";
import { Button, ErrorBanner, Pending } from "./ui";

const navClass = ({ isActive }: { isActive: boolean }) =>
  `rounded-md px-3 py-1.5 text-sm font-medium ${isActive ? "bg-stone-900 text-white" : "text-stone-700 hover:bg-stone-200"}`;

export function Layout() {
  const me = useMe();
  const qc = useQueryClient();
  // Any request answered "sign in to continue" sends the app back to the login page.
  useEffect(() => {
    const onUnauthorized = () => {
      qc.setQueryData(keys.me, undefined);
      qc.invalidateQueries({ queryKey: keys.me });
    };
    window.addEventListener("wg:unauthorized", onUnauthorized);
    return () => window.removeEventListener("wg:unauthorized", onUnauthorized);
  }, [qc]);
  const logout = useMutation({
    mutationFn: () => call(api.POST("/api/auth/logout")),
    onSettled: () => {
      qc.clear();
      qc.invalidateQueries({ queryKey: keys.me });
    },
  });

  if (me.isError && me.error instanceof ApiError && me.error.status === 401) {
    return <LoginPage />;
  }
  if (!me.data && !me.isError) {
    // First load, or a request paused while the browser is offline.
    return (
      <div className="flex min-h-full items-center justify-center">
        <Pending paused={me.isPaused} />
      </div>
    );
  }
  return (
    <div className="flex min-h-full flex-col">
      <header className="border-b border-stone-200 bg-white">
        <div className="mx-auto flex max-w-6xl items-center justify-between gap-4 px-4 py-2.5">
          <div className="flex items-center gap-6">
            <NavLink to="/projects" className="flex items-center gap-2 text-base font-semibold tracking-tight text-stone-900">
              <span className="inline-flex h-7 w-7 items-center justify-center rounded-md bg-stone-900 font-serif text-amber-400">W</span>
              Writers' Guild
            </NavLink>
            <nav className="flex items-center gap-1">
              <NavLink to="/projects" className={navClass}>
                Projects
              </NavLink>
              <NavLink to="/writers" className={navClass} end>
                Writers
              </NavLink>
              <NavLink to="/writers/stats" className={navClass}>
                Stats
              </NavLink>
              <NavLink to="/settings" className={navClass}>
                Settings
              </NavLink>
              {me.data?.user.role === "admin" && (
                <NavLink to="/users" className={navClass}>
                  Users
                </NavLink>
              )}
            </nav>
          </div>
          <div className="flex items-center gap-4 text-sm">
            <a href="/guide/" target="_blank" rel="noopener" className="font-medium text-stone-700 hover:text-stone-900">
              Guide ↗
            </a>
            {me.data && (
              <NavLink to="/account" className="text-stone-600 hover:text-stone-900 hover:underline" title={`${me.data.user.role} · your account`}>
                {me.data.user.display_name || me.data.user.username}
              </NavLink>
            )}
            <Button size="sm" variant="ghost" onClick={() => logout.mutate()} loading={logout.isPending} title="Sign out of this browser">
              Sign out
            </Button>
          </div>
        </div>
      </header>
      <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-6">
        {me.isError ? <ErrorBanner error={me.error} onRetry={() => me.refetch()} /> : <Outlet />}
      </main>
    </div>
  );
}
