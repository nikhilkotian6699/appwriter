import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, call, type Me } from "../api/client";
import { keys, useMe } from "../api/hooks";
import { Badge, Button, ErrorBanner, Field, Input, PageHeader, Pending } from "../components/ui";

/** AccountPage changes the signed-in account's display name and password. */
export default function AccountPage() {
  const me = useMe();
  const qc = useQueryClient();
  const [displayName, setDisplayName] = useState("");
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [savedName, setSavedName] = useState(false);
  const [changed, setChanged] = useState(false);
  useEffect(() => {
    if (me.data) setDisplayName(me.data.user.display_name);
  }, [me.data]);

  const rename = useMutation({
    mutationFn: () => call(api.PUT("/api/account", { body: { display_name: displayName.trim() } })),
    onSuccess: (user) => {
      qc.setQueryData(keys.me, (old: Me | undefined) => (old ? { ...old, user } : old));
      setSavedName(true);
      setTimeout(() => setSavedName(false), 2000);
    },
  });
  const changePassword = useMutation({
    mutationFn: () => call(api.PUT("/api/account/password", { body: { current_password: current, new_password: next } })),
    onSuccess: () => {
      setCurrent("");
      setNext("");
      setConfirm("");
      setChanged(true);
    },
  });
  const mismatch = confirm !== "" && confirm !== next;

  if (!me.data) return me.error ? <ErrorBanner error={me.error} onRetry={() => me.refetch()} /> : <Pending paused={me.isPaused} />;
  const u = me.data.user;
  return (
    <div className="max-w-xl">
      <PageHeader
        title="Account"
        subtitle={
          <span className="flex items-center gap-2">
            Signed in as <span className="font-medium text-stone-800">{u.username}</span>
            <Badge tone={u.role === "admin" ? "amber" : "stone"}>{u.role}</Badge>
          </span>
        }
      />
      <form
        className="space-y-4 rounded-lg border border-stone-200 bg-white p-5 shadow-sm"
        onSubmit={(e) => {
          e.preventDefault();
          rename.mutate();
        }}
      >
        <Field label="Display name" help="Shown in the header instead of the username. Leave empty to show the username.">
          <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} maxLength={120} />
        </Field>
        <ErrorBanner error={rename.error} />
        <div className="flex items-center gap-3">
          <Button variant="primary" type="submit" loading={rename.isPending} disabled={displayName.trim() === u.display_name}>
            Save name
          </Button>
          {savedName && <span className="text-sm text-green-700">Saved.</span>}
        </div>
      </form>
      <form
        className="mt-6 space-y-4 rounded-lg border border-stone-200 bg-white p-5 shadow-sm"
        onSubmit={(e) => {
          e.preventDefault();
          if (!mismatch) changePassword.mutate();
        }}
      >
        <h2 className="font-semibold text-stone-900">Change password</h2>
        <p className="text-xs text-stone-500">At least 8 characters. Changing it signs you out everywhere else; this browser stays signed in.</p>
        <Field label="Current password">
          <Input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" required />
        </Field>
        <Field label="New password">
          <Input type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" required minLength={8} maxLength={72} />
        </Field>
        <Field label="Repeat the new password" error={mismatch ? "The two passwords differ." : undefined}>
          <Input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} autoComplete="new-password" required />
        </Field>
        <ErrorBanner error={changePassword.error} />
        <div className="flex items-center gap-3">
          <Button variant="primary" type="submit" loading={changePassword.isPending} disabled={!current || next.length < 8 || confirm !== next}>
            Change password
          </Button>
          {changed && <span className="text-sm text-green-700">Password changed. Other sessions were signed out.</span>}
        </div>
      </form>
    </div>
  );
}
