import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { DNSProviderDialog } from "./DNSProviderDialog";
import type { DNSProvider, DNSProviderType } from "@/lib/types";

const types: DNSProviderType[] = [
  {
    moduleId: "dns.providers.cloudflare",
    name: "cloudflare",
    title: "Cloudflare",
    fields: [{ key: "api_token", label: "API Token", type: "secret" }],
    source: "binary",
  },
  {
    moduleId: "dns.providers.unknown",
    name: "unknown",
    title: "Unknown",
    fields: [],
    source: "none",
  },
];

describe("DNSProviderDialog", () => {
  it("submits name, module and values for a new provider", () => {
    const onSubmit = vi.fn();
    render(
      <DNSProviderDialog
        open
        onOpenChange={() => {}}
        types={types}
        provider={null}
        submitting={false}
        onSubmit={onSubmit}
      />
    );
    fireEvent.change(screen.getByLabelText(/^name/i), { target: { value: "CF" } });
    fireEvent.change(screen.getByLabelText(/provider type/i), {
      target: { value: "dns.providers.cloudflare" },
    });
    const token = screen.getByLabelText(/API Token/i) as HTMLInputElement;
    expect(token.type).toBe("password");
    fireEvent.change(token, { target: { value: "t" } });
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    expect(onSubmit).toHaveBeenCalledWith({
      name: "CF",
      moduleId: "dns.providers.cloudflare",
      values: { api_token: "t" },
    });
  });

  it("keeps a stored secret by submitting an empty value", () => {
    const onSubmit = vi.fn();
    const provider: DNSProvider = {
      id: 1,
      name: "CF",
      moduleId: "dns.providers.cloudflare",
      values: {},
      secretsSet: ["api_token"],
      usedBy: [],
      available: true,
    };
    render(
      <DNSProviderDialog
        open
        onOpenChange={() => {}}
        types={types}
        provider={provider}
        submitting={false}
        onSubmit={onSubmit}
      />
    );
    expect(screen.getByPlaceholderText("•••••• (unchanged)")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));
    expect(onSubmit).toHaveBeenCalledWith({
      name: "CF",
      moduleId: "dns.providers.cloudflare",
      values: { api_token: "" },
    });
  });

  it("edits raw JSON for providers without a schema", () => {
    const onSubmit = vi.fn();
    render(
      <DNSProviderDialog
        open
        onOpenChange={() => {}}
        types={types}
        provider={null}
        submitting={false}
        onSubmit={onSubmit}
      />
    );
    fireEvent.change(screen.getByLabelText(/^name/i), { target: { value: "U" } });
    fireEvent.change(screen.getByLabelText(/provider type/i), {
      target: { value: "dns.providers.unknown" },
    });
    fireEvent.change(screen.getByLabelText(/values \(json\)/i), {
      target: { value: '{"x":1}' },
    });
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));
    expect(onSubmit).toHaveBeenCalledWith({
      name: "U",
      moduleId: "dns.providers.unknown",
      values: { x: 1 },
    });
  });
});
