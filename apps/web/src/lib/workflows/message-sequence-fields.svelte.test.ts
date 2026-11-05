import { describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import MessageSequenceFields from './message-sequence-fields.svelte';
import type { Step } from './api';

function sequence(messages: { text: string; delay_minutes: number }[]): Step {
	return {
		id: 'follow',
		kind: 'message_sequence',
		name: 'Follow up',
		inputs: { messages: { literal: messages } }
	};
}

describe('message sequence fields', () => {
	it('adds, reorders and removes messages in the stored order', async () => {
		const oninput = vi.fn();
		const step = sequence([
			{ text: 'First', delay_minutes: 0 },
			{ text: 'Second', delay_minutes: 30 }
		]);
		const screen = render(MessageSequenceFields, { step, oninput });
		await screen.getByRole('button', { name: 'Add message' }).click();
		expect(oninput).toHaveBeenLastCalledWith('messages', {
			literal: [
				{ text: 'First', delay_minutes: 0 },
				{ text: 'Second', delay_minutes: 30 },
				{ text: '', delay_minutes: 60 }
			]
		});
		await screen.getByRole('button', { name: 'Move down: 1' }).click();
		expect(oninput).toHaveBeenLastCalledWith('messages', {
			literal: [
				{ text: 'Second', delay_minutes: 30 },
				{ text: 'First', delay_minutes: 0 }
			]
		});
		await screen.getByRole('button', { name: 'Remove message: 1' }).click();
		expect(oninput).toHaveBeenLastCalledWith('messages', {
			literal: [{ text: 'Second', delay_minutes: 30 }]
		});
	});

	it('stops at the message limit and hides editing when read only', async () => {
		const full = sequence(
			Array.from({ length: 5 }, (_, i) => ({ text: String(i), delay_minutes: 0 }))
		);
		const screen = render(MessageSequenceFields, { step: full, oninput: vi.fn() });
		await expect.element(screen.getByRole('button', { name: 'Add message' })).toBeDisabled();
		screen.unmount();
		const locked = render(MessageSequenceFields, {
			step: full,
			oninput: vi.fn(),
			readonly: true
		});
		await expect.element(locked.getByLabelText('Message 1')).toBeDisabled();
		expect(locked.getByRole('button', { name: 'Add message' }).elements()).toHaveLength(0);
	});
});
