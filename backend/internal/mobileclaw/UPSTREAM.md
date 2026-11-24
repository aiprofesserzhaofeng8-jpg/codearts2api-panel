# China Mobile MobileClaw protocol

This is China Mobile's desktop product, not similarly named third-party mobile clients.

Sources inspected 2026-11-13:
- https://ecloud.10086.cn/mobilecloud/act/hotcase
- https://ecloud.10086.cn/op-help-center/doc/article/100814
- Installed official MobileClaw 1.9.8: renderer HTTP calls and local engine request construction. No proprietary client code or binaries are bundled here; the export helper reads encryption-format parameters from the user's installed application.

Personal access token authorization uses the fixed zhenze-huhehaote.cmecloud.cn origin. The observed endpoints are `/desktop/manager/user-subjects`, `/desktop/gate/model/modelList`, `/desktop/manager/subscriptions/getAiProviders`, `/desktop/manager/credits/v2/exp-credit`, and `/desktop/v1/chat/completions`. The free route requires `pkg-types: FREE`; missing this header returned HTTP 500 in the minimal test. Raw JSON request fields and stream chunks are preserved. No agent tools are executed by the gateway.

This first adapter supports only personal FREE entitlement. The catalog is intersected with active FREE subscriptions; paid/enterprise packages are not advertised. Calls don't perform extra catalog network requests. Unknown context/output limits and image/tool capability metadata remain unknown; numeric display values from the upstream catalog aren't assumed to be tokens. Model `auto` and other routed names must not be merged into a specific native model.

Credit `remainTokens` is a remaining free-token balance. In the observed response `totalTokens` was smaller than the remainder; its allocation-total semantics are unconfirmed, so it is not used to manufacture total/used values. No daily claim API is implemented or executed.

Export reads and decrypts only the user's selected local access token in memory. It writes a new mode-0600 credential file, excludes refresh tokens/system fingerprints, and never edits official-client files. No renewal is advertised: re-export from the official client when access expires. SMS login and independent refresh-token ownership remain future work.

2026-11-14: Added official QR flow: POST /auth/qrcode, /auth/qrstate and /auth/qrlogin under /desktop/manager. The first response supplies qrCodeUrl (fixed cmpassport.com scanAuth page), whitespace-wrapped PNG base64, qrId and expiresInSeconds (observed 120). Pending/scanned states are 0/1; confirmed 2 exchanges once; -1 expires. Repeated checks are locally paced. A unique authorized cloud subject can be bound through /auth/refresh using the transient refresh token, matching the official client; ambiguous subjects require official-client selection/import. Only verified access credentials reach the existing explicit-save preview. Transient refresh tokens are not persisted. Real generation and pending polling passed; phone confirmation and account-binding responses were tested using fixtures, not claimed as live verified.

2026-11-14 follow-up: the user reported successful phone confirmation using the corresponding phone's cellular network. Newly created panel QR sessions now retain their own refreshToken and device metadata. POST /desktop/manager/auth/refresh uses that token as Bearer and the same deviceId/hostname/mac/clientVersion/os plus userId and aiProvider as in the installed 1.9.8 renderer. The returned accessToken/refreshToken replace the old pair through the existing serialized credential coordinator. The desktop export still excludes refresh material. No extra timer, desktop writes, or replay of refresh POSTs. The earlier transient-only statement describes the prior implementation. Real rotation remains unverified; fixtures cover it.

2026-11-24 search: installed desktop engine uses GET `/desktop/search/v2/mwebsearch` with q, ref_prom=aiso-sr, sid, count and personal Bearer authorization plus service-version 2.6.1. A user-authorized live request returned HTTP 200, errno 0 and items with title/url/summary_ai/summary. The adapter implements that native endpoint only; the desktop's DuckDuckGo/Exa fallback chain is not used. No proprietary code is bundled.
