# Tasks and notifications (phase 4a)

Loom executes workspace and harness turns on the server, independently of SSE
subscribers. Closing every tab does not stop a turn or cancel an approval.
Harness turns have no cloud request timeout. Pending requests and completed
results use the existing discussion session records; there is no separate task
store. Stop, harness exit and server shutdown still cancel execution. A daemon
restart marks unfinished work interrupted; this slice does not resume processes
or durable checkpoints after a restart.

All notification channels are **off by default**, including existing Web Push
subscriptions. Defaults notify `task.waiting`, `task.failed`, `node.offline`, and
`task.completed` only when the turn lasts **more than 120 seconds**. The latter is
the first `task.completed → notify` automation. Optional quiet hours suppress
notifications, rather than delaying them. Set an IANA timezone for quiet hours;
an equal start/end means all day. Tests explicitly bypass rules and quiet hours.

Notifications carry a discussion title and status. The opt-in
`include_summaries` flag adds at most 120 characters of a request summary;
secret questions are generic. No full prompt, answer, tool output, raw provider
frame, credential or hidden reasoning is sent. Offered decision labels remain
visible on action buttons. Titles themselves may contain personal information.
The most recent update per task is retained during a burst. Delivery batches
are limited to one per configured rate window (10 seconds by default, bounded
to 64 tasks). Several tasks become a count-only digest linking to Tasks; open it
to see and answer all pending requests. Network errors are exposed by channel
in `last_errors`; delivery is best effort, with no persistent retry queue.

## ntfy on an iPhone

1. Install the [ntfy iOS app](https://docs.ntfy.sh/subscribe/phone/) and allow
   notifications. Add a topic on `https://ntfy.sh` or your own ntfy server.
2. Configure the same server and topic in Loom; enable the ntfy channel. Use a
   hard-to-guess topic or an authenticated reserved topic. Save an optional ntfy
   access token; it is sealed in Loom's existing installation secret store.
3. Set `public_base_url` to Loom's HTTPS URL **reachable from the phone**. A LAN
   address may work on your Wi-Fi but not on cellular; HTTP/LAN settings return
   `public_url_warning`. Loom derives an omitted base URL from the request Host
   on save. Behind a TLS proxy, enter the HTTPS URL explicitly.
4. Use the ntfy test endpoint, then close all Loom tabs and run a harness that
   asks for approval or a choice. The notification click link opens
   `<public_base_url>/#/chat/<discussion_id>`.

Approvals offer one HTTP action per option. A single non-secret, single-choice
question with up to three options offers one action per option. Free-text-only
questions, multiple questions, multi-select, secret inputs and forms offer
**Open**; answer them in the existing authenticated request UI. ntfy accepts
[at most three actions per message](https://docs.ntfy.sh/publish/#action-buttons),
so approvals with more options are split into several publishes in one delivery
batch. Client/platform action presentation varies: use the click link when the
installed phone client does not show the HTTP buttons. Self-hosted ntfy users
should follow its [iOS push configuration](https://docs.ntfy.sh/config/) for
background delivery; a subscription alone is not a delivery guarantee.

## Webhook

Set an HTTP(S) URL and optionally a shared secret. Each POST body is the redacted
domain event JSON, without an enclosing message object. `X-Loom-Signature` is
`sha256=<hex HMAC-SHA256(secret, exact body bytes)>`. With a configured secret,
`X-Loom-Secret` also contains that secret for receivers using a header check.
Use constant-time verification before trusting a request. An empty secret yields
an empty-key signature and provides no sender authentication. Redirects are not
followed. A burst digest has `count` and a status such as `3 task updates`, with
the representative event type; refresh Tasks for the individual updates.

## Web Push

Use HTTPS (or localhost for development), browser notification permission and
PushManager support. On iPhone, use a supported Home Screen web app. LAN HTTP
is only a preview and is not offered as Web Push. The settings client must
supply `?secure=${window.isSecureContext}` on settings and push requests and must
also check `window.isSecureContext`, `Notification` and `PushManager` itself.
`GET /api/notify` returns `secure:false`, an explanatory `reason`, and no public
key for insecure requests. When TLS terminates at a proxy, use Loom's existing
`LOOM_COOKIE_SECURE=1` deployment setting; a `secure=true` client report then
allows the HTTPS origin. `secure=false` always disables the offer.

The VAPID P-256 keypair is generated once and sealed in the existing secret
store; legacy keys migrate without changing existing subscriptions. ES256 JWT
signing uses the Go standard library. This slice deliberately sends a
**payload-free tickle** (RFC 8030), instead of implementing RFC 8291/8188 payload
encryption. The service worker wakes without a tab, fetches
`/api/notify/pending` with same-origin credentials and `cache:no-store`, shows a
notification with **Open** only, and navigates/focuses the discussion link.
Pending notices are an in-memory, bounded 24-hour view, never cached or persisted
as a separate notification inbox. Repeated wakes may redisplay the same tag.

A password login cookie is required to fetch protected pending notices after
all tabs close. The worker cannot access in-memory Bearer keys or the browser's
E2E relay session. If normal auth is unavailable or expired it shows a generic
“Open Loom” notification; opening Loom restores the usual authentication flow.
A different-origin public URL is not opened by the worker: it falls back to the
same-origin Tasks link. Push endpoints must be public HTTPS destinations;
private destinations, DNS rebinding and redirects are rejected. Expired
subscriptions (404/410) are removed. No conversation/API response or queued send
is added to PWA caching.

## Action capability security

Only `POST /api/notify/answer/<token>` accepts an action capability without a
session cookie. The HMAC-SHA256 signature binds the discussion, request,
**offered answer**, random nonce and expiry (24 hours by default, configurable
from 60 to 86400 seconds). The HTTP body cannot replace that answer. Tokens are
issued and consumed under a lock; one request resolution wins even with
simultaneous taps. Expired, tampered, replayed or wrong-request actions return
409. Other offered tokens become useless as soon as the live request resolves.
The normal API never treats a capability as a login, Bearer key or session.
Tokens cannot read a transcript or invoke another API, and vault locking blocks
request resolution. Restart revokes issuance and live request state.

Treat action URLs as credentials for that one offered decision: ntfy and anyone
with topic access can see them. Use HTTPS, protect topic access, and configure
reverse-proxy access logs to redact `/api/notify/answer/` paths. Loom never logs
tokens, notification credentials or receiver response bodies. Complex answers
continue to use `POST /api/workspace/sessions/{id}/requests/{request_id}` with
normal Loom authentication and the canonical RequestAnswer schema.

## Tasks UI contract

`GET /api/tasks` and each `data:` frame of `GET /api/tasks/stream` have the same
JSON shape. SSE sends an initial snapshot, lifecycle updates and a one-second
elapsed/activity refresh; a slow subscriber reconnects to a fresh snapshot.
Both endpoints require normal authentication and an unlocked vault. Times are
Unix milliseconds; elapsed is seconds. Active tasks come first, then up to 100
finished turns from the last 24 hours, newest first. Unknown historical start
or finish times are not fabricated; records without a usable time are omitted.
One task identifies one turn (`discussion_id:started_at`), not the discussion.

```json
{
  "ok": true,
  "tasks": [{
    "id": "d:1791540000000",
    "discussion_id": "d",
    "title": "Project work",
    "project_id": "p",
    "executor": "codex",
    "model": "native-model-id",
    "status": "waiting_input",
    "started_at": 1791540000000,
    "elapsed_seconds": 3600,
    "last_activity_at": 1791543600000,
    "last_activity_text": "Waiting for user",
    "steps_started": 3,
    "steps_completed": 2,
    "requests": [{"id":"codex:r","kind":"user_input","summary":"Choose a target"}]
  }],
  "recent_window_seconds": 86400,
  "recent_limit": 100
}
```

`status` is `running`, `waiting_input`, `waiting_approval`, `done`, `failed`, or
`cancelled`. `finished_at` is omitted for active turns and present for finished
ones. Requests are always an array; they contain only id/kind/summary. Fetch the
normal discussion endpoint for full canonical questions/options, then use the
existing request-answer endpoint. Step counts reflect observed tool/item
lifecycle events, not hidden reasoning or a predicted total. Step counts are
`null` for older records without phase 4 observations. Activity text is
an intentionally generic progress label, not an excerpt of the answer.

`GET /api/events?since=<decimal-id>` returns
`{"ok":true,"events":[],"latest":42,"gap":false}`. The ring retains 512 events.
IDs and the ring are process-local. `gap:true` means the cursor is too old or
beyond the current head; refresh Tasks. Event fields: `id`, `at`, `type`, `title`,
`status`; optional `task_id`, `discussion_id`, `machine_id`, `request_id`,
`request_kind`, `summary`, `duration_seconds`, `count`. Titles/summaries are
truncated to 120 characters. Types: `task.started`, `task.waiting`,
`task.resumed`, `task.completed`, `task.failed`, `node.offline`, `node.online`,
`engine.down`. Cancelled/unsaved turns use `task.failed` with their actual status;
restart interruptions appear as failed Tasks snapshots. Node transitions use existing observations plus a read-only
30-second paired-node ping owned by the server lifecycle. The first observation
establishes a baseline. Engine outages are emitted on changes in existing
engine health observations; no inference/model load is started by monitoring.

## Notifications settings UI contract

`GET /api/notify?secure=true` returns the following defaults before configuration.
`secure` and `reason` describe the page/deployment; `public_url_warning` is a
human-readable warning or empty string. `last_errors` is a map from channel to
sanitized delivery error, empty after success. The key is public, never private.

```json
{
  "ok": true,
  "config": {
    "public_base_url": "",
    "token_ttl_seconds": 86400,
    "ntfy": {"enabled":false,"server_url":"https://ntfy.sh","topic":"","token_set":false},
    "webhook": {"enabled":false,"url":"","secret_set":false},
    "push": {"enabled":false},
    "rules": {
      "events": ["task.waiting","task.completed","task.failed","node.offline"],
      "completed_after_seconds": 120,
      "include_summaries": false,
      "rate_seconds": 10,
      "quiet_hours": {"enabled":false,"start":"22:00","end":"07:00","timezone":"UTC"}
    }
  },
  "secure": true,
  "reason": "",
  "public_url_warning": "This LAN or HTTP address may be unreachable from your phone; use a reachable HTTPS address for links and actions",
  "push_public_key": "<base64url uncompressed P-256 public key>",
  "push_subscription_count": 0,
  "last_errors": {}
}
```

Save with `POST /api/notify?secure=true`, `Content-Type: application/json`, body
`{"config":<the complete config>,"ntfy_token":"optional replacement","webhook_secret":"optional replacement"}`.
Omit either secret field to preserve it; an empty string deletes it.
`token_set`/`secret_set` from clients are ignored. The response is the same
redacted envelope as GET. `token_ttl_seconds` is 60–86400, `rate_seconds` 1–3600,
and `completed_after_seconds` 0–86400. Push enablement requires secure context
and an HTTPS public base URL. There is no Settings page implementation in this
backend slice; these contracts are for the design owner's UI.

Tests: `POST /api/notify/test {"channel":"ntfy"}` (or `webhook`, `push`) returns
`{"ok":true}` on accepted delivery, 502 with a sanitized error on failure.
Testing is explicit and can send through a configured but disabled channel.
Push subscription: `POST /api/notify/push/subscribe?secure=true` accepts the
browser's `subscription.toJSON()` object:
`{"endpoint":"https://push-service/...","keys":{"p256dh":"...","auth":"..."}}`.
Unsubscribe accepts `{"endpoint":"https://push-service/..."}` at
`POST /api/notify/push/unsubscribe`; both return `{"ok":true}`. The historical
`/api/push/key`, `/api/push/subscribe`, `/api/push/unsubscribe` aliases remain.
`GET /api/notify/pending` returns `{"ok":true,"notifications":[]}` or items with
`title`, `body`, `url`, `tag`, `event`; action capabilities are never returned.

## Acceptance

Automated fixtures cover native and harness background completion, a fake ACP
approval with no subscribers, a cookie-free notification action followed by a
persisted result, and a cancelled browser subscription before a question opens.
They check unlimited harness lifetime, token binding/tamper/replay/expiry,
coalescing, signatures, payloads, secure-context reporting, stable sealed VAPID
keys, and worker deep links. A literal hour-long run on an installed harness,
actual iPhone delivery and TLS-proxy/push-service interoperability remain
real-platform acceptance checks; accelerated fixtures do not certify them.
