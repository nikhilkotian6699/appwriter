import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, call } from "../api/client";
import { keys } from "../api/hooks";
import { Button, ErrorBanner, Field, Input } from "../components/ui";

/** LoginPage asks for the username and password; there is no sign-up. */
export function LoginPage() {
  const qc = useQueryClient();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const login = useMutation({
    mutationFn: () => call(api.POST("/api/auth/login", { body: { username: username.trim(), password } })),
    onSuccess: (me) => {
      qc.setQueryData(keys.me, me);
      qc.invalidateQueries();
    },
  });
  const err = login.error;
  const locked = err instanceof ApiError && err.status === 429;
  return (
    <div className="flex min-h-full flex-col items-center justify-center bg-stone-100 px-4 py-12">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex items-center justify-center gap-2 text-xl font-semibold tracking-tight text-stone-900">
          <span className="inline-flex h-9 w-9 items-center justify-center rounded-md bg-stone-900 font-serif text-amber-400">W</span>
          Writers' Guild
        </div>
        <form
          className="space-y-4 rounded-lg border border-stone-200 bg-white p-6 shadow-sm"
          onSubmit={(e) => {
            e.preventDefault();
            if (!login.isPending) login.mutate();
          }}
        >
          <h1 className="text-lg font-semibold text-stone-900">Sign in</h1>
          <Field label="Username">
            <Input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" autoCapitalize="none" autoFocus required maxLength={64} />
          </Field>
          <Field label="Password">
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required maxLength={256} />
          </Field>
          {err !== null && (
            <div className={locked ? "rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900" : ""}>
              {locked ? <span>{(err as ApiError).message}</span> : <ErrorBanner error={err} />}
            </div>
          )}
          <Button variant="primary" type="submit" loading={login.isPending} className="w-full justify-center" disabled={!username.trim() || !password}>
            Sign in
          </Button>
          <p className="text-center text-xs text-stone-500">
            No account? The admin adds accounts on the Users page. New here?{" "}
            <a href="/guide/" target="_blank" rel="noopener" className="font-medium text-stone-700 underline">
              Read the guide
            </a>
            .
          </p>
        </form>
      </div>
    </div>
  );
}
