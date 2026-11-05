import type { components } from "@openpost/api-contract";
import { inboxQueryKeys } from "./inbox";
import { openPostQueryPolicy, liveQueryStaleTime } from "./policies";
export type ConversationAutomation = components["schemas"]["ConversationAutomation"];
export type MessageAutomationReply = components["schemas"]["AutomationMessage"];
export interface MessageAutomationQueryAPI {
  get(
    workspaceID: string,
    conversationID: string,
    signal: AbortSignal,
  ): Promise<ConversationAutomation>;
}
export function messageAutomationQueryOptions(
  api: MessageAutomationQueryAPI,
  workspaceID: string,
  conversationID: string,
) {
  return {
    ...openPostQueryPolicy(liveQueryStaleTime),
    queryKey: [
      ...inboxQueryKeys.engagementRoot(workspaceID),
      "message-automation",
      conversationID,
    ] as const,
    enabled: Boolean(workspaceID && conversationID),
    queryFn: ({ signal }: { signal: AbortSignal }) => api.get(workspaceID, conversationID, signal),
  };
}
