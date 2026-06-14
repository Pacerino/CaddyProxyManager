import { useEffect } from "react";
import { useFieldArray, useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import type { Host } from "@/lib/types";

const schema = z.object({
  matcher: z.string().optional(),
  domains: z.array(z.object({ value: z.string().min(1) })).min(1),
  upstreams: z.array(z.object({ backend: z.string().min(1) })).min(1),
});

export type HostFormValues = z.infer<typeof schema>;

export interface HostPayload {
  domains: string;
  matcher: string;
  upstreams: { backend: string }[];
}

interface HostDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  host: Host | null;
  submitting: boolean;
  onSubmit: (payload: HostPayload) => void;
}

function toFormValues(host: Host | null): HostFormValues {
  if (!host) {
    return {
      matcher: "",
      domains: [{ value: "" }],
      upstreams: [{ backend: "" }],
    };
  }
  return {
    matcher: host.matcher ?? "",
    domains: host.domains
      ? host.domains.split(/[\s,]+/).filter(Boolean).map((value) => ({ value }))
      : [{ value: "" }],
    upstreams:
      host.Upstreams?.length > 0
        ? host.Upstreams.map((u) => ({ backend: u.backend }))
        : [{ backend: "" }],
  };
}

export function HostDialog({
  open,
  onOpenChange,
  host,
  submitting,
  onSubmit,
}: HostDialogProps) {
  const {
    register,
    handleSubmit,
    control,
    reset,
    formState: { errors },
  } = useForm<HostFormValues>({
    resolver: zodResolver(schema),
    defaultValues: toFormValues(host),
  });

  useEffect(() => {
    reset(toFormValues(host));
  }, [host, open, reset]);

  const domains = useFieldArray({ control, name: "domains" });
  const upstreams = useFieldArray({ control, name: "upstreams" });

  const submit = handleSubmit((data) => {
    onSubmit({
      matcher: data.matcher ?? "",
      domains: data.domains.map((d) => d.value).join(" "),
      upstreams: data.upstreams,
    });
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{host ? "Edit host" : "Add host"}</DialogTitle>
          <DialogDescription>
            Configure domains, an optional matcher and upstream backends.
          </DialogDescription>
        </DialogHeader>

        <form className="space-y-5" onSubmit={submit}>
          <div className="space-y-2">
            <Label>Domains</Label>
            {domains.fields.map((field, index) => (
              <div key={field.id} className="flex gap-2">
                <Input
                  placeholder="example.com"
                  {...register(`domains.${index}.value` as const)}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  disabled={domains.fields.length === 1}
                  onClick={() => domains.remove(index)}
                >
                  <Trash2 className="size-4" />
                </Button>
              </div>
            ))}
            {errors.domains && (
              <p className="text-sm text-destructive">
                At least one valid domain is required
              </p>
            )}
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => domains.append({ value: "" })}
            >
              <Plus className="size-4" />
              Add domain
            </Button>
          </div>

          <div className="space-y-2">
            <Label htmlFor="matcher">Matcher (optional)</Label>
            <Input id="matcher" placeholder="/api/*" {...register("matcher")} />
          </div>

          <div className="space-y-2">
            <Label>Upstreams</Label>
            {upstreams.fields.map((field, index) => (
              <div key={field.id} className="flex gap-2">
                <Input
                  placeholder="127.0.0.1:8080"
                  {...register(`upstreams.${index}.backend` as const)}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  disabled={upstreams.fields.length === 1}
                  onClick={() => upstreams.remove(index)}
                >
                  <Trash2 className="size-4" />
                </Button>
              </div>
            ))}
            {errors.upstreams && (
              <p className="text-sm text-destructive">
                At least one valid upstream is required
              </p>
            )}
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => upstreams.append({ backend: "" })}
            >
              <Plus className="size-4" />
              Add upstream
            </Button>
          </div>

          <DialogFooter>
            <Button type="submit" disabled={submitting}>
              {submitting ? "Saving…" : "Save"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
