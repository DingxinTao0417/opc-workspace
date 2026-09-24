import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { MemoryRouter, useLocation } from "react-router-dom";
import { useUiStore } from "../store/ui";
import { renderAiRichText } from "./AiRichText";

function LocationProbe() {
  const location = useLocation();
  return (
    <output aria-label="location">{location.pathname + location.search}</output>
  );
}

function renderMessage(content: string) {
  return render(
    <MemoryRouter initialEntries={["/tasks"]}>
      <div>{renderAiRichText(content)}</div>
      <LocationProbe />
    </MemoryRouter>,
  );
}

afterEach(() => {
  cleanup();
  useUiStore.setState({
    rightOverviewCollapsed: false,
    rightPanelTab: "summary",
    rightPanelRequestMode: "open",
    rightPanelRequest: 0,
  });
});

describe("AI rich text workspace launchers", () => {
  it("switches to AI mode, expands the right workspace and requests one exact tool", () => {
    useUiStore.setState({ rightOverviewCollapsed: true });
    renderMessage("可以从[变更审查](/ai?workspace=review)继续。");

    fireEvent.click(screen.getByRole("link", { name: "变更审查" }));

    expect(screen.getByLabelText("location")).toHaveTextContent("/ai");
    expect(useUiStore.getState()).toMatchObject({
      rightOverviewCollapsed: false,
      rightPanelTab: "review",
      rightPanelRequestMode: "open",
      rightPanelRequest: 1,
    });
  });

  it("renders an injected workspace parameter as inert text", () => {
    renderMessage(
      "不要打开[浏览器](/ai?workspace=browser&url=https://example.com)。",
    );
    expect(screen.queryByRole("link", { name: "浏览器" })).toBeNull();
    expect(useUiStore.getState().rightPanelRequest).toBe(0);
  });
});
