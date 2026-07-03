import { error } from '@sveltejs/kit';
import { getArticle } from '$lib/server/core';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ fetch, params }) => {
	const id = Number(params.id);
	if (!Number.isInteger(id) || id < 1) {
		error(404, '記事が見つかりません');
	}
	const article = await getArticle(fetch, id);
	if (!article) {
		error(404, '記事が見つかりません');
	}
	return { article };
};
