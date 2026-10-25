import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { HostsPage } from "./Hosts";
import { api } from "@/lib/api";

vi.mock("@/lib/api", async (orig) => ({
  ...(await orig<typeof import("@/lib/api")>()),
  api: { get: vi.fn(), put: vi.fn(), post: vi.fn(), delete: vi.fn() },
}));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));
vi.mock("@/components/HostDialog", () => ({ HostDialog: () => null }));

const get = api.get as unknown as ReturnType<typeof vi.fn>;
const hosts = [
  { ID: 1, domains: "a.com", matcher: "", Upstreams: [], dnsProviderId: 2 },
];
const WARNING = "DNS provider ignored in Caddyfile mode";

function mockProviders(providers: Promise<unknown>) {
  get.mockImplementation((url: string) =>
    url === "/hosts"
      ? Promise.resolve({ data: { result: hosts } })
      : providers
  );
}

describe("HostsPage DNS provider warning", () => {
  beforeEach(() => vi.clearAllMocks());

  it("shows no warning while the provider request is pending", async () => {
    mockProviders(new Promise(() => {}));
    render(<HostsPage />);
    await screen.findByText("a.com");
    expect(screen.queryByLabelText(WARNING)).not.toBeInTheDocument();
  });

  it("shows the warning on a 400 (Caddyfile mode)", async () => {
    mockProviders(Promise.reject({ response: { status: 400 } }));
    render(<HostsPage />);
    expect(await screen.findByLabelText(WARNING)).toBeInTheDocument();
  });

  it("shows no warning on a 500", async () => {
    mockProviders(Promise.reject({ response: { status: 500 } }));
    render(<HostsPage />);
    await screen.findByText("a.com");
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.queryByLabelText(WARNING)).not.toBeInTheDocument();
  });
});
