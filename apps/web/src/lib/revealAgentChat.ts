import { useUiStore } from "../store/ui";
import { useWorkspacePanels } from "../store/workspacePanels";

// A handoff card lives in the conversation, not in the right workspace.
// Reveal it only when the workspace has taken over the chat viewport.
export function revealAgentChat() {
  const panels = useWorkspacePanels.getState();
  if (panels.maximized) panels.toggleMaximized();
  if (
    window.matchMedia?.("(max-width: 960px)").matches &&
    !useUiStore.getState().rightOverviewCollapsed
  ) {
    useUiStore.getState().toggleRightOverviewCollapsed();
  }
}
