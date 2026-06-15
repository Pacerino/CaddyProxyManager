import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { PluginDialog } from "./PluginDialog";
import type { CaddyModule, PluginSchema } from "@/lib/types";

const cfModule: CaddyModule = {
  id: "dns.providers.cloudflare",
  namespace: "dns.providers",
  name: "cloudflare",
  standard: false,
};

const cfSchema: PluginSchema = {
  moduleId: "dns.providers.cloudflare",
  title: "Cloudflare DNS",
  scopes: ["global"],
  path: "apps/tls/automation/policies",
  fields: [{ key: "api_token", label: "API Token", type: "secret", required: true }],
};

const rawModule: CaddyModule = {
  id: "http.handlers.unknown",
  namespace: "http.handlers",
  name: "unknown",
  standard: false,
};

describe("PluginDialog typed form", () => {
  it("submits schema values", () => {
    const onSubmit = vi.fn();
    render(
      <PluginDialog
        open
        onOpenChange={() => {}}
        module={cfModule}
        schema={cfSchema}
        existing={null}
        submitting={false}
        onSubmit={onSubmit}
      />
    );

    fireEvent.change(screen.getByLabelText(/API Token/i), {
      target: { value: "tok123" },
    });
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    expect(onSubmit).toHaveBeenCalledWith({
      moduleId: "dns.providers.cloudflare",
      enabled: true,
      values: { api_token: "tok123" },
    });
  });
});

describe("PluginDialog raw JSON fallback", () => {
  it("rejects invalid JSON", () => {
    const onSubmit = vi.fn();
    render(
      <PluginDialog
        open
        onOpenChange={() => {}}
        module={rawModule}
        schema={null}
        existing={null}
        submitting={false}
        onSubmit={onSubmit}
      />
    );

    fireEvent.change(screen.getByLabelText(/Config path/i), {
      target: { value: "apps/http/servers/srv0" },
    });
    fireEvent.change(screen.getByLabelText(/Config \(JSON\)/i), {
      target: { value: "{ not json" },
    });
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getByText(/must be valid JSON/i)).toBeInTheDocument();
  });

  it("submits valid raw config with path", () => {
    const onSubmit = vi.fn();
    render(
      <PluginDialog
        open
        onOpenChange={() => {}}
        module={rawModule}
        schema={null}
        existing={null}
        submitting={false}
        onSubmit={onSubmit}
      />
    );

    fireEvent.change(screen.getByLabelText(/Config path/i), {
      target: { value: "apps/http/servers/srv0" },
    });
    fireEvent.change(screen.getByLabelText(/Config \(JSON\)/i), {
      target: { value: '{"foo":"bar"}' },
    });
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    expect(onSubmit).toHaveBeenCalledWith({
      moduleId: "http.handlers.unknown",
      enabled: true,
      path: "apps/http/servers/srv0",
      config: { foo: "bar" },
    });
  });
});
