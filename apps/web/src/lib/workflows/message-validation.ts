import { z } from 'zod';
import type { Definition, Source, Step, Value } from './api';
import { m } from '$lib/paraglide/messages';
type Issue = { node: string; field: string; message: string };

const messageWindowMinutes = 24 * 60;
const maxMessageBytes = 1000;
export const maxSequenceMessages = 5;

export function messageSourceIssues(source: Source): Issue[] {
	if (source.kind !== 'message_received') return [];
	const issues: Issue[] = [];
	if (!source.account_ids?.length)
		issues.push({ node: 'source', field: 'account_ids', message: m.workflows_required() });
	const limit = Number(source.daily_limit);
	if (!Number.isInteger(limit) || limit < 1 || limit > 1000)
		issues.push({
			node: 'source',
			field: 'daily_limit',
			message: m.workflows_message_daily_limit()
		});
	return issues;
}

export type SequenceMessage = { text: string; delay_minutes: number };

const sequenceSchema = z.array(
	z.object({ text: z.string(), delay_minutes: z.number().default(0) })
);
const sequenceTextSchema = z.string().transform((text, context) => {
	try {
		return JSON.parse(text);
	} catch {
		context.addIssue({ code: 'custom', message: 'invalid JSON' });
		return z.NEVER;
	}
});

/** Authored messages arrive as a stored list, or as text from a JSON import. */
export function sequenceMessages(value: Value['literal']): SequenceMessage[] {
	const list = sequenceSchema.safeParse(value);
	if (list.success) return list.data;
	return sequenceTextSchema.pipe(sequenceSchema).safeParse(value).data ?? [];
}

function sequenceListValid(messages: SequenceMessage[]): boolean {
	return (
		messages.length >= 1 &&
		messages.length <= maxSequenceMessages &&
		messages.every(
			({ text, delay_minutes }) =>
				text.trim() !== '' &&
				(text.includes('{{') || new TextEncoder().encode(text.trim()).length <= maxMessageBytes) &&
				Number.isInteger(delay_minutes) &&
				delay_minutes >= 0 &&
				delay_minutes < messageWindowMinutes
		)
	);
}

type MessagePath = { elapsed: number; privateReply: boolean; answered: boolean };

function sequenceIssue(step: Step, source: Source, path: MessagePath): string {
	if (source.kind !== 'message_received' && source.kind !== 'comment_received')
		return m.workflows_message_sequence_source();
	if (source.kind === 'comment_received' && !path.privateReply)
		return m.workflows_message_sequence_private();
	const messages = sequenceMessages(step.inputs?.messages?.literal);
	if (!sequenceListValid(messages)) return m.workflows_message_sequence_limits();
	const delay = messages.reduce((total, message) => total + message.delay_minutes, 0);
	return delay + (source.kind === 'message_received' ? path.elapsed : 0) >= messageWindowMinutes
		? m.workflows_message_delay_window()
		: '';
}

/** Report what the provider window or a run's one answer can never accommodate. */
export function messageActionIssues(definition: Definition): Issue[] {
	const { source } = definition;
	const issues: Issue[] = [];
	const report = (step: Step, field: string, message: string) =>
		message && issues.push({ node: step.id, field, message });
	function visit(step: Step, path: MessagePath): MessagePath {
		const next = { ...path };
		if (step.kind === 'wait' && !step.inputs?.minutes?.reference) {
			const minutes = Number(step.inputs?.minutes?.literal);
			if (Number.isFinite(minutes)) next.elapsed += minutes;
		}
		if (step.kind === 'private_comment_reply') next.privateReply = true;
		if (step.kind === 'comment_reply' && step.inputs?.channel?.literal === 'private')
			next.privateReply = true;
		if (step.kind === 'message_reply' && source.kind === 'message_received') {
			if (next.elapsed >= messageWindowMinutes)
				report(step, 'text', m.workflows_message_delay_window());
		}
		if (step.kind === 'message_sequence')
			report(step, 'messages', sequenceIssue(step, source, next));
		if (step.kind === 'message_reply' || step.kind === 'message_sequence') {
			if (next.answered)
				report(
					step,
					step.kind === 'message_reply' ? 'text' : 'messages',
					m.workflows_message_sequence_once()
				);
			next.answered = true;
		}
		return next;
	}
	function walk(steps: Step[], start: MessagePath): MessagePath {
		let path = start;
		for (const step of steps) {
			path = visit(step, path);
			const yes = walk(step.then ?? [], path);
			const no = walk(step.else ?? [], path);
			path = {
				elapsed: Math.max(yes.elapsed, no.elapsed),
				privateReply: yes.privateReply && no.privateReply,
				answered: yes.answered || no.answered
			};
		}
		return path;
	}
	if (
		source.kind === 'message_received' ||
		source.kind === 'comment_received' ||
		usesSequence(definition.steps ?? [])
	)
		walk(definition.steps ?? [], { elapsed: 0, privateReply: false, answered: false });
	return issues;
}

function usesSequence(steps: Step[]): boolean {
	return steps.some(
		(step) =>
			step.kind === 'message_sequence' ||
			usesSequence(step.then ?? []) ||
			usesSequence(step.else ?? [])
	);
}
