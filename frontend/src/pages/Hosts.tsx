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
import type { Host } from "@/lib/types";
import { HostDialog, type HostPayload } from "@/components/HostDialog";

export function HostsPage() {
  const [hosts, setHosts] = useState<Host[]>([]);
  const [loading, setLoading] = useState(true);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<Host | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const loadHosts = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.get<ApiEnvelope<Host[]>>("/hosts");
      setHosts(unwrap(res.data) ?? []);
    } catch {
      toast.error("Failed to load hosts");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadHosts();
  }, [loadHosts]);

  const openCreate = () => {
    setEditing(null);
    setDialogOpen(true);
  };

  const openEdit = (host: Host) => {
    setEditing(host);
    setDialogOpen(true);
  };

  const saveHost = async (payload: HostPayload) => {
    setSubmitting(true);
    try {
      if (editing) {
        await api.put("/hosts", {
          ...editing,
          domains: payload.domains,
          matcher: payload.matcher,
          Upstreams: payload.upstreams.map((u) => ({ backend: u.backend })),
        });
        toast.success("Host updated");
      } else {
        await api.post("/hosts", {
          domains: payload.domains,
          matcher: payload.matcher,
          Upstreams: payload.upstreams,
        });
        toast.success("Host created");
      }
      setDialogOpen(false);
      loadHosts();
    } catch {
      toast.error("Failed to save host");
    } finally {
      setSubmitting(false);
    }
  };

  const deleteHost = async (id: number) => {
    try {
      await api.delete(`/hosts/${id}`);
      toast.success("Host deleted");
      loadHosts();
    } catch {
      toast.error("Failed to delete host");
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Hosts</h1>
          <p className="text-sm text-muted-foreground">
            Manage your reverse proxy hosts.
          </p>
        </div>
        <Button onClick={openCreate}>
          <Plus className="size-4" />
          Add host
        </Button>
      </div>

      <div className="rounded-lg border border-border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-12">ID</TableHead>
              <TableHead>Domains</TableHead>
              <TableHead>Matcher</TableHead>
              <TableHead>Upstreams</TableHead>
              <TableHead className="w-28 text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground">
                  Loading…
                </TableCell>
              </TableRow>
            ) : hosts.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground">
                  No hosts yet.
                </TableCell>
              </TableRow>
            ) : (
              hosts.map((host) => (
                <TableRow key={host.ID}>
                  <TableCell>{host.ID}</TableCell>
                  <TableCell className="font-medium">{host.domains}</TableCell>
                  <TableCell>{host.matcher || "—"}</TableCell>
                  <TableCell>
                    {host.Upstreams?.map((u) => u.backend).join(", ") || "—"}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => openEdit(host)}
                      >
                        <Pencil className="size-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => deleteHost(host.ID)}
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

      <HostDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        host={editing}
        submitting={submitting}
        onSubmit={saveHost}
      />
    </div>
  );
}
