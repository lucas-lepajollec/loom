# UI data boundary implementation record — 2026-10-10

Durable contracts and consumer inventory now live in [architecture](architecture.md#ui-observation-boundaries); reproduction and crawl scenarios live in [UI validation](agents/ui-validation.md).

## Confirmed failure

The original `get()` parsed error bodies without rejecting HTTP failure.
`refreshLibrary()` published those bodies directly to `app.models` and
`app.presets`. A remote engine proxy's HTTP 502 response
`{ok:false,error:"remote engine unreachable"}` therefore replaced an array
with an object. Picker's `.filter()` threw during rendering, including while
the selector was closed. PageGuard hid the failed render but did not repair
the store. Palette, Local, Hub and Bench shared the same poisoned catalog.


Mission A validation passed UI checks, Linux binary build, Go vet, Windows
cross-build and documentation links. The full Go suite was attempted with
Go 1.26.9 and a temporary HOME: sandbox socket denial stopped
`TestHandleHubSearch` and `TestFetchReleaseChannels`; other packages completed.
The route crawl was attempted after building but the sandbox denied its first
loopback listener (`127.0.0.1`, EPERM), before browser launch. Therefore the
expanded crawl, real WebKit/iPhone behavior, physical microphone/audio and live
engine/harness recovery remain unverified here. Run the crawl and full Go suite
in a socket-capable environment before declaring browser acceptance complete.
