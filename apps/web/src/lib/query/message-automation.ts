import type { MessageAutomationQueryAPI } from '@openpost/query-catalog';
import { client } from '$lib/api/client';
import { queryGET } from './transport';
import { m } from '$lib/paraglide/messages';
export const messageAutomationQueryAPI: MessageAutomationQueryAPI = {
	async get(workspaceID, conversationID, signal) {
		const { data } = await queryGET({
			signal,
			fallback: m.workflows_load_failed(),
			request: (signal) =>
				client.GET('/messages/{conversation_id}/automation', {
					params: {
						query: { workspace_id: workspaceID },
						path: { conversation_id: conversationID }
					},
					signal
				})
		});
		return data ?? { replies: [] };
	}
};
