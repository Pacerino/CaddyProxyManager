import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { api, unwrap, type ApiEnvelope } from "@/lib/api";
import type { DNSProvider, DNSProviderHost, DNSProviderType } from "@/lib/types";
import {
  DNSProviderDialog,
  type DNSProviderPayload,
} from "@/components/DNSProviderDialog";

interface ApiError {
  response?: {
    status?: number;
    data?: { error?: ApiEnvelope<unknown>["error"] };
  };
}

const errorMessage = (err: unknown, fallback: string) =>
  (err as ApiError).response?.data?.error?.message ?? fallback;

// DNSProvidersSection manages the named DNS credentials hosts can use for
// DNS challenges. It needs Caddy API mode.
export function DNSProvidersSection() {
  const [providers, setProviders] = useState<DNSProvider[]>([]);
  const [types, setTypes] = useState<DNSProviderType[]>([]);
  const [loading, setLoading] = useState(true);
  const [unavailable, setUnavailable] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<DNSProvider | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.get<ApiEnvelope<DNSProvider[]>>("/caddy/dns-providers");
      setProviders(unwrap(res.data) ?? []);
      setUnavailable(false);
    } catch (err) {
      setProviders([]);
      // 400 is the API's answer in Caddyfile mode.
      if ((err as ApiError).response?.status === 400) setUnavailable(true);
      else toast.error("Failed to load DNS providers");
    }
    try {
      const res = await api.get<ApiEnvelope<DNSProviderType[]>>(
        "/caddy/dns-provider-types"
      );
      setTypes(unwrap(res.data) ?? []);
    } catch {
      setTypes([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const titleOf = (p: DNSProvider) =>
    types.find((t) => t.moduleId === p.moduleId)?.title ?? p.moduleId;

  const openDialog = (provider: DNSProvider | null) => {
    setEditing(provider);
    setDialogOpen(true);
  };

  const save = async (payload: DNSProviderPayload) => {
    setSubmitting(true);
    try {
      if (editing) await api.put(`/caddy/dns-providers/${editing.id}`, payload);
      else await api.post("/caddy/dns-providers", payload);
      toast.success("DNS provider saved");
      setDialogOpen(false);
      load();
    } catch (err) {
      toast.error(errorMessage(err, "Failed to save DNS provider"));
    } finally {
      setSubmitting(false);
    }
  };

  const remove = async (provider: DNSProvider) => {
    try {
      await api.delete(`/caddy/dns-providers/${provider.id}`);
      toast.success("DNS provider removed");
      load();
    } catch (err) {
      const e = err as ApiError;
      if (e.response?.status === 409) {
        // The conflicting hosts are in the error envelope's invalid field.
        const used = (e.response.data?.error?.invalid as DNSProviderHost[] | undefined) ?? [];
        const domains = used.map((h) => h.domains).join(", ");
        toast.error(
          domains
            ? `"${provider.name}" is used by: ${domains}`
            : `"${provider.name}" is used by hosts`
        );
      } else {
        toast.error(errorMessage(err, "Failed to remove DNS provider"));
      }
    }
  };

  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold">DNS providers</h2>
          <p className="text-sm text-muted-foreground">
            Credentials for DNS challenges. Pick one per host in the host editor.
          </p>
        </div>
        {!unavailable && (
          <Button size="sm" onClick={() => openDialog(null)} disabled={types.length === 0}>
            <Plus className="size-4" />
            Add provider
          </Button>
        )}
      </div>

      {unavailable ? (
        <div className="rounded-lg border border-border bg-muted/40 px-4 py-3 text-sm text-muted-foreground">
          DNS providers require Caddy API mode (CPM_CADDY_MODE=api).
        </div>
      ) : (
        <div className="rounded-lg border border-border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Provider</TableHead>
                <TableHead className="w-24">Used by</TableHead>
                <TableHead className="w-28 text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow>
                  <TableCell colSpan={4} className="text-center text-muted-foreground">
                    Loading…
                  </TableCell>
                </TableRow>
              ) : providers.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className="text-center text-muted-foreground">
                    No DNS providers yet.
                  </TableCell>
                </TableRow>
              ) : (
                providers.map((p) => (
                  <TableRow key={p.id}>
                    <TableCell className="font-medium">{p.name}</TableCell>
                    <TableCell>
                      {titleOf(p)}
                      {!p.available && (
                        <span
                          className="ml-2 rounded bg-destructive/10 px-1.5 py-0.5 text-xs text-destructive"
                          title="This provider module is not in the current Caddy build"
                        >
                          unavailable
                        </span>
                      )}
                    </TableCell>
                    <TableCell>{p.usedBy.length}</TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={`Edit ${p.name}`}
                          onClick={() => openDialog(p)}
                        >
                          <Pencil className="size-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={`Delete ${p.name}`}
                          onClick={() => remove(p)}
                        >
                          <Trash2 className="size-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      )}

      <DNSProviderDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        types={types}
        provider={editing}
        submitting={submitting}
        onSubmit={save}
      />
    </section>
  );
}
