import { expect, it } from 'vitest';
import type { Definition, Step } from './api';
import { workflowIssues } from './validation';

function textStep(id: string, reference: string): Step {
	return {
		id,
		kind: 'text',
		name: id,
		inputs: { text: { reference }, operation: { literal: 'trim' } }
	};
}

it('validates each node against its preceding outputs and its own branch scope', () => {
	const definition: Definition = {
		schema: 1,
		source: { kind: 'manual' },
		steps: [
			textStep('prepare', 'source["release.title"]'),
			{
				id: 'gate',
				kind: 'condition',
				name: 'Gate',
				inputs: {
					left: { reference: 'prepare.text' },
					operator: { literal: 'contains' },
					right: { literal: 'Release' }
				},
				then: [textStep('inside', 'gate.matched'), textStep('follow', 'inside.text')],
				else: [textStep('opposite', 'inside.text')]
			},
			textStep('after', 'inside.text'),
			textStep('self', 'self.text'),
			textStep('nested', 'source.release.title')
		]
	};
	const issues = workflowIssues(definition, { 'release.title': 'Release' });
	expect(issues.map(({ node, field }) => ({ node, field }))).toEqual([
		{ node: 'opposite', field: 'text' },
		{ node: 'after', field: 'text' },
		{ node: 'self', field: 'text' },
		{ node: 'nested', field: 'text' }
	]);
});

it('refreshes discovered source fields when the sample changes', () => {
	const definition: Definition = {
		schema: 1,
		source: { kind: 'manual' },
		steps: [textStep('first', 'source.title')]
	};
	expect(workflowIssues(definition, { title: 'Release' })).toEqual([]);
	expect(
		workflowIssues(definition, { body: 'Release' }).map(({ node, field }) => ({
			node,
			field
		}))
	).toEqual([{ node: 'first', field: 'text' }]);
});

const messageReply = (id: string): Step => ({
	id,
	kind: 'message_reply',
	name: id,
	inputs: {
		message_id: { reference: 'source.message_id' },
		text: { literal: 'Here is the guide' },
		delivery: { literal: 'review' }
	}
});
const waitFor = (id: string, minutes: number): Step => ({
	id,
	kind: 'wait',
	name: id,
	inputs: { minutes: { literal: minutes } }
});

it('requires an account and daily limit for the message trigger', () => {
	const issues = workflowIssues({
		schema: 1,
		source: { kind: 'message_received' },
		steps: [messageReply('reply')]
	});
	expect(issues.filter(({ node }) => node === 'source').map(({ field }) => field)).toEqual([
		'account_ids',
		'daily_limit'
	]);
});

it('rejects a delay the Instagram messaging window can never accommodate, across branches', () => {
	const source: Definition['source'] = {
		kind: 'message_received',
		account_ids: ['instagram'],
		daily_limit: 20
	};
	const delayed = (steps: Step[]) =>
		workflowIssues({ schema: 1, source, steps }).filter(({ field }) => field === 'text');
	expect(delayed([waitFor('wait', 1439), messageReply('reply')])).toEqual([]);
	expect(delayed([waitFor('wait', 1440), messageReply('reply')]).map(({ node }) => node)).toEqual([
		'reply'
	]);
	const branch: Step = {
		id: 'branch',
		kind: 'condition',
		name: 'branch',
		inputs: {
			left: { literal: 'a' },
			operator: { literal: 'equals' },
			right: { literal: 'a' }
		},
		then: [waitFor('inside', 1500)]
	};
	expect(delayed([branch, messageReply('after')]).map(({ node }) => node)).toEqual(['after']);
});

function sequenceStep(
	messages: { text: string; delay_minutes: number }[] | string,
	inputs: Step['inputs'] = {}
): Step {
	return {
		id: 'follow',
		kind: 'message_sequence',
		name: 'Follow up',
		inputs: { messages: { literal: messages }, delivery: { literal: 'review' }, ...inputs }
	};
}

function sequenceIssues(source: Definition['source'], steps: Step[]) {
	return workflowIssues({ schema: 1, source, steps })
		.filter((issue) => issue.node === 'follow' && issue.field === 'messages')
		.map((issue) => issue.message);
}

const messageTrigger: Definition['source'] = {
	kind: 'message_received',
	account_ids: ['ig'],
	daily_limit: 20
};
const commentTrigger: Definition['source'] = {
	kind: 'comment_received',
	account_ids: ['ig'],
	daily_limit: 20
};
const fromMessage = { message_id: { reference: 'source.message_id' } };
const fromComment = { engagement_id: { reference: 'source.engagement_id' } };

it('accepts follow-ups the window can accommodate', () => {
	const messages = [
		{ text: 'One', delay_minutes: 0 },
		{ text: 'Two', delay_minutes: 60 }
	];
	expect(sequenceIssues(messageTrigger, [sequenceStep(messages, fromMessage)])).toEqual([]);
	expect(
		sequenceIssues(messageTrigger, [sequenceStep(JSON.stringify(messages), fromMessage)])
	).toEqual([]);
});

it('rejects follow-ups no run could ever send', () => {
	const late = [
		{ text: 'One', delay_minutes: 800 },
		{ text: 'Two', delay_minutes: 800 }
	];
	expect(sequenceIssues(messageTrigger, [sequenceStep(late, fromMessage)])).toEqual([
		"This delay outlasts Instagram's 24 hour messaging window. Shorten the wait."
	]);
	for (const messages of [
		[],
		[{ text: ' ', delay_minutes: 0 }],
		[{ text: 'One', delay_minutes: 1.5 }],
		[{ text: 'One', delay_minutes: 1440 }],
		[{ text: '가'.repeat(334), delay_minutes: 0 }],
		Array.from({ length: 6 }, () => ({ text: 'One', delay_minutes: 0 }))
	])
		expect(sequenceIssues(messageTrigger, [sequenceStep(messages, fromMessage)])).toHaveLength(1);
});

it('requires a comment follow-up to continue a private reply on every path', () => {
	const messages = [{ text: 'One', delay_minutes: 0 }];
	const privateReply: Step = {
		id: 'private',
		kind: 'private_comment_reply',
		name: 'Private',
		inputs: {
			engagement_id: { reference: 'source.engagement_id' },
			text: { literal: 'Hi' },
			delivery: { literal: 'review' }
		}
	};
	expect(sequenceIssues(commentTrigger, [sequenceStep(messages, fromComment)])).toEqual([
		'A follow-up needs a private reply earlier in this path.'
	]);
	expect(
		sequenceIssues(commentTrigger, [privateReply, sequenceStep(messages, fromComment)])
	).toEqual([]);
	const branch: Step = {
		id: 'gate',
		kind: 'condition',
		name: 'Gate',
		inputs: {
			left: { literal: 'a' },
			operator: { literal: 'equals' },
			right: { literal: 'a' }
		},
		then: [privateReply]
	};
	expect(
		sequenceIssues(commentTrigger, [branch, sequenceStep(messages, fromComment)])
	).toHaveLength(1);
	expect(sequenceIssues({ kind: 'manual' }, [sequenceStep(messages, fromMessage)])).toEqual([
		'Follow-ups need the comment or incoming message trigger.'
	]);
});
