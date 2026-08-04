import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { ThemeProvider } from "@/components/ui/theme";
import { ProjectProvider } from "@/components/ui/ProjectProvider";
import { Shell } from "@/components/ui/Shell";
import * as api from "@/lib/api-client";
import { usePathname, useSearchParams } from "next/navigation";

vi.mock("next/navigation", () => ({
  usePathname: vi.fn(() => "/"),
  useSearchParams: vi.fn(() => new URLSearchParams()),
}));

describe("Shell", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders the top nav, tree nav, breadcrumb and children", async () => {
    vi.spyOn(api, "listTests").mockResolvedValue([
      {
        id: "1",
        name: "Checkout Load",
        target_url: "http://x",
        virtual_users: 5,
        duration_seconds: 30,
        created_at: "2026-07-24T00:00:00Z",
      },
    ]);
    vi.spyOn(api, "listProjects").mockResolvedValue([]);

    render(
      <ThemeProvider>
        <ProjectProvider>
          <Shell>
            <p>page content</p>
          </Shell>
        </ProjectProvider>
      </ThemeProvider>,
    );

    expect(screen.getByText("BoltRunner")).toBeInTheDocument();
    expect(screen.getByText("page content")).toBeInTheDocument();
    expect(
      screen.getByRole("navigation", { name: "Workspace" }),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(
        screen.getByRole("link", { name: /Checkout Load/i }),
      ).toBeInTheDocument(),
    );
  });

  it("shows a Default-only breadcrumb on the root path", async () => {
    vi.spyOn(api, "listTests").mockResolvedValue([]);
    vi.spyOn(api, "listProjects").mockResolvedValue([]);
    render(
      <ThemeProvider>
        <ProjectProvider>
          <Shell>
            <p>page content</p>
          </Shell>
        </ProjectProvider>
      </ThemeProvider>,
    );
    expect(
      await screen.findByRole("navigation", { name: "Breadcrumb" }),
    ).toHaveTextContent("Default");
  });

  it("shows a still-usable page when listTests fails", async () => {
    vi.spyOn(api, "listTests").mockRejectedValue(new Error("boom"));
    vi.spyOn(api, "listProjects").mockResolvedValue([]);
    render(
      <ThemeProvider>
        <ProjectProvider>
          <Shell>
            <p>page content</p>
          </Shell>
        </ProjectProvider>
      </ThemeProvider>,
    );
    expect(await screen.findByText("page content")).toBeInTheDocument();
    expect(
      screen.getByRole("navigation", { name: "Breadcrumb" }),
    ).toHaveTextContent("Default");
  });

  it("shows an Admin breadcrumb on the admin path", async () => {
    vi.mocked(usePathname).mockReturnValue("/admin");
    vi.spyOn(api, "listTests").mockResolvedValue([]);
    vi.spyOn(api, "listProjects").mockResolvedValue([]);
    render(
      <ThemeProvider>
        <ProjectProvider>
          <Shell>
            <p>admin content</p>
          </Shell>
        </ProjectProvider>
      </ThemeProvider>,
    );
    expect(
      await screen.findByRole("navigation", { name: "Breadcrumb" }),
    ).toHaveTextContent("Admin");
  });

  it("shows a Test Runs breadcrumb on the history path with no testId", async () => {
    vi.mocked(usePathname).mockReturnValue("/history");
    vi.mocked(useSearchParams).mockReturnValue(new URLSearchParams());
    vi.spyOn(api, "listTests").mockResolvedValue([]);
    vi.spyOn(api, "listProjects").mockResolvedValue([]);
    render(
      <ThemeProvider>
        <ProjectProvider>
          <Shell>
            <p>history content</p>
          </Shell>
        </ProjectProvider>
      </ThemeProvider>,
    );
    expect(
      await screen.findByRole("navigation", { name: "Breadcrumb" }),
    ).toHaveTextContent("Test Runs");
  });

  it("shows the test name in the breadcrumb on the history path with a known testId", async () => {
    vi.mocked(usePathname).mockReturnValue("/history");
    vi.mocked(useSearchParams).mockReturnValue(new URLSearchParams("testId=1"));
    vi.spyOn(api, "listTests").mockResolvedValue([
      {
        id: "1",
        name: "Checkout Load",
        target_url: "http://x",
        virtual_users: 5,
        duration_seconds: 30,
        created_at: "2026-07-24T00:00:00Z",
      },
    ]);
    vi.spyOn(api, "listProjects").mockResolvedValue([]);
    render(
      <ThemeProvider>
        <ProjectProvider>
          <Shell>
            <p>history content</p>
          </Shell>
        </ProjectProvider>
      </ThemeProvider>,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("navigation", { name: "Breadcrumb" }),
      ).toHaveTextContent("Checkout Load"),
    );
  });

  it("falls back to the raw testId in the breadcrumb when the test is unknown", async () => {
    vi.mocked(usePathname).mockReturnValue("/history");
    vi.mocked(useSearchParams).mockReturnValue(
      new URLSearchParams("testId=unknown-id"),
    );
    vi.spyOn(api, "listTests").mockResolvedValue([]);
    vi.spyOn(api, "listProjects").mockResolvedValue([]);
    render(
      <ThemeProvider>
        <ProjectProvider>
          <Shell>
            <p>history content</p>
          </Shell>
        </ProjectProvider>
      </ThemeProvider>,
    );
    expect(
      await screen.findByRole("navigation", { name: "Breadcrumb" }),
    ).toHaveTextContent("unknown-id");
  });

  it("shows a Run breadcrumb on a run detail path", async () => {
    vi.mocked(usePathname).mockReturnValue("/runs/r1");
    vi.mocked(useSearchParams).mockReturnValue(new URLSearchParams());
    vi.spyOn(api, "listTests").mockResolvedValue([]);
    vi.spyOn(api, "listProjects").mockResolvedValue([]);
    render(
      <ThemeProvider>
        <ProjectProvider>
          <Shell>
            <p>run content</p>
          </Shell>
        </ProjectProvider>
      </ThemeProvider>,
    );
    expect(
      await screen.findByRole("navigation", { name: "Breadcrumb" }),
    ).toHaveTextContent("Run r1");
  });

  it("renders the bottom tab bar", async () => {
    vi.spyOn(api, "listTests").mockResolvedValue([]);
    vi.spyOn(api, "listProjects").mockResolvedValue([]);
    render(
      <ThemeProvider>
        <ProjectProvider>
          <Shell>
            <p>page content</p>
          </Shell>
        </ProjectProvider>
      </ThemeProvider>,
    );
    expect(
      await screen.findByRole("navigation", { name: "Primary" }),
    ).toBeInTheDocument();
  });

  it("wraps the tree nav so it is hidden below md and shown at md and up", async () => {
    vi.spyOn(api, "listTests").mockResolvedValue([]);
    vi.spyOn(api, "listProjects").mockResolvedValue([]);
    render(
      <ThemeProvider>
        <ProjectProvider>
          <Shell>
            <p>page content</p>
          </Shell>
        </ProjectProvider>
      </ThemeProvider>,
    );
    const treeNav = await screen.findByRole("navigation", {
      name: "Workspace",
    });
    expect(treeNav.parentElement).toHaveClass("hidden", "md:block");
  });

  it("shows a Tests breadcrumb on the tests path", async () => {
    vi.mocked(usePathname).mockReturnValue("/tests");
    vi.spyOn(api, "listTests").mockResolvedValue([]);
    vi.spyOn(api, "listProjects").mockResolvedValue([]);
    render(
      <ThemeProvider>
        <ProjectProvider>
          <Shell>
            <p>tests content</p>
          </Shell>
        </ProjectProvider>
      </ThemeProvider>,
    );
    expect(
      await screen.findByRole("navigation", { name: "Breadcrumb" }),
    ).toHaveTextContent("Tests");
  });

  it("shows the fetched project name in the breadcrumb and tree nav", async () => {
    vi.mocked(usePathname).mockReturnValue("/");
    vi.spyOn(api, "listTests").mockResolvedValue([]);
    vi.spyOn(api, "listProjects").mockResolvedValue([
      { id: "p1", name: "Payments", created_at: "2026-07-24T00:00:00Z", is_default: false },
    ]);

    render(
      <ThemeProvider>
        <ProjectProvider>
          <Shell>
            <p>page content</p>
          </Shell>
        </ProjectProvider>
      </ThemeProvider>,
    );

    await waitFor(() =>
      expect(
        screen.getByRole("navigation", { name: "Breadcrumb" }),
      ).toHaveTextContent("Payments"),
    );
    expect(
      screen.getByRole("navigation", { name: "Workspace" }),
    ).toHaveTextContent("Payments");
  });

  it("keeps showing Default when the projects endpoint fails", async () => {
    vi.mocked(usePathname).mockReturnValue("/");
    vi.spyOn(api, "listTests").mockResolvedValue([]);
    vi.spyOn(api, "listProjects").mockRejectedValue(new Error("boom"));

    render(
      <ThemeProvider>
        <ProjectProvider>
          <Shell>
            <p>page content</p>
          </Shell>
        </ProjectProvider>
      </ThemeProvider>,
    );

    expect(
      await screen.findByRole("navigation", { name: "Breadcrumb" }),
    ).toHaveTextContent("Default");
  });

  it("shows the test name in the breadcrumb on a test detail path", async () => {
    vi.mocked(usePathname).mockReturnValue("/tests/1");
    vi.mocked(useSearchParams).mockReturnValue(new URLSearchParams());
    vi.spyOn(api, "listTests").mockResolvedValue([
      {
        id: "1",
        name: "Checkout Load",
        target_url: "http://x",
        virtual_users: 5,
        duration_seconds: 30,
        created_at: "2026-07-24T00:00:00Z",
      },
    ]);
    vi.spyOn(api, "listProjects").mockResolvedValue([]);

    render(
      <ThemeProvider>
        <ProjectProvider>
          <Shell>
            <p>detail content</p>
          </Shell>
        </ProjectProvider>
      </ThemeProvider>,
    );

    await waitFor(() =>
      expect(
        screen.getByRole("navigation", { name: "Breadcrumb" }),
      ).toHaveTextContent("Checkout Load"),
    );
  });

  it("falls back to the raw id in the breadcrumb for an unknown test detail path", async () => {
    vi.mocked(usePathname).mockReturnValue("/tests/unknown-id");
    vi.mocked(useSearchParams).mockReturnValue(new URLSearchParams());
    vi.spyOn(api, "listTests").mockResolvedValue([]);
    vi.spyOn(api, "listProjects").mockResolvedValue([]);

    render(
      <ThemeProvider>
        <ProjectProvider>
          <Shell>
            <p>detail content</p>
          </Shell>
        </ProjectProvider>
      </ThemeProvider>,
    );

    expect(
      await screen.findByRole("navigation", { name: "Breadcrumb" }),
    ).toHaveTextContent("unknown-id");
  });
});
