import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AiProviderSettings } from "./AiProviderSettings";
import { ApiError } from "../api/client";

afterEach(() => {
  cleanup();
});

const mockState = vi.hoisted(() => {
  const mutation = () => ({
    mutateAsync: vi.fn(async (_input?: unknown) => ({})),
    isPending: false,
    error: null as unknown,
  });
  return {
    providers: [] as unknown[],
    providersStatus: "success" as "pending" | "error" | "success",
    createProvider: mutation(),
    setKey: mutation(),
    checkHealth: mutation(),
    deleteProvider: mutation(),
  };
});

vi.mock("../api/hooks", () => ({
  useAiProvidersQuery: () => ({
    data:
      mockState.providersStatus === "success" ? mockState.providers : undefined,
    isPending: mockState.providersStatus === "pending",
    isError: mockState.providersStatus === "error",
    error: mockState.providersStatus === "error" ? new Error("boom") : null,
    refetch: vi.fn(),
  }),
  useCreateAiProvider: () => mockState.createProvider,
  useSetAiProviderKey: () => mockState.setKey,
  useCheckAiProviderHealth: () => mockState.checkHealth,
  useDeleteAiProvider: () => mockState.deleteProvider,
}));

function readyProvider(overrides: Record<string, unknown> = {}) {
  return {
    id: "provider-1",
    name: "DeepSeek",
    protocol: "openai_chat",
    base_url: "https://api.deepseek.com/v1",
    model: "deepseek-chat",
    status: "ready",
    health_status: "healthy",
    health_error_code: null,
    has_key: true,
    last_health_at: "2026-09-01T12:00:00Z",
    version: 3,
    created_at: "2026-09-01T11:00:00Z",
    updated_at: "2026-09-01T12:00:00Z",
    ...overrides,
  };
}

describe("AiProviderSettings", () => {
  it("shows the registration form when no provider exists and registers on submit", async () => {
    mockState.providers = [];
    render(<AiProviderSettings />);

    expect(screen.getByText("名称")).toBeTruthy();
    expect(screen.getByText("登记供应商")).toBeTruthy();
    fireEvent.change(screen.getByPlaceholderText("例如 DeepSeek"), {
      target: { value: "DeepSeek" },
    });
    fireEvent.change(
      screen.getByPlaceholderText("https://api.example.com/v1"),
      { target: { value: "https://api.deepseek.com/v1" } },
    );
    fireEvent.change(screen.getByPlaceholderText("例如 deepseek-chat"), {
      target: { value: "deepseek-chat" },
    });
    fireEvent.click(screen.getByText("登记供应商"));

    await waitFor(() => {
      expect(mockState.createProvider.mutateAsync).toHaveBeenCalledWith({
        name: "DeepSeek",
        protocol: "openai_chat",
        base_url: "https://api.deepseek.com/v1",
        model: "deepseek-chat",
      });
    });
  });

  it("renders a ready provider with status and key saving", async () => {
    mockState.providers = [readyProvider()];
    render(<AiProviderSettings />);

    expect(screen.getByText("DeepSeek")).toBeTruthy();
    expect(screen.getByText("已就绪")).toBeTruthy();
    expect(screen.getByPlaceholderText("已保存（输入可更换）")).toBeTruthy();

    fireEvent.change(screen.getByPlaceholderText("已保存（输入可更换）"), {
      target: { value: "sk-new" },
    });
    fireEvent.click(screen.getByText("保存密钥"));

    await waitFor(() => {
      expect(mockState.setKey.mutateAsync).toHaveBeenCalledWith({
        id: "provider-1",
        apiKey: "sk-new",
        expectedVersion: 3,
      });
    });
  });

  it("maps the unavailable key store error to a safe explanation", () => {
    mockState.providers = [readyProvider()];
    mockState.setKey.mutateAsync = vi.fn(async () => {
      throw new Error("unused");
    });
    mockState.setKey.error = new ApiError("store unavailable", {
      code: "AI_KEY_STORE_UNAVAILABLE",
    });
    render(<AiProviderSettings />);

    expect(
      screen.getByText(/操作系统安全存储不可用，无法保存 API 密钥/),
    ).toBeTruthy();
  });

  it("shows the human-readable health failure reason", () => {
    mockState.providers = [
      readyProvider({
        status: "unavailable",
        health_status: "unhealthy",
        health_error_code: "AI_KEY_INVALID",
      }),
    ];
    render(<AiProviderSettings />);
    expect(screen.getByText("未就绪")).toBeTruthy();
    expect(screen.getByText("API 密钥被拒绝（401/403）")).toBeTruthy();
  });

  it("renders every registered provider card and opens the add form on demand", () => {
    mockState.providers = [
      readyProvider(),
      readyProvider({
        id: "provider-2",
        name: "Kimi",
        model: "kimi-k2",
        base_url: "https://api.moonshot.cn/v1",
      }),
    ];
    render(<AiProviderSettings />);

    const deepSeekHeadings = screen
      .getAllByText("DeepSeek")
      .filter((node) => node.tagName === "H4");
    expect(deepSeekHeadings.length).toBe(1);
    expect(screen.getByText("Kimi")).toBeTruthy();
    expect(screen.queryByText("名称")).toBeNull();

    fireEvent.click(screen.getByText("添加供应商"));
    expect(screen.getByText("名称")).toBeTruthy();
  });
});
