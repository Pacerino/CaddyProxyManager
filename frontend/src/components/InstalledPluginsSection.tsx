import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { api, unwrap, type ApiEnvelope } from "@/lib/api";
import type {
  InstalledPlugin,
  PluginOperation,
  PluginsState,
  PluginUsage,
} from "@/lib/types";
import {
  AddPluginDialog,
  CADDY_UPDATE_NOTE,
  RESTART_NOTE,
} from "@/components/AddPluginDialog";
import { PluginOperationProgress, isFinished } from "@/components/PluginOperationProgress";

interface ApiError {
  response?: {
    status?: number;
    data?: { error?: { message?: string; invalid?: unknown } };
  };
}

interface Props {
  // Called after an operation finished so the page can refresh its data
  // (module list, DNS provider types).
  onChanged?: () => void;
}

const usageLabel = (u: PluginUsage) => {
  switch (u.kind) {
    case "dnsProvider":
      return `Used by DNS provider '${u.name}'`;
    case "hostPlugin":
      return `Used by host plugin '${u.name}'`;
    case "moduleConfig":
      return `Used by module configuration '${u.name}'`;
    default:
      return `Used by '${u.name}'`;
  }
};

const failureLabel = (op: PluginOperation) => {
  const outcome = op.step === "rolledBack" ? "was rolled back" : "failed";
  switch (op.kind) {
    case "fallback":
      return "Caddy switched back to the shipped binary";
    case "update":
      return `The last update ${outcome}`;
    case "remove":
      return `Removing ${op.package} ${outcome}`;
    default:
      return `Installing ${op.package} ${outcome}`;
  }
};

const repoURL = (path: string) => `https://${path.split("/").slice(0, 3).join("/")}`;

// InstalledPluginsSection lists the plugins built into Caddy and lets the
// user add, remove and update them. It needs Caddy API mode.
export function InstalledPluginsSection({ onChanged }: Props) {
  const [state, setState] = useState<PluginsState | null>(null);
  const [unavailable, setUnavailable] = useState(false);
  const [operation, setOperation] = useState<PluginOperation | null>(null);
  const [addOpen, setAddOpen] = useState(false);
  const [removing, setRemoving] = useState<InstalledPlugin | null>(null);
  const [usages, setUsages] = useState<PluginUsage[]>([]);
  const [submitting, setSubmitting] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await api.get<ApiEnvelope<PluginsState>>("/caddy/plugins");
      const s = unwrap(res.data);
      setState(s);
      setUnavailable(false);
      // An operation already running on page load shows its progress.
      setOperation((cur) => cur ?? s.operation ?? null);
    } catch (err) {
      setState(null);
      // 400 is the API's answer in Caddyfile mode.
      if ((err as ApiError).response?.status === 400) setUnavailable(true);
      else toast.error("Failed to load plugins");
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const finished = useCallback(
    (op: PluginOperation) => {
      setOperation(op);
      load();
      onChanged?.();
    },
    [load, onChanged]
  );

  const running = operation !== null && !isFinished(operation);

  const startError = (err: unknown, fallback: string) => {
    const e = err as ApiError;
    toast.error(
      e.response?.status === 409
        ? "Another plugin operation is already running"
        : e.response?.data?.error?.message ?? fallback
    );
  };

  const remove = async () => {
    if (!removing) return;
    setSubmitting(true);
    try {
      const res = await api.delete<ApiEnvelope<PluginOperation>>(
        "/caddy/plugins",
        { params: { path: removing.path } }
      );
      setRemoving(null);
      setOperation(unwrap(res.data));
    } catch (err) {
      const e = err as ApiError;
      const invalid = e.response?.data?.error?.invalid as PluginUsage[] | undefined;
      if (e.response?.status === 409 && invalid?.length) setUsages(invalid);
      else {
        setRemoving(null);
        startError(err, "Failed to remove plugin");
      }
    } finally {
      setSubmitting(false);
    }
  };

  const update = async () => {
    try {
      const res = await api.post<ApiEnvelope<PluginOperation>>(
        "/caddy/plugins/update"
      );
      setOperation(unwrap(res.data));
    } catch (err) {
      startError(err, "Failed to start update");
    }
  };

  const setAutoUpdate = async (autoUpdate: boolean) => {
    try {
      await api.put("/caddy/plugins/settings", { autoUpdate });
      setState((s) => (s ? { ...s, autoUpdate } : s));
    } catch {
      toast.error("Failed to save setting");
    }
  };

  const canChange = !!state?.installEnabled;
  const setup = state?.setup;
  const lastFailed =
    state?.lastUpdate &&
    (state.lastUpdate.step === "failed" || state.lastUpdate.step === "rolledBack")
      ? state.lastUpdate
      : null;
  // The last failed change of any kind, unless the update banner or the
  // progress panel already shows it.
  const lastChangeFailed =
    state?.lastFailed &&
    !(state.update.available && state.lastFailed.id === lastFailed?.id) &&
    state.lastFailed.id !== operation?.id
      ? state.lastFailed
      : null;

  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold">Installed plugins</h2>
          <p className="text-sm text-muted-foreground">
            Plugins built into the Caddy binary managed by CPM.
          </p>
        </div>
        {canChange && !unavailable && (
          <Button size="sm" onClick={() => setAddOpen(true)} disabled={running}>
            <Plus className="size-4" />
            Add plugin
          </Button>
        )}
      </div>

      {unavailable ? (
        <div className="rounded-lg border border-border bg-muted/40 px-4 py-3 text-sm text-muted-foreground">
          Installing plugins requires Caddy API mode (CPM_CADDY_MODE=api).
        </div>
      ) : (
        state && (
          <>
            {!state.installEnabled && setup?.state !== "setup-needed" && (
              <div className="rounded-lg border border-border bg-muted/40 px-4 py-3 text-sm text-muted-foreground">
                {state.disabledReason ||
                  setup?.reason ||
                  "Installing plugins is not available."}
              </div>
            )}
            {setup?.state === "setup-needed" && (
              <div className="space-y-2 rounded-lg border border-border bg-muted/40 px-4 py-3 text-sm">
                <p className="text-muted-foreground">{setup.reason}</p>
                {setup.dropIn && (
                  <>
                    <pre className="overflow-x-auto rounded bg-muted p-2 text-xs">
                      {setup.dropIn}
                    </pre>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() =>
                        navigator.clipboard
                          ?.writeText(setup.dropIn ?? "")
                          .then(() => toast.success("Copied"))
                          .catch(() => toast.error("Copy failed"))
                      }
                    >
                      Copy
                    </Button>
                  </>
                )}
              </div>
            )}
            {lastChangeFailed && (
              <div
                role="alert"
                className="rounded-lg border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive"
              >
                {failureLabel(lastChangeFailed)}
                {lastChangeFailed.error ? `: ${lastChangeFailed.error}` : ""}
              </div>
            )}
            {state.update.available && (
              <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-muted/40 px-4 py-3 text-sm">
                <div className="space-y-1">
                  <div>
                    Caddy {state.update.shipped} is available (running{" "}
                    {state.update.managed})
                  </div>
                  {lastFailed && (
                    <div className="text-destructive">
                      The last update {lastFailed.step === "rolledBack" ? "was rolled back" : "failed"}
                      {lastFailed.error ? `: ${lastFailed.error}` : ""}
                    </div>
                  )}
                  <label className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      checked={state.autoUpdate}
                      onChange={(e) => setAutoUpdate(e.target.checked)}
                    />
                    Update automatically
                  </label>
                </div>
                {state.installEnabled && (
                  <Button size="sm" onClick={update} disabled={running}>
                    Update
                  </Button>
                )}
              </div>
            )}
            <div className="rounded-lg border border-border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Plugin</TableHead>
                    <TableHead className="w-32">Version</TableHead>
                    <TableHead>Modules</TableHead>
                    <TableHead className="w-28">Status</TableHead>
                    {canChange && (
                      <TableHead className="w-20 text-right">Actions</TableHead>
                    )}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {state.installed.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={5} className="text-center text-muted-foreground">
                        No plugins installed.
                      </TableCell>
                    </TableRow>
                  ) : (
                    state.installed.map((p) => (
                      <TableRow key={p.path}>
                        <TableCell className="font-medium">
                          <a
                            href={repoURL(p.path)}
                            target="_blank"
                            rel="noreferrer"
                            className="hover:underline"
                          >
                            {p.path}
                          </a>
                        </TableCell>
                        <TableCell>{p.version}</TableCell>
                        <TableCell className="text-muted-foreground">
                          {p.modules.join(", ")}
                        </TableCell>
                        <TableCell>{p.tested ? "✔ Tested" : "⚠ Untested"}</TableCell>
                        {canChange && (
                          <TableCell className="text-right">
                            <Button
                              variant="ghost"
                              size="icon"
                              aria-label={`Remove ${p.path}`}
                              disabled={running}
                              onClick={() => {
                                setUsages([]);
                                setRemoving(p);
                              }}
                            >
                              <Trash2 className="size-4" />
                            </Button>
                          </TableCell>
                        )}
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </div>
          </>
        )
      )}

      {operation && (
        <PluginOperationProgress operation={operation} onFinished={finished} />
      )}

      <AddPluginDialog
        open={addOpen}
        onOpenChange={setAddOpen}
        onStarted={setOperation}
      />

      <Dialog open={removing !== null} onOpenChange={(o) => !o && setRemoving(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Remove plugin</DialogTitle>
            <DialogDescription>
              Remove {removing?.path} from your Caddy build.
            </DialogDescription>
          </DialogHeader>
          {usages.length > 0 && (
            <div className="space-y-1 text-sm text-destructive">
              <p>This plugin is still in use:</p>
              <ul className="list-disc pl-5">
                {usages.map((u) => (
                  <li key={`${u.kind}-${u.id}`}>{usageLabel(u)}</li>
                ))}
              </ul>
            </div>
          )}
          <p className="text-sm text-muted-foreground">{RESTART_NOTE}</p>
          <p className="text-sm text-muted-foreground">{CADDY_UPDATE_NOTE}</p>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRemoving(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={remove}
              disabled={submitting || usages.length > 0}
            >
              Remove &amp; restart Caddy
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}
