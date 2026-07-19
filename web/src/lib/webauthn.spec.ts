// パスキー儀式のブラウザ側変換ヘルパ(ADR00021)。moka-core(go-webauthn)の JSON は
// バイナリ列を base64url 文字列で運ぶ — navigator.credentials の ArrayBuffer 界との往復だけを
// ここで検証する。navigator.credentials 自体はブラウザの領分(モック不要の純関数のみ)。
import { describe, expect, it } from 'vitest';
import {
	base64urlToBuffer,
	bufferToBase64url,
	credentialToJSON,
	parseCreationOptions,
	parseRequestOptions
} from './webauthn';

function bytes(...values: number[]): ArrayBuffer {
	const buffer = new ArrayBuffer(values.length);
	new Uint8Array(buffer).set(values);
	return buffer;
}

function asBytes(source: BufferSource | undefined): number[] {
	if (source instanceof ArrayBuffer) return [...new Uint8Array(source)];
	if (source instanceof Uint8Array) return [...source];
	throw new Error('unexpected buffer source');
}

describe('base64url conversion', () => {
	it('round-trips arbitrary bytes', () => {
		const original = bytes(0, 1, 2, 127, 250, 255);
		const decoded = base64urlToBuffer(bufferToBase64url(original));
		expect(asBytes(decoded)).toEqual(asBytes(original));
	});

	it('encodes with the url-safe alphabet and without padding', () => {
		expect(bufferToBase64url(bytes(0xfb, 0xff))).toBe('-_8');
	});

	it('decodes url-safe input with missing padding', () => {
		expect(asBytes(base64urlToBuffer('-_8'))).toEqual([0xfb, 0xff]);
	});

	it('handles the empty string', () => {
		expect(bufferToBase64url(bytes())).toBe('');
		expect(asBytes(base64urlToBuffer(''))).toEqual([]);
	});
});

describe('parseCreationOptions (登録の儀式)', () => {
	const creationJSON = {
		publicKey: {
			rp: { id: 'localhost', name: 'moka-1' },
			user: { id: 'AQID', name: 'owner', displayName: 'owner' },
			challenge: 'BAUG',
			pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
			timeout: 60000,
			excludeCredentials: [
				{ type: 'public-key', id: 'BwgJ', transports: ['internal', 'weird'] },
				{ type: 'public-key', id: 'AQID', transports: ['weird'] }
			],
			authenticatorSelection: { residentKey: 'required', userVerification: 'preferred' },
			attestation: 'none'
		}
	};

	it('converts base64url fields to buffers and keeps the rest', () => {
		const options = parseCreationOptions(creationJSON);

		expect(asBytes(options.publicKey.challenge)).toEqual([4, 5, 6]);
		expect(asBytes(options.publicKey.user.id)).toEqual([1, 2, 3]);
		expect(options.publicKey.user.name).toBe('owner');
		expect(options.publicKey.rp).toEqual({ id: 'localhost', name: 'moka-1' });
		expect(options.publicKey.pubKeyCredParams).toEqual([{ type: 'public-key', alg: -7 }]);
		expect(options.publicKey.authenticatorSelection?.residentKey).toBe('required');
		expect(options.publicKey.attestation).toBe('none');
	});

	it('converts excludeCredentials ids and drops unknown transports', () => {
		const options = parseCreationOptions(creationJSON);
		const [first, second] = options.publicKey.excludeCredentials ?? [];

		expect(asBytes(first?.id)).toEqual([7, 8, 9]);
		expect(first?.transports).toEqual(['internal']);
		expect(second?.transports).toBeUndefined();
	});

	it('rejects malformed options', () => {
		expect(() => parseCreationOptions({ publicKey: { challenge: 'AQID' } })).toThrowError();
	});
});

describe('parseRequestOptions (ログインの儀式)', () => {
	it('converts base64url fields to buffers and keeps the rest', () => {
		const options = parseRequestOptions({
			publicKey: {
				challenge: 'AQID',
				rpId: 'localhost',
				timeout: 60000,
				allowCredentials: [{ type: 'public-key', id: 'BAUG', transports: ['usb'] }],
				userVerification: 'preferred'
			}
		});

		expect(asBytes(options.publicKey.challenge)).toEqual([1, 2, 3]);
		expect(options.publicKey.rpId).toBe('localhost');
		const [allowed] = options.publicKey.allowCredentials ?? [];
		expect(asBytes(allowed?.id)).toEqual([4, 5, 6]);
		expect(allowed?.transports).toEqual(['usb']);
	});

	it('rejects malformed options', () => {
		expect(() => parseRequestOptions({ publicKey: {} })).toThrowError();
	});
});

describe('credentialToJSON', () => {
	it('serializes a registration credential without native toJSON', () => {
		const credential = {
			id: 'cred-id',
			rawId: bytes(1, 2, 3),
			type: 'public-key',
			authenticatorAttachment: 'platform',
			getClientExtensionResults: () => ({}),
			response: {
				clientDataJSON: bytes(4, 5, 6),
				attestationObject: bytes(7, 8, 9),
				getTransports: () => ['internal']
			}
		};

		expect(credentialToJSON(credential)).toEqual({
			id: 'cred-id',
			rawId: 'AQID',
			type: 'public-key',
			authenticatorAttachment: 'platform',
			clientExtensionResults: {},
			response: {
				clientDataJSON: 'BAUG',
				attestationObject: 'BwgJ',
				transports: ['internal']
			}
		});
	});

	it('serializes a login credential and omits a missing userHandle', () => {
		const credential = {
			id: 'cred-id',
			rawId: bytes(1),
			type: 'public-key',
			authenticatorAttachment: null,
			response: {
				clientDataJSON: bytes(2),
				authenticatorData: bytes(3),
				signature: bytes(4),
				userHandle: null
			}
		};

		expect(credentialToJSON(credential)).toEqual({
			id: 'cred-id',
			rawId: 'AQ',
			type: 'public-key',
			clientExtensionResults: {},
			response: { clientDataJSON: 'Ag', authenticatorData: 'Aw', signature: 'BA' }
		});
	});

	it('includes the userHandle when the authenticator returns one', () => {
		const credential = {
			id: 'cred-id',
			rawId: bytes(1),
			type: 'public-key',
			response: {
				clientDataJSON: bytes(2),
				authenticatorData: bytes(3),
				signature: bytes(4),
				userHandle: bytes(9)
			}
		};

		const json = credentialToJSON(credential);
		expect(json).toMatchObject({ response: { userHandle: 'CQ' } });
	});

	it('prefers the native toJSON when the browser provides it', () => {
		const native = { id: 'native', rawId: 'AQID', response: {} };
		const credential = {
			id: 'native',
			rawId: bytes(1),
			type: 'public-key',
			toJSON: () => native,
			response: { clientDataJSON: bytes(2) }
		};

		expect(credentialToJSON(credential)).toBe(native);
	});
});
