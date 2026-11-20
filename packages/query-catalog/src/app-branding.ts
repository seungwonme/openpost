import type { components } from "@openpost/api-contract";
import type { QueryFunctionContext } from "@tanstack/query-core";
import { openPostWorkspaceKey } from "./keys";
import { openPostQueryPolicy } from "./policies";

export type AppBrandingProfile = components["schemas"]["AppBrandingProfile"];
export type AppBrandingSettings = components["schemas"]["AppBrandingSettings"];
export type AppBrandingProfileInput = components["schemas"]["AppBrandingProfileInput"];
export type AppBrandingImageInput = components["schemas"]["AppBrandingImageInput"];
export type AppBrandingUpdate = components["schemas"]["AppBrandingUpdate"];

export interface AppBrandingQueryAPI {
  getAppBranding(workspaceId: string, signal: AbortSignal): Promise<AppBrandingSettings>;
}
export const appBrandingQueryKey = (workspaceId: string) =>
  openPostWorkspaceKey(workspaceId, "app-branding");
export function appBrandingQueryOptions(api: AppBrandingQueryAPI, workspaceId: string) {
  const queryKey = appBrandingQueryKey(workspaceId);
  return {
    ...openPostQueryPolicy(),
    queryKey,
    enabled: Boolean(workspaceId),
    queryFn: ({ signal }: QueryFunctionContext<typeof queryKey>) =>
      api.getAppBranding(workspaceId, signal),
  };
}

export const defaultAppBranding: AppBrandingProfile = { display_name: "Aiden OpenPost" };

// Dark-specific artwork is optional. Uploaded colors are never tinted by themes.
export function appBrandingImage(profile: AppBrandingProfile, dark: boolean, wordmark = false) {
  return wordmark
    ? dark
      ? (profile.dark_wordmark ?? profile.wordmark)
      : profile.wordmark
    : dark
      ? (profile.dark_logo ?? profile.logo)
      : profile.logo;
}
