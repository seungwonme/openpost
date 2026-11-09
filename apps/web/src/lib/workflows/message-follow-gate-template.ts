import type { Definition, Step } from './api';
import { defaultFollowGate } from './message-validation';
import { m } from '$lib/paraglide/messages';

export const followGateTemplateID = 'instagram-follow-link';

/** A comment campaign that sends its link only to people who follow the account. */
export function followGateTemplates(): {
	id: string;
	name: string;
	description: string;
	definition: Definition;
}[] {
	const ask: Step = {
		id: 'ask',
		kind: 'private_comment_reply',
		name: m.workflows_comment_private_node(),
		inputs: {
			engagement_id: { reference: 'source.engagement_id' },
			text: { literal: m.workflows_follow_template_ask() },
			quick_reply_title: { literal: m.workflows_follow_gate_quick_reply_default() },
			delivery: { literal: 'automatic' }
		}
	};
	const link: Step = {
		id: 'link',
		kind: 'message_sequence',
		name: m.workflows_message_sequence_node(),
		inputs: {
			engagement_id: { reference: 'source.engagement_id' },
			response_window_minutes: { literal: 10080 },
			delivery: { literal: 'review' },
			messages: { literal: [{ text: m.workflows_follow_template_link(), delay_minutes: 0 }] },
			follow_gate: { literal: defaultFollowGate() }
		}
	};
	return [
		{
			id: followGateTemplateID,
			name: m.workflows_follow_template(),
			description: m.workflows_follow_template_help(),
			definition: {
				schema: 1,
				source: {
					kind: 'comment_received',
					account_ids: [],
					keywords: [],
					excluded_keywords: [],
					match_mode: 'word',
					daily_limit: 100,
					top_level_only: true,
					priority: 50,
					post_scope: 'selected'
				},
				steps: [ask, link]
			}
		}
	];
}
