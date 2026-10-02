import { useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, call, errorMessage, type Writer, type WriterRole, type WriterTestResult } from "../api/client";
import { keys, useGatewayModels, useMe, useWriters } from "../api/hooks";
import { Badge, Button, ConfirmDialog, Dialog, EmptyState, ErrorBanner, Field, Input, PageHeader, Pending, Select, Spinner, Textarea } from "../components/ui";
import { fmtCost, fmtTokens, slugify } from "../lib/format";

type FormState = {
  id?: string;
  slug?: string;
  is_system: boolean;
  name: string;
  model_alias: string;
  aliasTouched: boolean;
  system_prompt: string;
  roles: WriterRole[];
  enabled: boolean;
  temperature: number;
};

const FIXED_SUFFIX = `Guild rules (fixed, always apply)
- Produce original text only. Never reproduce, quote at length or closely paraphrase passages from any published work, including the works of the author whose craft you emulate. Evoke a sensibility; do not copy sentences.
- Always follow the required output format exactly. When JSON is requested, return only valid JSON with no commentary before or after it.`;

export default function WritersPage() {
  const writers = useWriters();
  const me = useMe();
  const qc = useQueryClient();
  const [form, setForm] = useState<FormState | null>(null);
  const [toDelete, setToDelete] = useState<Writer | null>(null);
  const [quickTest, setQuickTest] = useState<{ writer: Writer; result?: WriterTestResult; error?: string } | null>(null);

  const invalidate = () => qc.invalidateQueries({ queryKey: keys.writers });

  const toggle = useMutation({
    mutationFn: (w: Writer) =>
      call(
        api.PUT("/api/writers/{writerId}", {
          params: { path: { writerId: w.id } },
          body: { name: w.name, model_alias: w.model_alias, system_prompt: w.system_prompt, roles: w.roles, enabled: !w.enabled, temperature: w.temperature },
        }),
      ),
    onSuccess: invalidate,
  });

  const duplicate = useMutation({
    mutationFn: (w: Writer) => call(api.POST("/api/writers/{writerId}/duplicate", { params: { path: { writerId: w.id } } })),
    onSuccess: invalidate,
  });

  const remove = useMutation({
    mutationFn: (w: Writer) => call(api.DELETE("/api/writers/{writerId}", { params: { path: { writerId: w.id } } })),
    onSuccess: () => {
      invalidate();
      setToDelete(null);
    },
  });

  const runQuickTest = useMutation({
    mutationFn: (w: Writer) =>
      call(api.POST("/api/writers/test", { body: { writer_id: w.id, name: w.name, model_alias: w.model_alias, system_prompt: w.system_prompt, temperature: w.temperature } })),
    onMutate: (w) => setQuickTest({ writer: w }),
    onSuccess: (result, w) => setQuickTest({ writer: w, result }),
    onError: (e, w) => setQuickTest({ writer: w, error: errorMessage(e) }),
  });

  const openNew = () =>
    setForm({ is_system: false, name: "", model_alias: "", aliasTouched: false, system_prompt: "", roles: ["critic", "co-writer"], enabled: true, temperature: 0.7 });
  const openEdit = (w: Writer) =>
    setForm({
      id: w.id,
      slug: w.slug,
      is_system: w.is_system,
      name: w.name,
      model_alias: w.model_alias,
      aliasTouched: true,
      system_prompt: w.system_prompt,
      roles: w.roles,
      enabled: w.enabled,
      temperature: w.temperature,
    });

  const guild = writers.data?.filter((w) => !w.is_system) ?? [];
  const system = writers.data?.filter((w) => w.is_system) ?? [];

  return (
    <div>
      <PageHeader
        title="Writers"
        subtitle="Each writer is a voice and a craft sensibility. Critics attend the Guild; co-writers draft with you. The system agents run the editorial work."
        actions={
          <Button variant="primary" onClick={openNew}>
            Add writer
          </Button>
        }
      />
      <ErrorBanner error={writers.error || toggle.error || duplicate.error} onRetry={writers.error ? () => writers.refetch() : undefined} />
      {writers.isPending && <Pending paused={writers.isPaused} />}
      {writers.data && guild.length === 0 && <EmptyState title="No writers yet">Add one, or duplicate a system agent's settings to start.</EmptyState>}

      <div className="grid gap-3 md:grid-cols-2">
        {guild.map((w) => (
          <WriterCard key={w.id} w={w} onEdit={openEdit} onToggle={(x) => toggle.mutate(x)} onDuplicate={(x) => duplicate.mutate(x)} onDelete={setToDelete} onTest={(x) => runQuickTest.mutate(x)} />
        ))}
      </div>

      <h2 className="mb-2 mt-8 text-lg font-semibold text-stone-900">System agents</h2>
      <p className="mb-3 text-sm text-stone-500">Fixed roles with editable prompts and aliases. The editor-in-chief merges critiques, the lead writer applies accepted fixes, the bible keeper proposes story bible updates.</p>
      <div className="grid gap-3 md:grid-cols-3">
        {system.map((w) => (
          <WriterCard key={w.id} w={w} onEdit={openEdit} onToggle={(x) => toggle.mutate(x)} onDuplicate={(x) => duplicate.mutate(x)} onDelete={setToDelete} onTest={(x) => runQuickTest.mutate(x)} />
        ))}
      </div>

      <Dialog open={form !== null} title={form?.id ? `Edit ${form.name}` : "Add writer"} onClose={() => setForm(null)} wide>
        {form && me.data && (
          <WriterForm
            form={form}
            aliasPrefix={me.data.alias_prefix}
            defaultAlias={me.data.default_model_alias}
            onChange={setForm}
            onSaved={() => {
              invalidate();
              setForm(null);
            }}
            onCancel={() => setForm(null)}
          />
        )}
      </Dialog>

      <Dialog open={quickTest !== null} title={`Test: ${quickTest?.writer.name ?? ""}`} onClose={() => setQuickTest(null)}>
        {quickTest && (
          <div>
            {!quickTest.result && !quickTest.error && (
              <div className="flex items-center gap-2 text-sm text-stone-600">
                <Spinner /> Asking the gateway…
              </div>
            )}
            {quickTest.error && <ErrorBanner error={quickTest.error} />}
            {quickTest.result && <TestResultView r={quickTest.result} />}
          </div>
        )}
      </Dialog>

      <ConfirmDialog
        open={toDelete !== null}
        title="Delete this writer?"
        message={
          <p>
            <strong>{toDelete?.name}</strong> will be removed. Past runs keep their cost, but their statistics will no longer show this writer.
          </p>
        }
        confirmLabel="Delete writer"
        busy={remove.isPending}
        onConfirm={() => toDelete && remove.mutate(toDelete)}
        onClose={() => setToDelete(null)}
      />
    </div>
  );
}

function WriterCard({
  w,
  onEdit,
  onToggle,
  onDuplicate,
  onDelete,
  onTest,
}: {
  w: Writer;
  onEdit: (w: Writer) => void;
  onToggle: (w: Writer) => void;
  onDuplicate: (w: Writer) => void;
  onDelete: (w: Writer) => void;
  onTest: (w: Writer) => void;
}) {
  return (
    <div className={`rounded-lg border bg-white p-4 shadow-sm ${w.enabled ? "border-stone-200" : "border-stone-200 opacity-60"}`}>
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="truncate font-semibold text-stone-900">{w.name}</span>
            {w.is_system && <Badge tone="blue">system</Badge>}
            {!w.enabled && <Badge tone="red">disabled</Badge>}
          </div>
          <div className="mt-0.5 truncate font-mono text-xs text-stone-500" title={w.model_alias}>
            {w.slug} → {w.model_alias}
          </div>
        </div>
        <label className="flex shrink-0 items-center gap-1.5 text-xs text-stone-600">
          <input type="checkbox" checked={w.enabled} onChange={() => onToggle(w)} /> enabled
        </label>
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-1.5">
        {w.roles.map((r) => (
          <Badge key={r} tone={r === "critic" ? "amber" : "green"}>
            {r}
          </Badge>
        ))}
        <span className="text-xs text-stone-500">temperature {w.temperature}</span>
      </div>
      <p className="mt-2 line-clamp-3 text-sm text-stone-600">{w.system_prompt || <span className="italic text-stone-400">No prompt yet.</span>}</p>
      <div className="mt-3 flex flex-wrap gap-1">
        <Button size="sm" onClick={() => onEdit(w)}>
          Edit
        </Button>
        <Button size="sm" onClick={() => onTest(w)}>
          Test writer
        </Button>
        {!w.is_system && (
          <>
            <Button size="sm" variant="ghost" onClick={() => onDuplicate(w)}>
              Duplicate
            </Button>
            <Button size="sm" variant="ghost" onClick={() => onDelete(w)}>
              Delete
            </Button>
          </>
        )}
      </div>
    </div>
  );
}

function WriterForm({
  form,
  aliasPrefix,
  defaultAlias,
  onChange,
  onSaved,
  onCancel,
}: {
  form: FormState;
  aliasPrefix: string;
  defaultAlias: string;
  onChange: (f: FormState) => void;
  onSaved: () => void;
  onCancel: () => void;
}) {
  const models = useGatewayModels();
  const [testResult, setTestResult] = useState<WriterTestResult | null>(null);
  const slug = form.slug ?? slugify(form.name);
  const suggestedAlias = useMemo(() => (form.id ? form.model_alias : `${aliasPrefix}${slug}`), [aliasPrefix, slug, form.id, form.model_alias]);

  // Pre-fill the alias from the convention until the author edits it.
  useEffect(() => {
    if (!form.aliasTouched && !form.id && form.model_alias !== suggestedAlias) {
      onChange({ ...form, model_alias: suggestedAlias });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [suggestedAlias]);

  const body = () => ({
    name: form.name,
    model_alias: form.model_alias,
    system_prompt: form.system_prompt,
    roles: form.roles,
    enabled: form.enabled,
    temperature: form.temperature,
  });

  const save = useMutation({
    mutationFn: () =>
      form.id
        ? call(api.PUT("/api/writers/{writerId}", { params: { path: { writerId: form.id } }, body: body() }))
        : call(api.POST("/api/writers", { body: body() })),
    onSuccess: onSaved,
  });

  const test = useMutation({
    mutationFn: () =>
      call(
        api.POST("/api/writers/test", {
          body: { writer_id: form.id, name: form.name || "Unnamed writer", model_alias: form.model_alias, system_prompt: form.system_prompt, temperature: form.temperature },
        }),
      ),
    onSuccess: setTestResult,
  });

  const toggleRole = (r: WriterRole) =>
    onChange({ ...form, roles: form.roles.includes(r) ? form.roles.filter((x) => x !== r) : [...form.roles, r] });

  return (
    <form
      className="space-y-4"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <div className="grid gap-4 md:grid-cols-2">
        <Field label="Name" help={form.id ? `Slug: ${slug} (fixed)` : `Slug will be: ${slug}`}>
          <Input value={form.name} onChange={(e) => onChange({ ...form, name: e.target.value })} required maxLength={120} autoFocus placeholder="e.g. Virginia Woolf" />
        </Field>
        <Field
          label="Gateway model alias"
          help={
            defaultAlias
              ? `Convention: ${aliasPrefix}{slug}. Your gateway also offers "${defaultAlias}" as a default.`
              : `Convention: ${aliasPrefix}{slug}. The alias must exist on the gateway.`
          }
        >
          <Input value={form.model_alias} onChange={(e) => onChange({ ...form, model_alias: e.target.value, aliasTouched: true })} required maxLength={200} className="font-mono" />
        </Field>
      </div>
      <Field
        label="Aliases on the gateway"
        help={
          models.error
            ? `Could not list models: ${errorMessage(models.error)}`
            : models.data && models.data.aliases.length === 0
              ? `The gateway lists ${models.data.all_models.length} models but none start with "${aliasPrefix}". Showing all of them.`
              : "Pick one to fill the alias field."
        }
      >
        <Select
          value=""
          disabled={!models.data}
          onChange={(e) => {
            if (e.target.value) onChange({ ...form, model_alias: e.target.value, aliasTouched: true });
          }}
        >
          <option value="">{models.isLoading ? "Loading…" : "Choose an alias"}</option>
          {(models.data?.aliases.length ? models.data.aliases : models.data?.all_models ?? []).map((m) => (
            <option key={m} value={m}>
              {m}
            </option>
          ))}
        </Select>
      </Field>
      <Field label="System prompt" help="Describe the author whose voice and craft sensibility this writer should emulate. Yours to write.">
        <Textarea rows={6} value={form.system_prompt} onChange={(e) => onChange({ ...form, system_prompt: e.target.value })} maxLength={20000} />
      </Field>
      <div className="rounded-md border border-stone-200 bg-stone-50 p-3 text-xs text-stone-600">
        <div className="mb-1 font-medium text-stone-700">Appended to every prompt (not editable)</div>
        <pre className="whitespace-pre-wrap font-sans">{FIXED_SUFFIX}</pre>
      </div>
      <div className="grid gap-4 md:grid-cols-3">
        {!form.is_system && (
          <div>
            <span className="mb-1 block text-sm font-medium text-stone-700">Roles</span>
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={form.roles.includes("critic")} onChange={() => toggleRole("critic")} /> Critic (attends the Guild)
            </label>
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={form.roles.includes("co-writer")} onChange={() => toggleRole("co-writer")} /> Co-writer (drafts with you)
            </label>
          </div>
        )}
        <Field label={`Temperature: ${form.temperature.toFixed(2)}`} help="Lower is steadier, higher is wilder.">
          <input type="range" min={0} max={2} step={0.05} value={form.temperature} onChange={(e) => onChange({ ...form, temperature: Number(e.target.value) })} className="w-full" />
        </Field>
        <div>
          <span className="mb-1 block text-sm font-medium text-stone-700">Status</span>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={form.enabled} onChange={(e) => onChange({ ...form, enabled: e.target.checked })} /> Enabled
          </label>
        </div>
      </div>

      <div className="rounded-md border border-stone-200 p-3">
        <div className="flex items-center justify-between gap-3">
          <div className="text-sm text-stone-700">Send a short sample request through the gateway with this alias and prompt.</div>
          <Button onClick={() => test.mutate()} loading={test.isPending} disabled={!form.model_alias.trim()}>
            Test writer
          </Button>
        </div>
        <div className="mt-2">
          <ErrorBanner error={test.error} />
          {testResult && <TestResultView r={testResult} />}
        </div>
      </div>

      <ErrorBanner error={save.error} />
      <div className="flex justify-end gap-2">
        <Button onClick={onCancel}>Cancel</Button>
        <Button variant="primary" type="submit" loading={save.isPending} disabled={!form.name.trim() || !form.model_alias.trim()}>
          {form.id ? "Save changes" : "Add writer"}
        </Button>
      </div>
    </form>
  );
}

function TestResultView({ r }: { r: WriterTestResult }) {
  return (
    <div className={`rounded-md border p-3 text-sm ${r.ok ? "border-green-200 bg-green-50" : "border-red-200 bg-red-50"}`}>
      {r.ok ? <p className="whitespace-pre-wrap text-stone-800">{r.reply}</p> : <p className="text-red-800">{r.error}</p>}
      <div className="mt-2 text-xs text-stone-500">
        <span className="font-mono">{r.model_alias}</span> · {fmtTokens(r.prompt_tokens)} prompt / {fmtTokens(r.completion_tokens)} completion tokens ·{" "}
        {fmtCost(r.cost_usd, r.cost_estimated, r.cost_known)} · {r.latency_ms} ms
      </div>
    </div>
  );
}
