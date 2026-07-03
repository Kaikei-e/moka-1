"""評価データセット — 公開RSSからの収集(≥5s/req)、manifest固定、リキャップ週合成.

本文 JSON(art-*.json)は著作権のため gitignore。manifest.json(URL+SHA-256)のみコミット。
"""

import hashlib
import json
import time
import urllib.robotparser
import xml.etree.ElementTree as ET
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Literal

import httpx
from pydantic import BaseModel

from moka_eval.config import ARTICLES_DIR, DATA_DIR

USER_AGENT = "moka-eval/0.1 (model-eval dataset builder; one-shot, rate-limited)"
MIN_REQUEST_INTERVAL_S = 5.0

type Lang = Literal["ja", "en"]
type Kind = Literal["jp_tech", "jp_general", "en"]


@dataclass(frozen=True)
class Feed:
    url: str
    kind: Kind
    lang: Lang


FEEDS: tuple[Feed, ...] = (
    Feed("https://www.publickey1.jp/atom.xml", "jp_tech", "ja"),
    Feed("https://gigazine.net/news/rss_2.0/", "jp_tech", "ja"),
    Feed("https://rss.itmedia.co.jp/rss/2.0/news_bursts.xml", "jp_tech", "ja"),
    Feed("https://zenn.dev/feed", "jp_tech", "ja"),
    Feed("https://gihyo.jp/feed/rss2", "jp_tech", "ja"),
    # NHKは記事ページが403(news.web.nhkへのリダイレクト先で拒否)のためYahoo!トピックスを使う
    Feed("https://news.yahoo.co.jp/rss/topics/top-picks.xml", "jp_general", "ja"),
    Feed("https://news.yahoo.co.jp/rss/topics/domestic.xml", "jp_general", "ja"),
    Feed("https://feeds.arstechnica.com/arstechnica/index", "en", "en"),
    Feed("https://feeds.bbci.co.uk/news/technology/rss.xml", "en", "en"),
)

QUOTAS: dict[Kind, int] = {"jp_tech": 20, "jp_general": 5, "en": 5}
MIN_CHARS = 500
MAX_CHARS = 8000


class Article(BaseModel):
    id: str
    url: str
    domain: str
    lang: Lang
    kind: Kind
    title: str
    text: str
    chars: int
    sha256: str
    fetched_at: str


class FeedItem(BaseModel):
    """リキャップ週合成用の軽量エントリ(フィードの要旨のみ、本文フェッチなし)."""

    title: str
    summary: str
    url: str
    lang: Lang


class ManifestEntry(BaseModel):
    id: str
    url: str
    domain: str
    lang: Lang
    kind: Kind
    title: str
    chars: int
    sha256: str
    fetched_at: str


class DatasetError(RuntimeError):
    """データセット収集・検証の失敗."""


class RateLimiter:
    """全HTTPリクエスト共通の最小間隔を守る(tenets §8-5)."""

    def __init__(self, interval_s: float = MIN_REQUEST_INTERVAL_S) -> None:
        self._interval = interval_s
        self._last = 0.0

    def wait(self) -> None:
        now = time.monotonic()
        remaining = self._last + self._interval - now
        if remaining > 0:
            time.sleep(remaining)
        self._last = time.monotonic()


class PoliteFetcher:
    """UA明示 + robots.txt 尊重 + レートリミット."""

    def __init__(self, limiter: RateLimiter) -> None:
        self._limiter = limiter
        self._http = httpx.Client(
            headers={"User-Agent": USER_AGENT},
            timeout=httpx.Timeout(30.0, connect=10.0),
            follow_redirects=True,
        )
        self._robots: dict[str, urllib.robotparser.RobotFileParser] = {}

    def close(self) -> None:
        self._http.close()

    def allowed(self, url: str) -> bool:
        origin = httpx.URL(url)
        base = f"{origin.scheme}://{origin.host}"
        parser = self._robots.get(base)
        if parser is None:
            parser = urllib.robotparser.RobotFileParser()
            try:
                self._limiter.wait()
                resp = self._http.get(f"{base}/robots.txt")
                if resp.status_code == 200:
                    parser.parse(resp.text.splitlines())
                else:
                    parser.allow_all = True
            except httpx.HTTPError:
                parser.allow_all = True
            self._robots[base] = parser
        return parser.can_fetch(USER_AGENT, url)

    def get(self, url: str) -> httpx.Response:
        self._limiter.wait()
        resp = self._http.get(url)
        resp.raise_for_status()
        return resp


def _text_of(elem: ET.Element | None) -> str:
    return (elem.text or "").strip() if elem is not None else ""


def parse_feed(xml_text: str) -> list[tuple[str, str, str]]:
    """RSS2/Atom両対応で (title, link, summary) を返す."""
    root = ET.fromstring(xml_text)  # noqa: S314 — 収集元は自明な公開フィードのみ
    entries: list[tuple[str, str, str]] = []
    # RSS 2.0 / RSS 1.0(RDF): 名前空間の有無に関わらず item 要素を拾う
    for item in root.iter():
        if item.tag != "item" and not item.tag.endswith("}item"):
            continue
        ns = item.tag.removesuffix("item")
        title = _text_of(item.find(f"{ns}title"))
        link = _text_of(item.find(f"{ns}link"))
        summary = _text_of(item.find(f"{ns}description"))
        if title and link:
            entries.append((title, link, summary))
    # Atom
    ns = {"a": "http://www.w3.org/2005/Atom"}
    for entry in root.findall("a:entry", ns):
        title = _text_of(entry.find("a:title", ns))
        link_elem = entry.find("a:link", ns)
        link = link_elem.get("href", "") if link_elem is not None else ""
        summary = _text_of(entry.find("a:summary", ns)) or _text_of(entry.find("a:content", ns))
        if title and link:
            entries.append((title, link, summary))
    return entries


def _extract_text(html: str, url: str) -> str | None:
    try:
        import trafilatura  # pyrefly: ignore[missing-import]  # dataset グループ限定の依存
    except ImportError as err:
        msg = "trafilatura required: run with `uv run --group dataset ...`"
        raise DatasetError(msg) from err
    return trafilatura.extract(html, url=url, include_comments=False)


def _now_iso() -> str:
    return datetime.now(UTC).isoformat(timespec="seconds")


def fetch_dataset(*, per_feed_limit: int = 12) -> tuple[list[Article], list[FeedItem]]:
    """フィード横断で QUOTAS を満たすまで記事本文を収集する。失敗はスキップ."""
    limiter = RateLimiter()
    fetcher = PoliteFetcher(limiter)
    articles: list[Article] = []
    feed_items: list[FeedItem] = []
    counts: dict[Kind, int] = {"jp_tech": 0, "jp_general": 0, "en": 0}
    try:
        candidates: list[tuple[Feed, str, str]] = []
        for feed in FEEDS:
            try:
                resp = fetcher.get(feed.url)
                entries = parse_feed(resp.text)
            except (httpx.HTTPError, ET.ParseError) as err:
                print(f"skip feed {feed.url}: {err}")
                continue
            for title, link, summary in entries[:per_feed_limit]:
                candidates.append((feed, title, link))
                if summary:
                    feed_items.append(
                        FeedItem(title=title, summary=summary[:400], url=link, lang=feed.lang)
                    )
        # ラウンドロビン気味に、クォータ未達の kind から取得
        for feed, title, link in candidates:
            if counts[feed.kind] >= QUOTAS[feed.kind]:
                continue
            if not fetcher.allowed(link):
                print(f"robots disallow: {link}")
                continue
            try:
                page = fetcher.get(link)
            except httpx.HTTPError as err:
                print(f"skip {link}: {err}")
                continue
            text = _extract_text(page.text, link)
            if text is None or not (MIN_CHARS <= len(text) <= MAX_CHARS):
                continue
            index = len(articles)
            articles.append(
                Article(
                    id=f"art-{index:03d}",
                    url=link,
                    domain=httpx.URL(link).host or "",
                    lang=feed.lang,
                    kind=feed.kind,
                    title=title,
                    text=text,
                    chars=len(text),
                    sha256=hashlib.sha256(text.encode()).hexdigest(),
                    fetched_at=_now_iso(),
                )
            )
            counts[feed.kind] += 1
            print(f"[{sum(counts.values()):02d}] {feed.kind} {link} ({len(text)} chars)")
            if all(counts[k] >= QUOTAS[k] for k in QUOTAS):
                break
    finally:
        fetcher.close()
    return articles, feed_items


def save_dataset(articles: list[Article], feed_items: list[FeedItem]) -> None:
    ARTICLES_DIR.mkdir(parents=True, exist_ok=True)
    for article in articles:
        path = ARTICLES_DIR / f"{article.id}.json"
        path.write_text(article.model_dump_json(indent=2), encoding="utf-8")
    manifest = [
        ManifestEntry(**article.model_dump(exclude={"text"})).model_dump() for article in articles
    ]
    (ARTICLES_DIR / "manifest.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2), encoding="utf-8"
    )
    (DATA_DIR / "feeditems.json").write_text(
        json.dumps([i.model_dump() for i in feed_items], ensure_ascii=False, indent=2),
        encoding="utf-8",
    )


def load_articles() -> list[Article]:
    paths = sorted(ARTICLES_DIR.glob("art-*.json"))
    if not paths:
        msg = "no articles found; run `moka-eval dataset fetch` first"
        raise DatasetError(msg)
    return [Article.model_validate_json(p.read_text(encoding="utf-8")) for p in paths]


def verify_dataset() -> list[str]:
    """manifest と本文のハッシュ整合を確認。問題を文字列で返す(空=OK)."""
    manifest_path = ARTICLES_DIR / "manifest.json"
    if not manifest_path.is_file():
        return ["manifest.json missing"]
    problems: list[str] = []
    entries = [
        ManifestEntry.model_validate(e)
        for e in json.loads(manifest_path.read_text(encoding="utf-8"))
    ]
    for entry in entries:
        path = ARTICLES_DIR / f"{entry.id}.json"
        if not path.is_file():
            problems.append(f"{entry.id}: body file missing")
            continue
        article = Article.model_validate_json(path.read_text(encoding="utf-8"))
        actual = hashlib.sha256(article.text.encode()).hexdigest()
        if actual != entry.sha256:
            problems.append(f"{entry.id}: sha256 mismatch")
    return problems


class RecapWeek(BaseModel):
    week_id: str
    entries: list[FeedItem]


def build_recap_weeks(*, n_weeks: int = 3, per_week: int = 18) -> list[RecapWeek]:
    """feeditems から決定的に n 週分を合成して凍結する."""
    items_path = DATA_DIR / "feeditems.json"
    if not items_path.is_file():
        msg = "feeditems.json missing; run `moka-eval dataset fetch` first"
        raise DatasetError(msg)
    items = [FeedItem.model_validate(i) for i in json.loads(items_path.read_text(encoding="utf-8"))]
    needed = n_weeks * per_week
    if len(items) < needed:
        msg = f"need {needed} feed items, have {len(items)}"
        raise DatasetError(msg)
    weeks = [
        RecapWeek(week_id=f"week-{w + 1}", entries=items[w * per_week : (w + 1) * per_week])
        for w in range(n_weeks)
    ]
    for week in weeks:
        path = ARTICLES_DIR / f"recap-{week.week_id}.json"
        path.write_text(week.model_dump_json(indent=2), encoding="utf-8")
    return weeks


def load_recap_weeks() -> list[RecapWeek]:
    paths = sorted(ARTICLES_DIR.glob("recap-week-*.json"))
    if not paths:
        msg = "no recap weeks; run `moka-eval dataset recap` first"
        raise DatasetError(msg)
    return [RecapWeek.model_validate_json(p.read_text(encoding="utf-8")) for p in paths]
