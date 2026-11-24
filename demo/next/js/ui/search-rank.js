// Adapted from Paradev commandPalette, Superset Inc. (MIT). See docs/design/licenses/paradev.txt.
const TITLE_EXACT = 100;
const TITLE_PREFIX = 90;
const TITLE_WORD_PREFIX = 80;
const TITLE_ALL_TOKENS = 70;
const TITLE_SUBSTRING = 60;
const KEYWORD_PREFIX = 50;
const ALL_TOKENS = 40;

function normalize(text) {
	return text.trim().normalize("NFD").replace(/\p{M}/gu, "").toLowerCase().replace(/\s+/g, " ");
}

function words(text) {
	return text.split(/[^\p{L}\p{N}]+/u).filter(Boolean);
}

function hasWordPrefix(wordList, prefix) {
	return wordList.some((word) => word.startsWith(prefix));
}

/**
 * 0 means no match. Matches are contiguous (whole query or whole query
 * tokens at word starts), never character subsequences: "po" must not hit
 * "Toggle theme" via "ap-p-earance col-o-r". Ties keep provider order.
 */
export function scoreText(entry, rawQuery) {
	const query = normalize(rawQuery);
	if (!query) return 1;

	const title = normalize(entry.title);
	if (title === query) return TITLE_EXACT;
	if (title.startsWith(query)) return TITLE_PREFIX;

	const titleWords = words(title);
	if (hasWordPrefix(titleWords, query)) return TITLE_WORD_PREFIX;

	const tokens = words(query);
	if (
		tokens.length > 0 &&
		tokens.every((token) => hasWordPrefix(titleWords, token))
	) {
		return TITLE_ALL_TOKENS;
	}

	if (title.includes(query)) return TITLE_SUBSTRING;

	const keywords = (entry.keywords ?? []).map(normalize);
	if (
		keywords.some(
			(keyword) =>
				keyword.startsWith(query) || hasWordPrefix(words(keyword), query),
		)
	) {
		return KEYWORD_PREFIX;
	}

	const allWords = [...titleWords, ...keywords.flatMap(words)];
	if (
		tokens.length > 0 &&
		tokens.every((token) => hasWordPrefix(allWords, token))
	) {
		return ALL_TOKENS;
	}

	return 0;
}

function isWordStart(text, index) {
	return index === 0 || !/[\p{L}\p{N}]/u.test(text[index - 1] ?? "");
}

function findWordPrefix(text, token) {
	let index = text.indexOf(token);
	while (index >= 0) {
		if (isWordStart(text, index)) return index;
		index = text.indexOf(token, index + 1);
	}
	return -1;
}

/**
 * Where `query` matches in `text`, as offsets into `text`: the whole query
 * as a substring, or else each of its words at the start of a word.
 */
export function matchRanges(text, query) {
	const needle = query.trim().toLowerCase();
	const haystack = text.toLowerCase();
	// Lowercasing can change a string's length ("İ"), and then its offsets
	// no longer point into `text`.
	if (!needle || haystack.length !== text.length) return [];

	const whole = haystack.indexOf(needle);
	if (whole >= 0) return [[whole, whole + needle.length]];

	const ranges = [];
	for (const token of needle.split(/\s+/)) {
		const start = findWordPrefix(haystack, token);
		if (start < 0) return [];
		ranges.push([start, start + token.length]);
	}
	ranges.sort((a, b) => a[0] - b[0]);
	const merged = [];
	for (const range of ranges) {
		const last = merged[merged.length - 1];
		if (last && range[0] <= last[1]) last[1] = Math.max(last[1], range[1]);
		else merged.push([range[0], range[1]]);
	}
	return merged;
}
