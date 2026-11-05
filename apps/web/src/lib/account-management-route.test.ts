import { describe, expect, it } from 'vitest';
import {
	continuationHrefForNormalizedConnection,
	interpretAccountManagementURL,
	presentAccountManagementFeedback
} from './account-management-route';

describe('account management route URL interpretation', () => {
	it('shows the failed callback stage and bounded provider diagnostics once', () => {
		const url = new URL(
			'https://openpost.test/settings?tab=accounts&oauth_status=failed&oauth_reason=profile_fetch_failed&workspace_id=workspace-62'
		);
		url.searchParams.set('oauth_diagnostic', 'HTTP 400; code=100; subcode=33; trace=trace-safe');
		const state = interpretAccountManagementURL(url);
		expect(state).toEqual({
			feedback: {
				kind: 'profile_fetch_failed',
				diagnostic: 'HTTP 400; code=100; subcode=33; trace=trace-safe'
			},
			workspaceID: 'workspace-62',
			cleanHref: '/settings?tab=accounts'
		});
		expect(presentAccountManagementFeedback(state.feedback)).toEqual({
			tone: 'error',
			message:
				'OpenPost could not read the provider profile. Check the app permissions and account eligibility, then reconnect. HTTP 400; code=100; subcode=33; trace=trace-safe.'
		});
	});

	it.each([
		'profile=secret-token',
		'access_token=secret-token',
		'HTTP 400; code=<script>alert(1)</script>',
		'HTTP 400; trace=' + 'a'.repeat(97),
		'HTTP 400\naccess_token=secret-token'
	])('discards untrusted callback diagnostic text: %s', (diagnostic) => {
		const url = new URL(
			'https://openpost.test/settings?tab=accounts&oauth_status=failed&oauth_reason=profile_fetch_failed'
		);
		url.searchParams.set('oauth_diagnostic', diagnostic);
		const state = interpretAccountManagementURL(url);
		expect(state.cleanHref).toBe('/settings?tab=accounts');
		expect(presentAccountManagementFeedback(state.feedback)?.message).not.toContain(diagnostic);
	});

	it.each([
		'profile=invalid_response',
		'profile=invalid_user_id',
		'profile=professional_account_required'
	])('preserves controlled profile diagnostics: %s', (diagnostic) => {
		const url = new URL(
			'https://openpost.test/settings?tab=accounts&oauth_status=failed&oauth_reason=profile_fetch_failed'
		);
		url.searchParams.set('oauth_diagnostic', diagnostic);
		const state = interpretAccountManagementURL(url);
		expect(state.cleanHref).toBe('/settings?tab=accounts');
		expect(presentAccountManagementFeedback(state.feedback)?.message).toContain(diagnostic);
	});

	it('keeps Settings navigation in place while consuming scoped OAuth feedback', () => {
		expect(
			interpretAccountManagementURL(
				new URL(
					'https://openpost.test/settings?tab=accounts&oauth_status=cancelled&workspace_id=workspace-62&source=help'
				)
			)
		).toEqual({
			feedback: { kind: 'oauth_cancelled' },
			workspaceID: 'workspace-62',
			cleanHref: '/settings?tab=accounts&source=help'
		});
	});

	it('preserves the Settings tab and maps legacy errors to bounded one-time feedback', () => {
		expect(
			interpretAccountManagementURL(
				new URL('https://openpost.test/settings?tab=accounts&error=Provider%20unavailable')
			)
		).toEqual({
			feedback: { kind: 'oauth_failed' },
			workspaceID: '',
			cleanHref: '/settings?tab=accounts'
		});
	});

	it('does not consume unrelated Settings URL state', () => {
		expect(
			interpretAccountManagementURL(new URL('https://openpost.test/settings?tab=accounts#accounts'))
		).toEqual({
			feedback: null,
			workspaceID: '',
			cleanHref: '/settings?tab=accounts#accounts'
		});
	});

	it('shows an actionable error when Facebook returns no manageable Pages', () => {
		const state = interpretAccountManagementURL(
			new URL(
				'https://openpost.test/settings?tab=accounts&oauth_status=failed&oauth_reason=facebook_no_pages&workspace_id=workspace-62'
			)
		);

		expect(state).toEqual({
			feedback: { kind: 'facebook_no_pages' },
			workspaceID: 'workspace-62',
			cleanHref: '/settings?tab=accounts'
		});
		expect(presentAccountManagementFeedback(state.feedback)).toEqual({
			tone: 'error',
			message:
				'Facebook did not share any manageable Pages. Create a Page, request Page access, or check the Facebook app permissions, then try again.'
		});
	});

	it('explains when Unified Login shared no Instagram account', () => {
		const state = interpretAccountManagementURL(
			new URL(
				'https://openpost.test/settings?tab=accounts&oauth_status=failed&oauth_reason=instagram_no_assigned_account&workspace_id=workspace-62'
			)
		);

		expect(state).toEqual({
			feedback: { kind: 'instagram_no_assigned_account' },
			workspaceID: 'workspace-62',
			cleanHref: '/settings?tab=accounts'
		});
		expect(presentAccountManagementFeedback(state.feedback)).toEqual({
			tone: 'error',
			message:
				'Instagram found no professional account shared with this app. Log in again, choose the business portfolio that owns the account, and share the Instagram account.'
		});
	});

	it('explains when Instagram has no professional account linked to a managed Page', () => {
		const state = interpretAccountManagementURL(
			new URL(
				'https://openpost.test/settings?tab=accounts&oauth_status=failed&oauth_reason=instagram_no_page_linked_account&workspace_id=workspace-62'
			)
		);

		expect(state).toEqual({
			feedback: { kind: 'instagram_no_page_linked_account' },
			workspaceID: 'workspace-62',
			cleanHref: '/settings?tab=accounts'
		});
		expect(presentAccountManagementFeedback(state.feedback)).toEqual({
			tone: 'error',
			message:
				'Instagram needs a Business or Creator account linked to a Facebook Page you can manage. Link the account to a Page, check your Page access, then reconnect.'
		});
	});
});

describe('normalized account connection continuation', () => {
	it('opens the composer after the first destination is connected', () => {
		expect(
			continuationHrefForNormalizedConnection({
				workspaceID: 'workspace-62',
				accountIDs: ['account-9'],
				openFreshComposer: true
			})
		).toBe('/?workspace_id=workspace-62&account_ids=account-9');
	});

	it('returns later connections to Social accounts in Settings', () => {
		expect(
			continuationHrefForNormalizedConnection({
				workspaceID: 'workspace-62',
				accountIDs: ['account-9'],
				openFreshComposer: false
			})
		).toBe('/settings?tab=accounts');
	});
});
