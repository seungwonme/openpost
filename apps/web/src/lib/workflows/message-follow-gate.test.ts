import { expect, it } from 'vitest';
import type { Definition, Step, Value } from './api';
import { workflowIssues } from './validation';
import { followGateTemplates } from './message-follow-gate-template';

const commentTrigger: Definition['source'] = {
	kind: 'comment_received',
	account_ids: ['ig'],
	daily_limit: 20
};
const privateReply: Step = {
	id: 'private',
	kind: 'private_comment_reply',
	name: 'Private reply',
	inputs: {
		engagement_id: { reference: 'source.engagement_id' },
		text: { literal: 'Reply to get the link' },
		delivery: { literal: 'review' }
	}
};
const gate = {
	button_title: 'Open guide',
	button_url: 'https://example.com/guide',
	not_following_text: 'Follow us, then reply again.',
	max_checks: 3
};

function followUp(followGate: Value['literal'], first = 'Here is your guide'): Step {
	return {
		id: 'follow',
		kind: 'message_sequence',
		name: 'Follow up',
		inputs: {
			engagement_id: { reference: 'source.engagement_id' },
			messages: { literal: [{ text: first, delay_minutes: 0 }] },
			delivery: { literal: 'review' },
			follow_gate: { literal: followGate }
		}
	};
}

function gateIssues(source: Definition['source'], steps: Step[]) {
	return workflowIssues({ schema: 1, source, steps })
		.filter((issue) => issue.node === 'follow' && issue.field === 'follow_gate')
		.map((issue) => issue.message);
}

it('accepts a follow check on a comment campaign, stored or imported', () => {
	expect(gateIssues(commentTrigger, [privateReply, followUp(gate)])).toEqual([]);
	expect(gateIssues(commentTrigger, [privateReply, followUp(JSON.stringify(gate))])).toEqual([]);
	expect(gateIssues(commentTrigger, [privateReply, followUp(null)])).toEqual([]);
});

it('explains a follow check the server would reject', () => {
	for (const invalid of [
		{ ...gate, button_url: 'http://example.com/guide' },
		{ ...gate, button_url: 'https://{{source.text}}' },
		{ ...gate, button_title: ' ' },
		{ ...gate, button_title: 'a'.repeat(21) },
		{ ...gate, not_following_text: '' },
		{ ...gate, max_checks: 6 }
	])
		expect(gateIssues(commentTrigger, [privateReply, followUp(invalid)])).toHaveLength(1);
	expect(gateIssues(commentTrigger, [privateReply, followUp(gate, 'a'.repeat(641))])).toEqual([
		'The first message carries the link button and supports up to 640 characters.'
	]);
	const { engagement_id: _comment, ...inputs } = followUp(gate).inputs ?? {};
	const messageFollowUp: Step = {
		...followUp(gate),
		inputs: { ...inputs, message_id: { reference: 'source.message_id' } }
	};
	expect(
		gateIssues({ kind: 'message_received', account_ids: ['ig'], daily_limit: 20 }, [
			messageFollowUp
		])
	).toEqual(['A follow check needs the comment trigger and a private reply.']);
});

it('limits the quick reply title to what Instagram shows', () => {
	const issues = (title: string) =>
		workflowIssues({
			schema: 1,
			source: commentTrigger,
			steps: [
				{
					...privateReply,
					inputs: { ...privateReply.inputs, quick_reply_title: { literal: title } }
				}
			]
		}).filter((issue) => issue.field === 'quick_reply_title');
	expect(issues('팔로우했어요')).toEqual([]);
	expect(issues('a'.repeat(21))).toHaveLength(1);
});

it('starts the follow link template with a quick reply and a follow check', () => {
	const [template] = followGateTemplates();
	const [ask, link] = template.definition.steps ?? [];
	expect(ask.inputs?.quick_reply_title).toEqual({ literal: 'I followed' });
	expect(link.inputs?.follow_gate?.literal).toMatchObject({ button_title: 'Get the guide' });
	expect(
		workflowIssues(template.definition)
			.filter((issue) => issue.node === 'link')
			.map((issue) => issue.field)
	).toEqual(['follow_gate']);
});
