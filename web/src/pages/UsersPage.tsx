import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, call, type User, type UserRole } from "../api/client";
import { keys, useMe, useUsersUsage, type StatsPeriod } from "../api/hooks";
import { Badge, Button, ConfirmDialog, Dialog, ErrorBanner, Field, Input, PageHeader, Select, Spinner } from "../components/ui";
import { fmtCost, fmtDateTime, fmtTokens, timeAgo } from "../lib/format";

const PERIODS: { value: StatsPeriod; label: string }[] = [
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
  { value: "90d", label: "Last 90 days" },
  { value: "all", label: "All time" },
];

/** UsersPage lets an admin see every account and add one; there is no sign-up. */
export default function UsersPage() {
  const me = useMe();
  const isAdmin = me.data?.user.role === "admin";
  const [period, setPeriod] = useState<StatsPeriod>("all");
  const usage = useUsersUsage(period, isAdmin);
  const qc = useQueryClient();
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<UserRole>("author");
  const [created, setCreated] = useState<string | null>(null);
  const [editing, setEditing] = useState<User | null>(null);
  const [settingPassword, setSettingPassword] = useState<User | null>(null);
  const [deleting, setDeleting] = useState<User | null>(null);
  const [toggling, setToggling] = useState<User | null>(null);
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["users", "usage"] });
    qc.invalidateQueries({ queryKey: keys.users });
  };
  const toggle = useMutation({
    mutationFn: (u: User) =>
      call(u.disabled_at ? api.POST("/api/users/{userId}/enable", { params: { path: { userId: u.id } } }) : api.POST("/api/users/{userId}/disable", { params: { path: { userId: u.id } } })),
    onSuccess: () => {
      refresh();
      setToggling(null);
    },
  });
  const remove = useMutation({
    mutationFn: (u: User) => call(api.DELETE("/api/users/{userId}", { params: { path: { userId: u.id } }, body: { username: u.username } })),
    onSuccess: () => {
      refresh();
      setDeleting(null);
    },
  });
  const create = useMutation({
    mutationFn: () => call(api.POST("/api/users", { body: { username: username.trim(), display_name: displayName.trim() || undefined, password, role } })),
    onSuccess: (u) => {
      qc.invalidateQueries({ queryKey: keys.users });
      qc.invalidateQueries({ queryKey: ["users", "usage"] });
      setCreated(u.username);
      setUsername("");
      setDisplayName("");
      setPassword("");
      setRole("author");
    },
  });

  if (me.data && !isAdmin) {
    return (
      <div>
        <PageHeader title="Users" />
        <p className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900">Only an admin can see the accounts. Your own account is on the Account page.</p>
      </div>
    );
  }
  return (
    <div>
      <PageHeader title="Users" subtitle="Every account is a private workspace: you can see who has one and what it uses, never their manuscripts." />
      <div className="grid gap-6 lg:grid-cols-[1fr_22rem]">
        <div>
          <div className="mb-3 flex flex-wrap items-end gap-3">
            <label className="block text-sm">
              <span className="mb-1 block font-medium text-stone-700">Usage over</span>
              <Select value={period} onChange={(e) => setPeriod(e.target.value as StatsPeriod)} className="w-44">
                {PERIODS.map((p) => (
                  <option key={p.value} value={p.value}>
                    {p.label}
                  </option>
                ))}
              </Select>
            </label>
            <span className="pb-1.5 text-xs text-stone-500">Projects and chapters are what each account holds now; runs, calls, tokens and cost are for the period.</span>
          </div>
          <ErrorBanner error={usage.error} onRetry={() => usage.refetch()} />
          {usage.isLoading && <Spinner />}
          {usage.data && (
            <div className="overflow-x-auto rounded-lg border border-stone-200 bg-white shadow-sm">
              <table className="w-full text-sm">
                <thead className="bg-stone-50 text-left text-xs uppercase tracking-wide text-stone-500">
                  <tr>
                    <th className="px-3 py-2 font-medium">Account</th>
                    <th className="px-3 py-2 font-medium">Role</th>
                    <th className="px-3 py-2 text-right font-medium">Projects</th>
                    <th className="px-3 py-2 text-right font-medium">Chapters</th>
                    <th className="px-3 py-2 text-right font-medium">Runs</th>
                    <th className="px-3 py-2 text-right font-medium">Calls</th>
                    <th className="px-3 py-2 text-right font-medium">Tokens</th>
                    <th className="px-3 py-2 text-right font-medium">Cost</th>
                    <th className="px-3 py-2 font-medium">Last run</th>
                    <th className="px-3 py-2 font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {usage.data.accounts.map(({ user: u, usage: n, last_run_at }) => (
                    <tr key={u.id} className={`border-t border-stone-100 ${u.disabled_at ? "opacity-60" : ""}`}>
                      <td className="px-3 py-2">
                        <div className="font-medium text-stone-900">
                          {u.username}
                          {u.id === me.data?.user.id && <span className="ml-2 text-xs text-stone-400">(you)</span>}
                        </div>
                        <div className="text-xs text-stone-500">
                          {u.display_name || "—"} · since {fmtDateTime(u.created_at)}
                        </div>
                      </td>
                      <td className="px-3 py-2">
                        <div className="flex flex-wrap gap-1">
                          <Badge tone={u.role === "admin" ? "amber" : "stone"}>{u.role}</Badge>
                          {u.disabled_at && <Badge tone="red">disabled</Badge>}
                        </div>
                      </td>
                      <td className="px-3 py-2 text-right tabular-nums">{n.projects}</td>
                      <td className="px-3 py-2 text-right tabular-nums">{n.chapters}</td>
                      <td className="px-3 py-2 text-right tabular-nums">{n.runs}</td>
                      <td className="px-3 py-2 text-right tabular-nums">{n.model_calls}</td>
                      <td className="px-3 py-2 text-right tabular-nums text-stone-600">
                        {fmtTokens(n.prompt_tokens)} / {fmtTokens(n.completion_tokens)}
                      </td>
                      <td className="px-3 py-2 text-right tabular-nums">{n.model_calls ? fmtCost(n.cost_usd, n.cost_estimated) : "—"}</td>
                      <td className="px-3 py-2 text-stone-500">{last_run_at ? timeAgo(last_run_at) : "—"}</td>
                      <td className="px-3 py-2">
                        <div className="flex flex-wrap gap-1">
                          <Button size="sm" variant="ghost" onClick={() => setEditing(u)}>
                            Edit
                          </Button>
                          {u.id !== me.data?.user.id && (
                            <>
                              <Button size="sm" variant="ghost" onClick={() => setSettingPassword(u)}>
                                Password
                              </Button>
                              <Button size="sm" variant={u.disabled_at ? "ghost" : "danger"} onClick={() => setToggling(u)}>
                                {u.disabled_at ? "Enable" : "Disable"}
                              </Button>
                              {u.disabled_at && (
                                <Button size="sm" variant="danger" onClick={() => setDeleting(u)}>
                                  Delete
                                </Button>
                              )}
                            </>
                          )}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
                <tfoot className="border-t border-stone-200 bg-stone-50 text-xs font-medium text-stone-700">
                  <tr>
                    <td className="px-3 py-2" colSpan={2}>
                      Total · {usage.data.accounts.length} account{usage.data.accounts.length === 1 ? "" : "s"}
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums">{usage.data.totals.projects}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{usage.data.totals.chapters}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{usage.data.totals.runs}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{usage.data.totals.model_calls}</td>
                    <td className="px-3 py-2 text-right tabular-nums">
                      {fmtTokens(usage.data.totals.prompt_tokens)} / {fmtTokens(usage.data.totals.completion_tokens)}
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums">{fmtCost(usage.data.totals.cost_usd, usage.data.totals.cost_estimated)}</td>
                    <td className="px-3 py-2" colSpan={2} />
                  </tr>
                </tfoot>
              </table>
            </div>
          )}
        </div>
        <form
          className="h-fit space-y-3 rounded-lg border border-stone-200 bg-white p-4 shadow-sm"
          onSubmit={(e) => {
            e.preventDefault();
            if (!create.isPending) create.mutate();
          }}
        >
          <h2 className="font-semibold text-stone-900">Add an account</h2>
          <p className="text-xs text-stone-500">The new account starts with its own settings, the example writers and the system agents. Give the person the first password; they can change it on their Account page.</p>
          <Field label="Username" help="Lower-case, 3 to 32 characters: letters, digits, dots, hyphens, underscores.">
            <Input value={username} onChange={(e) => setUsername(e.target.value.toLowerCase())} autoCapitalize="none" required minLength={3} maxLength={32} />
          </Field>
          <Field label="Display name (optional)">
            <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} maxLength={120} />
          </Field>
          <Field label="First password" help="At least 8 characters.">
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" required minLength={8} maxLength={72} />
          </Field>
          <Field label="Role" help="Admins add and manage accounts; authors only see their own workspace.">
            <Select value={role} onChange={(e) => setRole(e.target.value as UserRole)}>
              <option value="author">author</option>
              <option value="admin">admin</option>
            </Select>
          </Field>
          <ErrorBanner error={create.error} />
          {created && <p className="text-sm text-green-700">Account {created} was added.</p>}
          <Button variant="primary" type="submit" loading={create.isPending} disabled={username.trim().length < 3 || password.length < 8}>
            Add account
          </Button>
        </form>
      </div>
      {editing && <EditUserDialog user={editing} self={editing.id === me.data?.user.id} onClose={() => setEditing(null)} onSaved={refresh} />}
      {settingPassword && <SetPasswordDialog user={settingPassword} onClose={() => setSettingPassword(null)} />}
      <ConfirmDialog
        open={toggling !== null}
        title={toggling?.disabled_at ? `Enable ${toggling?.username}?` : `Disable ${toggling?.username}?`}
        message={
          toggling?.disabled_at
            ? "The account can sign in again. Its work was kept all along."
            : "The account is signed out everywhere, its running runs are cancelled, and it cannot sign in until enabled again. Its projects, chapters and writers are kept."
        }
        confirmLabel={toggling?.disabled_at ? "Enable" : "Disable"}
        danger={!toggling?.disabled_at}
        busy={toggle.isPending}
        onConfirm={() => toggling && toggle.mutate(toggling)}
        onClose={() => setToggling(null)}
      />
      <ConfirmDialog
        open={deleting !== null}
        title={`Delete ${deleting?.username}?`}
        message={
          <>
            This removes the account and everything it owns: projects, chapters, story bibles, writers, runs and stats. It cannot be undone.
            <ErrorBanner error={remove.error} />
          </>
        }
        confirmLabel="Delete everything"
        typeToConfirm={deleting?.username}
        busy={remove.isPending}
        onConfirm={() => deleting && remove.mutate(deleting)}
        onClose={() => setDeleting(null)}
      />
      <ErrorBanner error={toggle.error} />
    </div>
  );
}

function EditUserDialog({ user, self, onClose, onSaved }: { user: User; self: boolean; onClose: () => void; onSaved: () => void }) {
  const [displayName, setDisplayName] = useState(user.display_name);
  const [role, setRole] = useState<UserRole>(user.role);
  const save = useMutation({
    mutationFn: () => call(api.PUT("/api/users/{userId}", { params: { path: { userId: user.id } }, body: { display_name: displayName.trim(), role: self ? undefined : role } })),
    onSuccess: () => {
      onSaved();
      onClose();
    },
  });
  return (
    <Dialog open title={`Edit ${user.username}`} onClose={onClose}>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <Field label="Display name">
          <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} maxLength={120} />
        </Field>
        <Field label="Role" help={self ? "Nobody changes their own role; ask another admin." : "One active admin always remains; the last one cannot be demoted."}>
          <Select value={role} onChange={(e) => setRole(e.target.value as UserRole)} disabled={self}>
            <option value="author">author</option>
            <option value="admin">admin</option>
          </Select>
        </Field>
        <ErrorBanner error={save.error} />
        <div className="flex justify-end gap-2">
          <Button onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" loading={save.isPending}>
            Save
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function SetPasswordDialog({ user, onClose }: { user: User; onClose: () => void }) {
  const [password, setPassword] = useState("");
  const [done, setDone] = useState(false);
  const set = useMutation({
    mutationFn: () => call(api.PUT("/api/users/{userId}/password", { params: { path: { userId: user.id } }, body: { new_password: password } })),
    onSuccess: () => setDone(true),
  });
  return (
    <Dialog open title={`Set a new password for ${user.username}`} onClose={onClose}>
      {done ? (
        <div className="space-y-4">
          <p className="text-sm text-stone-700">The password was set and the account was signed out everywhere. Pass the new password on to the person.</p>
          <div className="flex justify-end">
            <Button variant="primary" onClick={onClose}>
              Done
            </Button>
          </div>
        </div>
      ) : (
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            set.mutate();
          }}
        >
          <Field label="New password" help="At least 8 characters. The account is signed out everywhere.">
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" required minLength={8} maxLength={72} autoFocus />
          </Field>
          <ErrorBanner error={set.error} />
          <div className="flex justify-end gap-2">
            <Button onClick={onClose} disabled={set.isPending}>
              Cancel
            </Button>
            <Button variant="primary" type="submit" loading={set.isPending} disabled={password.length < 8}>
              Set password
            </Button>
          </div>
        </form>
      )}
    </Dialog>
  );
}
