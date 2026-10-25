import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { HostDialog } from "./HostDialog";
import type { DNSProvider } from "@/lib/types";

const providers: DNSProvider[] = [
  {
    id: 2,
    name: "CF",
    moduleId: "dns.providers.cloudflare",
    values: {},
    secretsSet: [],
    usedBy: [],
    available: true,
  },
];

// HostPluginsSection fetches on mount; stub it out so this test focuses on the
// host form payload.
vi.mock("./HostPluginsSection", () => ({
  HostPluginsSection: () => null,
}));

describe("HostDialog", () => {
  beforeEach(() => vi.clearAllMocks());

  it("submits joined domains and upstreams for a new host", async () => {
    const onSubmit = vi.fn();
    render(
      <HostDialog
        open
        onOpenChange={() => {}}
        host={null}
        dnsProviders={providers}
        submitting={false}
        onSubmit={onSubmit}
      />
    );

    const domainInputs = screen.getAllByPlaceholderText("example.com");
    fireEvent.change(domainInputs[0], { target: { value: "a.com" } });

    const upstreamInputs = screen.getAllByPlaceholderText("127.0.0.1:8080");
    fireEvent.change(upstreamInputs[0], { target: { value: "127.0.0.1:9000" } });

    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    expect(onSubmit).toHaveBeenCalledWith({
      matcher: "",
      domains: "a.com",
      upstreams: [{ backend: "127.0.0.1:9000" }],
      dnsProviderId: null,
    });
  });

  it("submits the selected DNS provider", async () => {
    const onSubmit = vi.fn();
    render(
      <HostDialog
        open
        onOpenChange={() => {}}
        host={null}
        dnsProviders={providers}
        submitting={false}
        onSubmit={onSubmit}
      />
    );
    fireEvent.change(screen.getAllByPlaceholderText("example.com")[0], {
      target: { value: "a.com" },
    });
    fireEvent.change(screen.getAllByPlaceholderText("127.0.0.1:8080")[0], {
      target: { value: "127.0.0.1:9000" },
    });
    fireEvent.change(screen.getByLabelText(/dns provider/i), {
      target: { value: "2" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({ dnsProviderId: 2 })
    );
  });

  it("hides the DNS provider select in Caddyfile mode", () => {
    render(
      <HostDialog
        open
        onOpenChange={() => {}}
        host={null}
        dnsProviders={null}
        submitting={false}
        onSubmit={() => {}}
      />
    );
    expect(screen.queryByLabelText(/dns provider/i)).not.toBeInTheDocument();
  });

  it("requires at least one domain and upstream", async () => {
    const onSubmit = vi.fn();
    render(
      <HostDialog
        open
        onOpenChange={() => {}}
        host={null}
        dnsProviders={[]}
        submitting={false}
        onSubmit={onSubmit}
      />
    );
    // Submit with empty fields -> zod validation blocks submission.
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));
    await waitFor(() =>
      expect(screen.getByText(/at least one valid domain/i)).toBeInTheDocument()
    );
    expect(onSubmit).not.toHaveBeenCalled();
  });
});
