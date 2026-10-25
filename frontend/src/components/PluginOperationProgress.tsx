import { useEffect, useRef, useState } from "react";
import { Check, Circle, Loader2, X } from "lucide-react";
import { api, unwrap, type ApiEnvelope } from "@/lib/api";
import type { PluginOperation, PluginOpStep } from "@/lib/types";

const STEPS: { step: PluginOpStep; label: string }[] = [
  { step: "requesting", label: "Requesting build" },
  { step: "downloading", label: "Downloading Caddy" },
  { step: "verifying", label: "Verifying binary" },
  { step: "restarting", label: "Restarting Caddy" },
  { step: "reapplying", label: "Re-applying configuration" },
];

const FINAL: PluginOpStep[] = ["done", "rolledBack", "failed"];

export const isFinished = (op: PluginOperation) => FINAL.includes(op.step);

interface Props {
  operation: PluginOperation;
  onFinished: (op: PluginOperation) => void;
}

// PluginOperationProgress polls a running operation every second and shows
// its current step and final outcome. Steps can move backwards while the
// backend rolls back, so only the current step is highlighted.
export function PluginOperationProgress({ operation, onFinished }: Props) {
  const [op, setOp] = useState(operation);
  const finished = useRef(false);
  const callback = useRef(onFinished);
  callback.current = onFinished;

  useEffect(() => {
    setOp(operation);
    finished.current = false;
  }, [operation.id]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    let cancelled = false;
    const finish = (o: PluginOperation) => {
      if (finished.current) return;
      finished.current = true;
      callback.current(o);
    };
    if (isFinished(operation)) {
      finish(operation);
      return;
    }
    const timer = setInterval(async () => {
      try {
        const res = await api.get<ApiEnvelope<PluginOperation>>(
          `/caddy/plugins/operations/${operation.id}`
        );
        if (cancelled) return;
        const next = unwrap(res.data);
        setOp(next);
        if (isFinished(next)) {
          clearInterval(timer);
          finish(next);
        }
      } catch {
        // Caddy restarts can briefly drop requests; keep polling.
      }
    }, 1000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [operation.id]); // eslint-disable-line react-hooks/exhaustive-deps

  const title = `${op.kind === "remove" ? "Removing" : op.kind === "update" ? "Updating" : "Installing"} ${op.package}`;

  return (
    <div className="rounded-lg border border-border p-4 space-y-3" data-testid="plugin-progress">
      <div className="text-sm font-medium">{title}</div>
      {isFinished(op) ? (
        <div
          role="status"
          className={
            op.step === "done"
              ? "flex items-center gap-2 text-sm text-green-600"
              : "flex items-center gap-2 text-sm text-destructive"
          }
        >
          {op.step === "done" ? (
            <Check className="size-4" />
          ) : (
            <X className="size-4" />
          )}
          <span>
            {op.step === "done" && "Done"}
            {op.step === "rolledBack" &&
              "Rolled back to your previous Caddy"}
            {op.step === "failed" && "Failed"}
            {op.error ? `: ${op.error}` : ""}
          </span>
        </div>
      ) : (
        <ol className="space-y-1">
          {STEPS.map(({ step, label }) => {
            const current = step === op.step;
            return (
              <li
                key={step}
                aria-current={current ? "step" : undefined}
                className={
                  current
                    ? "flex items-center gap-2 text-sm font-medium"
                    : "flex items-center gap-2 text-sm text-muted-foreground"
                }
              >
                {current ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <Circle className="size-4" />
                )}
                {label}
              </li>
            );
          })}
        </ol>
      )}
    </div>
  );
}
