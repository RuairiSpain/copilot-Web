from __future__ import annotations

from conftest import FakeResponse, run_main, set_config
from tracker import announcements as ann
from tracker import common
from tracker.store import load_announcements

RSS = """<?xml version="1.0"?><rss version="2.0"><channel><title>Azure Updates</title>
<item><guid>u1</guid><title>Generally available: Agentic retrieval in Azure AI Search</title>
  <link>https://azure.microsoft.com/updates/u1</link><pubDate>Mon, 06 Oct 2026 10:00:00 GMT</pubDate>
  <category>Launched</category><category>Azure AI Search</category>
  <description>&lt;p&gt;Agentic retrieval is &lt;b&gt;now&lt;/b&gt; GA.&lt;/p&gt;</description></item>
<item><title>Retirement: gpt-4o 2024-05-13</title><link>https://azure.microsoft.com/updates/u2</link>
  <pubDate>Tue, 07 Oct 2026 10:00:00 -0000</pubDate><category>Retirements</category><category>Azure OpenAI Service</category></item>
<item><guid>old</guid><title>Old news</title><link>https://x/old</link><pubDate>Mon, 01 Sep 2026 10:00:00 GMT</pubDate></item>
<item><guid>nodate</guid><title>No date</title><link>https://x/nodate</link></item>
<item><title>No id or link</title><pubDate>Mon, 06 Oct 2026 10:00:00 GMT</pubDate></item>
<item><guid>u1</guid><title>Duplicate in the same fetch</title><pubDate>Mon, 06 Oct 2026 10:00:00 GMT</pubDate></item>
</channel></rss>"""

ATOM = """<?xml version="1.0" encoding="utf-8"?><feed xmlns="http://www.w3.org/2005/Atom"><title>Blog</title>
<entry><id>a1</id><title>Foundry Agent Service is now in public preview</title>
  <link rel="alternate" href="https://blog/a1"/><link rel="self" href="https://blog/a1.atom"/>
  <published>2026-10-08T09:00:00Z</published><category term="Announcements"/><category/>
  <summary>Short &amp;amp; sweet</summary></entry>
<entry><title>Breaking change in the Responses API</title><link rel="related" href="https://blog/a2"/>
  <updated>2026-10-09T09:00:00+02:00</updated><content>Body</content></entry>
<entry><title>No links</title><updated>2026-10-09T09:00:00Z</updated></entry>
</feed>"""


def test_strip_html_and_dates():
    assert ann.strip_html("<p>a &amp; <b>b</b></p>") == "a & b"
    assert ann.strip_html(None) == ""
    assert ann.strip_html("x" * 10, limit=5) == "xxxx…"
    assert ann.parse_date("Mon, 06 Oct 2026 10:00:00 GMT") == "2026-10-06T10:00:00+00:00"
    assert ann.parse_date("Tue, 07 Oct 2026 10:00:00 -0000") == "2026-10-07T10:00:00+00:00"
    assert ann.parse_date("2026-10-09T09:00:00+02:00") == "2026-10-09T07:00:00+00:00"
    assert ann.parse_date("") == "" and ann.parse_date(None) == "" and ann.parse_date("not a date") == ""


def test_parse_rss_and_atom():
    rss = ann.parse_feed(RSS)
    assert rss[0] == {"item_id": "u1", "title": "Generally available: Agentic retrieval in Azure AI Search",
                      "url": "https://azure.microsoft.com/updates/u1", "published": "2026-10-06T10:00:00+00:00",
                      "categories": ["Launched", "Azure AI Search"], "summary": "Agentic retrieval is now GA."}
    assert rss[1]["item_id"] == "https://azure.microsoft.com/updates/u2" and rss[1]["summary"] == ""
    atom = ann.parse_feed(ATOM)
    assert atom[0]["url"] == "https://blog/a1" and atom[0]["categories"] == ["Announcements"]
    assert atom[0]["summary"] == "Short & sweet"
    assert atom[1]["item_id"] == "https://blog/a2" and atom[1]["published"] == "2026-10-09T07:00:00+00:00"
    assert atom[1]["summary"] == "Body"
    assert atom[2]["url"] == "" and atom[2]["item_id"] == ""


def test_label_for():
    assert ann.label_for(["Compute", " In preview "], "x") == ("In preview", "Preview")
    assert ann.label_for([], "Action required: migrate") == ("", "Breaking change")
    assert ann.label_for([], "Retiring classic agents") == ("", "Retirement")
    assert ann.label_for([], "Now GA") == ("", "GA")
    assert ann.label_for([], "New preview SDK") == ("", "Preview")
    assert ann.label_for([], "Monthly update") == ("", "New")


def test_collect_and_conditional_requests(project, http, monkeypatch):
    http.add("GET", "https://www.microsoft.com/releasecommunications/",
             FakeResponse(200, RSS, {"ETag": '"v1"', "Last-Modified": "Mon, 06 Oct 2026 10:00:00 GMT"}),
             FakeResponse(304))
    http.add("GET", "https://devblogs.microsoft.com/foundry/feed/", FakeResponse(200, ATOM))
    assert run_main(ann, monkeypatch) == 0
    rows = {r["item_id"]: r for r in load_announcements()}
    assert set(rows) == {"u1", "https://azure.microsoft.com/updates/u2", "a1", "https://blog/a2"}
    assert rows["u1"]["products"] == "Azure AI Search" and rows["u1"]["label"] == "GA" and rows["u1"]["status"] == "Launched"
    assert rows["https://azure.microsoft.com/updates/u2"]["products"] == "Azure OpenAI"
    assert rows["https://azure.microsoft.com/updates/u2"]["label"] == "Retirement"
    assert rows["a1"]["products"] == "Microsoft Foundry" and rows["a1"]["label"] == "Preview"   # named in the title
    assert rows["https://blog/a2"]["products"] == "Microsoft Foundry"                          # the feed's default product
    state = common.load_json(project / "state" / "feeds.json")
    assert state["azure-updates"]["etag"] == '"v1"' and "etag" not in state["foundry-devblog"]

    # Second run: the Azure feed answers 304 to the conditional request; the blog repeats its items.
    assert run_main(ann, monkeypatch, "--only", "azure-updates") == 0
    sent = [c for c in http.calls if c["url"].startswith("https://www.microsoft.com")][-1]["headers"]
    assert sent["If-None-Match"] == '"v1"' and sent["If-Modified-Since"] == "Mon, 06 Oct 2026 10:00:00 GMT"
    assert run_main(ann, monkeypatch, "--only", "foundry-devblog") == 0
    assert len(load_announcements()) == 4


def test_failures_and_unknown_feed(project, http, monkeypatch):
    http.add("GET", "https://www.microsoft.com/releasecommunications/", FakeResponse(500))
    http.add("GET", "https://devblogs.microsoft.com/foundry/feed/", FakeResponse(200, "<rss><channel/></rss>"))
    assert run_main(ann, monkeypatch) == 1
    assert "azure-updates" not in common.load_json(project / "state" / "feeds.json")
    assert run_main(ann, monkeypatch, "--only", "nope") == 2


def test_github_discussions(project, http, monkeypatch):
    set_config(project, lambda c: c.update(feeds=[{"id": "d", "kind": "github-discussions", "owner": "o", "repo": "r",
                                                   "category": "Announcements", "product": "Microsoft Foundry"}]))
    assert run_main(ann, monkeypatch) == 0  # no token: skipped
    assert not any(c["url"].endswith("/graphql") for c in http.calls)
    monkeypatch.setenv("GITHUB_TOKEN", "t")
    nodes = [{"id": "D1", "title": "What's new in Foundry: October", "url": "https://gh/d1", "createdAt": "2026-10-02T00:00:00Z",
              "bodyText": "Highlights", "category": {"name": "Announcements"}},
             {"id": "D2", "title": "Question", "url": "https://gh/d2", "createdAt": "2026-10-02T00:00:00Z", "category": {"name": "Q&A"}},
             {"id": "D3", "title": "No category", "url": "https://gh/d3", "createdAt": "2026-10-02T00:00:00Z", "category": None}]
    http.add("POST", "https://api.github.com/graphql",
             FakeResponse(200, {"data": {"repository": {"discussions": {"nodes": nodes}}}}),
             FakeResponse(200, {"errors": [{"message": "Could not resolve to a Repository"}]}))
    assert run_main(ann, monkeypatch) == 0
    assert [r["item_id"] for r in load_announcements()] == ["D1"]
    assert http.calls[-1]["json"]["variables"] == {"owner": "o", "name": "r", "n": 30}
    assert run_main(ann, monkeypatch) == 1  # GraphQL error


def test_discussions_without_category_filter(project, http, monkeypatch):
    monkeypatch.setenv("GITHUB_TOKEN", "t")
    feed = {"id": "d", "kind": "github-discussions", "owner": "o", "repo": "r"}
    nodes = [{"id": "D3", "title": "T", "url": "u", "createdAt": "2026-10-02T00:00:00Z", "category": None}]
    http.add("POST", "https://api.github.com/graphql", FakeResponse(200, {"data": {"repository": {"discussions": {"nodes": nodes}}}}))
    items, validators = ann.fetch_feed(feed, {})
    assert [i["categories"] for i in items] == [[]] and validators == {}
