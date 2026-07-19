// パスキー儀式のブラウザ側変換ヘルパ(ADR00021)。新規依存を足さず、素の
// navigator.credentials と往復するための純関数だけを置く:
//   moka-core(go-webauthn)の JSON(バイナリ列 = base64url 文字列)
//     → parse*Options → navigator.credentials.create / .get(ArrayBuffer 界)
//     → credentialToJSON → moka-core の finish(WebAuthn JSON 形式)
// PublicKeyCredential.toJSON のようなネイティブ API があればそれを優先し、
// 無いブラウザでは自前の変換にフォールバックする。
import { credentialCreationOptionsSchema, credentialRequestOptionsSchema } from '$lib/api/schemas';

export function bufferToBase64url(buffer: ArrayBuffer): string {
	const bytes = new Uint8Array(buffer);
	let binary = '';
	for (const byte of bytes) binary += String.fromCharCode(byte);
	return btoa(binary).replaceAll('+', '-').replaceAll('/', '_').replace(/=+$/, '');
}

export function base64urlToBuffer(value: string): ArrayBuffer {
	const base64 = value.replaceAll('-', '+').replaceAll('_', '/');
	const padded = base64.padEnd(base64.length + ((4 - (base64.length % 4)) % 4), '=');
	const binary = atob(padded);
	const buffer = new ArrayBuffer(binary.length);
	const bytes = new Uint8Array(buffer);
	for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
	return buffer;
}

const KNOWN_TRANSPORTS: readonly AuthenticatorTransport[] = [
	'ble',
	'hybrid',
	'internal',
	'nfc',
	'usb'
];

// 未知の transport はブラウザ API に渡さず黙って落とす(ヒントに過ぎないため)
function toTransports(values: string[] | undefined): AuthenticatorTransport[] | undefined {
	if (!values) return undefined;
	const known = values.filter((value): value is AuthenticatorTransport =>
		KNOWN_TRANSPORTS.some((transport) => transport === value)
	);
	return known.length > 0 ? known : undefined;
}

// CredentialCreationOptions.publicKey は DOM 型では optional — 儀式には必須なので必須で返す
export type ParsedCreationOptions = { publicKey: PublicKeyCredentialCreationOptions };
export type ParsedRequestOptions = { publicKey: PublicKeyCredentialRequestOptions };

export function parseCreationOptions(input: unknown): ParsedCreationOptions {
	const { publicKey } = credentialCreationOptionsSchema.parse(input);
	return {
		publicKey: {
			rp: publicKey.rp,
			user: { ...publicKey.user, id: base64urlToBuffer(publicKey.user.id) },
			challenge: base64urlToBuffer(publicKey.challenge),
			pubKeyCredParams: publicKey.pubKeyCredParams,
			timeout: publicKey.timeout,
			excludeCredentials: publicKey.excludeCredentials?.map((credential) => ({
				type: credential.type,
				id: base64urlToBuffer(credential.id),
				transports: toTransports(credential.transports)
			})),
			authenticatorSelection: publicKey.authenticatorSelection,
			attestation: publicKey.attestation
		}
	};
}

export function parseRequestOptions(input: unknown): ParsedRequestOptions {
	const { publicKey } = credentialRequestOptionsSchema.parse(input);
	return {
		publicKey: {
			challenge: base64urlToBuffer(publicKey.challenge),
			timeout: publicKey.timeout,
			rpId: publicKey.rpId,
			allowCredentials: publicKey.allowCredentials?.map((credential) => ({
				type: credential.type,
				id: base64urlToBuffer(credential.id),
				transports: toTransports(credential.transports)
			})),
			userVerification: publicKey.userVerification
		}
	};
}

// navigator.credentials.create / .get の戻り値を構造的に受ける(実 PublicKeyCredential は
// この型に代入可能)。テストでは素のオブジェクトで代用できる — ブラウザ API のモック不要
export interface PublicKeyCredentialLike {
	id: string;
	rawId: ArrayBuffer;
	type: string;
	authenticatorAttachment?: string | null;
	getClientExtensionResults?: () => AuthenticationExtensionsClientOutputs;
	toJSON?: () => unknown;
	response: {
		clientDataJSON: ArrayBuffer;
		attestationObject?: ArrayBuffer;
		authenticatorData?: ArrayBuffer;
		signature?: ArrayBuffer;
		userHandle?: ArrayBuffer | null;
		getTransports?: () => string[];
	};
}

type SerializedCredential = {
	id: string;
	rawId: string;
	type: string;
	authenticatorAttachment?: string;
	clientExtensionResults: AuthenticationExtensionsClientOutputs;
	response: Record<string, unknown>;
};

// 登録(attestation)とログイン(assertion)の両方を扱う: response に在るものだけを
// base64url へ写す。userHandle は null をそのまま送らない(無いなら省く)
export function credentialToJSON(credential: PublicKeyCredentialLike): unknown {
	if (typeof credential.toJSON === 'function') return credential.toJSON();

	const response: Record<string, unknown> = {
		clientDataJSON: bufferToBase64url(credential.response.clientDataJSON)
	};
	const { attestationObject, authenticatorData, signature, userHandle } = credential.response;
	if (attestationObject) response.attestationObject = bufferToBase64url(attestationObject);
	const transports = credential.response.getTransports?.();
	if (transports && transports.length > 0) response.transports = transports;
	if (authenticatorData) response.authenticatorData = bufferToBase64url(authenticatorData);
	if (signature) response.signature = bufferToBase64url(signature);
	if (userHandle) response.userHandle = bufferToBase64url(userHandle);

	const json: SerializedCredential = {
		id: credential.id,
		rawId: bufferToBase64url(credential.rawId),
		type: credential.type,
		clientExtensionResults: credential.getClientExtensionResults?.() ?? {},
		response
	};
	if (credential.authenticatorAttachment) {
		json.authenticatorAttachment = credential.authenticatorAttachment;
	}
	return json;
}
