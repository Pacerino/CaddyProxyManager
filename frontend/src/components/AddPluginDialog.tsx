import { useEffect, useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { api, unwrap, type ApiEnvelope } from "@/lib/api";
import type { CatalogPlugin, PluginOperation } from "@/lib/types";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onStarted: (op: PluginOperation) => void;
}

export const UNTESTED_WARNING =
  "This plugin hasn't been tested with CPM. Its settings may only be editable as raw JSON, and it might fail to start. If Caddy doesn't come up with it, CPM switches back to your previous Caddy automatically.";
export const RESTART_NOTE =
  "All hosts are briefly unavailable while Caddy restarts.";

interface ApiError {
  response?: { status?: number; data?: { error?: { message?: string } } };
}

// AddPluginDialog searches the plugin catalog and starts an install.
export function AddPluginDialog({ open, onOpenChange, onStarted }: Props) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<CatalogPlugin[]>([]);
  const [loading, setLoading] = useState(false);
  const [catalogError, setCatalogError] = useState("");
  const [selected, setSelected] = useState<CatalogPlugin | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) {
      setQuery("");
      setSelected(null);
      return;
    }
    let cancelled = false;
    // The first load (empty query) is immediate; typing is debounced.
    const timer = setTimeout(async () => {
      setLoading(true);
      try {
        const res = await api.get<ApiEnvelope<CatalogPlugin[]>>(
          "/caddy/plugins/catalog",
          { params: { q: query } }
        );
        if (cancelled) return;
        setResults(unwrap(res.data) ?? []);
        setCatalogError("");
      } catch (err) {
        if (cancelled) return;
        setResults([]);
        setCatalogError(
          `Plugin catalog unavailable: ${
            (err as ApiError).response?.data?.error?.message ??
            "the build service is unreachable"
          }`
        );
      } finally {
        if (!cancelled) setLoading(false);
      }
    }, query === "" ? 0 : 300);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [open, query]);

  const install = async () => {
    if (!selected) return;
    setSubmitting(true);
    try {
      const res = await api.post<ApiEnvelope<PluginOperation>>(
        "/caddy/plugins",
        { path: selected.path }
      );
      onOpenChange(false);
      onStarted(unwrap(res.data));
    } catch (err) {
      const e = err as ApiError;
      toast.error(
        e.response?.status === 409
          ? "Another plugin operation is already running"
          : e.response?.data?.error?.message ?? "Failed to start installation"
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Add plugin</DialogTitle>
          <DialogDescription>
            Search the Caddy plugin catalog and install a plugin into your Caddy
            build.
          </DialogDescription>
        </DialogHeader>
        <Input
          placeholder="Search plugins…"
          aria-label="Search plugins"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <div className="max-h-72 overflow-y-auto rounded-md border border-border">
          {catalogError ? (
            <div className="p-3 text-sm text-destructive">{catalogError}</div>
          ) : loading && results.length === 0 ? (
            <div className="p-3 text-sm text-muted-foreground">Loading…</div>
          ) : results.length === 0 ? (
            <div className="p-3 text-sm text-muted-foreground">
              No plugins found.
            </div>
          ) : (
            <ul>
              {results.map((p) => (
                <li key={p.path}>
                  <button
                    type="button"
                    disabled={p.installed}
                    aria-pressed={selected?.path === p.path}
                    onClick={() => setSelected(p)}
                    className={`w-full px-3 py-2 text-left text-sm hover:bg-muted/60 disabled:opacity-50 ${
                      selected?.path === p.path ? "bg-muted" : ""
                    }`}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-medium">{p.path}</span>
                      <span className="text-xs text-muted-foreground">
                        {p.installed ? "Installed" : p.tested ? "✔ Tested" : "⚠ Untested"}
                      </span>
                    </div>
                    <div className="text-xs text-muted-foreground">
                      {p.modules.join(", ")}
                    </div>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
        {selected && !selected.tested && (
          <div className="rounded-md border border-yellow-500/40 bg-yellow-500/10 px-3 py-2 text-sm">
            {UNTESTED_WARNING}
          </div>
        )}
        <p className="text-sm text-muted-foreground">{RESTART_NOTE}</p>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={install} disabled={!selected || submitting}>
            Install &amp; restart Caddy
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
