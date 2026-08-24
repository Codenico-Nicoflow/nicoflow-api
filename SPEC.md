# Nicoflow SPEC (index)

> **Canonical product, PRD, architecture, and engineering docs live in Confluence space `NI`.**
> This file holds only the code-canonical API contract (§3) and error codes (§4) for offline and skill use.
> Do not hand-edit §3/§4 here — regenerate from the code after running `contract-check`.

---

## Product, PRDs & Architecture → Confluence

| Section                                                                                                                 | Confluence link                                                               |
| ----------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| 1. Product Overview (What is Nicoflow, Hierarchy, Plan Tiers, Roadmap)                                                  | [Confluence §1](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/21037057) |
| 2. Product Requirements / PRDs (E-001 … E-037)                                                                          | [Confluence §2](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/21200935) |
| 3. Architecture & Technical Design (System, DB Schema, Backend, Frontend, Auth, WS, S3, Billing, Mobile, Design System) | [Confluence §3](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/21037111) |
| 5. Engineering Operations (Local Dev, CI/CD, Branching, Deployment, Env Vars, Migrations, Testing)                      | [Confluence §5](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/21070021) |
| 6. Architecture Decision Records (ADR-001 … ADR-006)                                                                    | [Confluence §6](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/21561366) |
| 7. Sprints (Overview + Sprint 01 … 25)                                                                                  | [Confluence §7](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/21168185) |

> **Confluence space NI** · cloudId `ef5c2411-b64a-429c-a200-17ea853e32ce` · space id `425986`
> Full page tree with ids: `.claude/skills/spec-sync/references/confluence-map.md`

---

## §3 API Contract (code-canonical, verified by contract-check)

## 3. API Endpoint Reference

**Base URL:** `https://api.nicoflow.app/v1/`
**Local dev:** `http://localhost:8080/v1/`

All authenticated endpoints require `Authorization: Bearer <jwt>` header.
All request and response bodies are `application/json`.

**Interactive docs (Swagger):** the authentication & user-management surface is annotated with swaggo and served at `GET /v1/swagger/index.html` (spec JSON at `/v1/swagger/doc.json`) in non-production environments. Regenerate from the handler annotations with `make swagger`.

**Postman collection:** the full API surface lives in `docs/postman/Nicoflow-API.postman_collection.json` (v2.1) with three environments — `Nicoflow-Local` (`http://localhost:8080`), `Nicoflow-Staging` (`https://nicoflow-api-staging.onrender.com`), `Nicoflow-Production` (`https://api.nicoflow.app`). Import the collection + one environment, select it, and run **Auth › Login** first — its test script captures `accessToken` into a collection variable so every protected request works; create requests auto-capture `areaId`/`projectId`/`taskId`/`subtaskId` for chaining. The `baseUrl` must **not** include `/v1` (the collection paths already carry it; a `/v1` in `baseUrl` produces `/v1/v1/...` → 404). Folders tagged **⚠ STUB 501** hit routes whose handlers return `501 not implemented` (bucket, search, attachments, ai, billing, notifications). **Keep it in sync: whenever a feature adds, removes, or changes an endpoint (or a request/response DTO), update this collection in the same PR** — add the new request, drop the STUB tag once a domain lands, and correct the body. Verify each request against the live route table (`grep -rn '\.\(Get\|Post\|Patch\|Put\|Delete\)("' internal cmd`) before committing.

---

### 3.1 Authentication & Users

#### POST /v1/auth/register

Create a new user account.

- **Auth required:** No
- **Plan required:** Any

**Request body**

```json
{
  "email": "user@example.com",
  "password": "Secret1234",
  "username": "johndoe",
  "platform": "web"
}
```

| Field      | Type   | Required | Constraints                                 |
| ---------- | ------ | -------- | ------------------------------------------- |
| `email`    | string | Yes      | Valid email format                          |
| `password` | string | Yes      | 8–72 chars, ≥1 uppercase, ≥1 lowercase      |
| `username` | string | Yes      | 3–20 chars, alphanumeric only               |
| `platform` | string | No       | `"web"` \| `"mobile"` — defaults to `"web"` |

> **Email verification:** registration does **not** log the user in. The API creates the user, issues an email-verification token, and (if SMTP is configured) sends a verification link. The response carries the **user only — no tokens and no refresh cookie**. The user must verify via `POST /v1/auth/verify-email` (resend via `POST /v1/auth/resend-verification`), then log in. Login enforcement of `email_verified` is gated by the server config `REQUIRE_EMAIL_VERIFICATION` (default false in dev where no SMTP is configured; true in staging/production).

**Response — 201 Created** (user only — no tokens, no Set-Cookie)

```json
{
  "token": "",
  "refreshToken": "",
  "user": {
    "id": "usr_abc123",
    "email": "...",
    "username": "...",
    "firstName": "...",
    "lastName": "...",
    "theme": "light",
    "language": "en",
    "imageUrl": "",
    "status": "regular"
  }
}
```

**Errors**

| Code                      | HTTP | Meaning                                 |
| ------------------------- | ---- | --------------------------------------- |
| `EMAIL_ALREADY_EXISTS`    | 409  | Email already in use                    |
| `USERNAME_ALREADY_EXISTS` | 409  | Username already taken                  |
| `INVALID_EMAIL`           | 422  | Email failed format validation          |
| `WEAK_PASSWORD`           | 400  | Password fails the policy above         |
| `INVALID_INPUT`           | 422  | Other validation failed (e.g. username) |
| `RATE_LIMITED`            | 429  | Too many registration attempts          |

---

#### POST /v1/auth/login

Authenticate and receive tokens.

- **Auth required:** No

**Request body**

```json
{
  "identifier": "user@example.com",
  "password": "Secret1234",
  "remember": true,
  "platform": "web",
  "timezone": "Europe/London"
}
```

| Field        | Type    | Required | Notes                                                           |
| ------------ | ------- | -------- | --------------------------------------------------------------- |
| `identifier` | string  | Yes      | Email address **or** username. (Legacy `email` still accepted.) |
| `password`   | string  | Yes      |                                                                 |
| `remember`   | boolean | Yes      | `true` → 7-day refresh token; `false` → 24-hour                 |
| `platform`   | string  | No       | `"web"` \| `"mobile"`                                           |
| `timezone`   | string  | No       | Client IANA zone. Best-effort self-heal: if valid and different from the stored value, the user's `timezone` is updated (an invalid/absent value is ignored — never fails the login). Lets an existing `'UTC'`-default row correct itself on next login. |

**Response — 200 OK**

```json
{
  "token": "<jwt>",
  "refreshToken": "<refresh-jwt>",
  "user": { ...IUser }
}
```

**Errors:** `UNAUTHORIZED` (401, invalid credentials — identical for unknown user and wrong password, by design, to prevent account enumeration), `EMAIL_NOT_VERIFIED` (403 — credentials valid but email unverified; only when `REQUIRE_EMAIL_VERIFICATION` is enabled; returned after the password check so it never leaks verification state to a wrong password), `INVALID_INPUT` (422), `RATE_LIMITED` (429 — IP rate limit or account lockout after repeated failures)

---

#### POST /v1/auth/refresh-token

Exchange a refresh token for a new access token. Refresh token is read from the `HttpOnly` cookie or the request body.

- **Auth required:** No (uses refresh token)

**Response — 200 OK**

```json
{
  "token": "<jwt>",
  "refreshToken": "<refresh-jwt>",
  "user": { ...IUser }
}
```

**Errors:** `INVALID_TOKEN` (401 — missing, malformed, expired, already-consumed, or tampered refresh token; on a detected reuse all of the user's refresh tokens are revoked)

> **Refresh token (dual-hash rotation):** 32 random bytes → 64-char hex raw token, returned to the client and set as an `HttpOnly` cookie. The DB stores `SHA-256(raw)` (fingerprint, O(1) lookup) and `bcrypt(raw)` (tamper check). Each refresh atomically deletes the old row and inserts a new one (single-use rotation); 0 rows deleted ⇒ reuse ⇒ revoke all. Cookie: `HttpOnly; Secure (prod); SameSite=Strict; Path=/v1/auth; Max-Age` 7 days (`remember=true`) or 24 h.

> **JWT (access token):** HS256, default 15-min TTL, claims `{ sub, email, plan: "free"|"pro", iss: "nicoflow-api", iat, exp }`. Plan is read from the claim — no per-request DB lookup.

---

#### POST /v1/auth/logout

Invalidate the current session (deletes the single refresh token carried by the cookie).

- **Auth required:** No — authenticates off the HttpOnly refresh cookie (`Path=/v1/auth`, `SameSite=Strict`), not the access token, so an expired JWT can't trap the user in a session they can't end. Idempotent: a missing or already-deleted token still returns 204.

**Response — 204 No Content**

---

#### POST /v1/auth/logout-all

Revoke **every** refresh token for the authenticated user (sign out of all devices).

- **Auth required:** Yes — revokes by the `userID` JWT claim; needs a live, valid session to authorize.

**Response — 204 No Content**

> **Frontend:** wired — the `useLogoutAllMutation` hook calls this endpoint and a "Sign out of all devices" affordance lives in the sidebar user menu (clears the session and redirects to sign-in). The underlying revoke-all logic is shared with the password-change and delete-account flows. A dedicated Profile/Security home for it can follow in E-021.

---

#### POST /v1/auth/verify-email

Confirm a user's email address using the token from the verification email. _(Login enforcement is gated by the `REQUIRE_EMAIL_VERIFICATION` config flag — when enabled, unverified accounts are rejected at login with `EMAIL_NOT_VERIFIED`.)_

- **Auth required:** No

**Request body**

```json
{ "token": "<raw-verification-token>" }
```

**Response — 200 OK** · **Errors:** `INVALID_TOKEN` (401 — invalid, expired, or already-used token), `INVALID_INPUT` (422), `RATE_LIMITED` (429)

---

#### POST /v1/auth/resend-verification

Re-send the email-verification link. Always returns 200 (no user enumeration).

- **Auth required:** No

**Request body**

```json
{ "email": "user@example.com" }
```

**Response — 200 OK** · **Errors:** `RATE_LIMITED` (429)

---

#### POST /v1/auth/forgot-password

Send a password-reset email.

- **Auth required:** No

**Request body**

```json
{ "email": "user@example.com" }
```

**Response — 200 OK** (always 200 to prevent user enumeration)

**Errors:** `RATE_LIMITED` (429)

---

#### POST /v1/auth/reset-password

Set a new password using the reset token.

- **Auth required:** No

**Request body**

```json
{
  "newPassword": "NewSecret1234",
  "confirmPassword": "NewSecret1234",
  "token": "<reset-token-from-email>"
}
```

**Response — 200 OK**

**Errors:** `INVALID_TOKEN` (401), `INVALID_TOKEN` (401), `INVALID_INPUT` (422)

---

#### POST /v1/auth/change-password

Change the password for the **currently logged-in** user (distinct from `reset-password`, which is the forgot-flow via an emailed token).

- **Auth required:** Yes (JWT)
- **Rate limit:** stricter per-user bucket (~5/min) on top of the global user limiter — the endpoint bcrypt-compares `currentPassword`, so it must not be an online password oracle.

**Request body**

```json
{
  "currentPassword": "<current-password>",
  "newPassword": "<new-password>",
  "confirmPassword": "<new-password>"
}
```

- `currentPassword` is re-verified via bcrypt; a mismatch → `UNAUTHORIZED` (401).
- `newPassword` policy is identical to register: 8–72 chars, ≥1 uppercase + ≥1 lowercase, validated server-side. `confirmPassword` must match.
- `newPassword` must **differ from the current password** — a no-op change is rejected with `INVALID_INPUT` (422). Checked only after the current-password verify passes, so it never leaks whether a guessed value matches the stored password.

**Response — 200 OK** — a fresh `{ token, refreshToken, user }` pair (same shape as login), plus a rotated `HttpOnly` refresh cookie. On success **all** of the user's refresh tokens are revoked and only the calling client is re-issued a pair — every other device is signed out while the changer stays signed in. A "password changed" notification email is sent best-effort (Mailtrap-only until E-044).

**Errors:** `UNAUTHORIZED` (401 — missing access token or wrong current password), `WEAK_PASSWORD` (400), `INVALID_INPUT` (422 — password mismatch **or** new password equals current), `RATE_LIMITED` (429)

---

#### GET /v1/users/profile

Retrieve the authenticated user's profile.

- **Auth required:** Yes

**Response — 200 OK**

```json
{
  "id": "01J...",
  "email": "user@example.com",
  "username": "johndoe",
  "firstName": "John",
  "lastName": "Doe",
  "theme": "light",
  "language": "en",
  "timezone": "Europe/London",
  "imageUrl": "https://...",
  "plan": "free",
  "calendar": {
    "weekStart": 1,
    "workdays": [0, 1, 2, 3, 4, 5, 6],
    "dayStartHour": 0,
    "dayEndHour": 24
  }
}
```

`timezone` is the user's IANA zone (drives when proactive-notification sweeps fire — see §3.12). Also echoed by `PATCH /v1/users/me`.

`calendar` (NIC-1890) is how the user wants the calendar grid drawn. It travels on **every** auth response — login, register, refresh, profile — because the grid needs it on first paint; a second round trip would render one frame of the wrong week. `workdays` is always an array, never `null`.

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `weekStart` | `0`–`6` | `1` (Mon) | First column of the week. **0 = Sunday**, matching JS `getDay()` and Go `time.Weekday`, so no consumer needs a translation table. |
| `workdays` | `number[]` | all seven | Which days the grid draws, same 0–6 encoding. Never empty. |
| `dayStartHour` | `0`–`23` | `0` | First hour drawn. |
| `dayEndHour` | `1`–`24` | `24` | Last hour drawn, **exclusive**. |

Three things about this shape that are load-bearing:

- **These live on `users`, not in a calendar-only table.** `weekStart` is not purely display: the notification sweeps (§3.12) and any future "this week" grouping key off a week boundary, and a second source of truth would let the backend and the grid disagree about which week a task is in.
- **`workdays` is a set, not a `workdaysOnly` boolean.** The work week is Mon–Fri across most of Europe but **Sun–Thu in Israel**, and the product ships Hebrew + RTL (§10) — a boolean cannot express both.
- **`dayEndHour` is exclusive with a ceiling of 24.** "08:00 to midnight" is the case the preference exists for, and an inclusive `23` cannot express it. `24` means "through midnight", not "00:00".

The defaults reproduce the pre-NIC-1890 behaviour exactly, so an existing user sees no change until they choose one. The hour window is a **default view, never a filter**: clients must widen the drawn range to include anything actually scheduled outside it (tasks *and* Google events), because a display setting that silently hides scheduled work is indistinguishable from losing it.

**Errors:** `UNAUTHORIZED` (401)

---

#### PATCH /v1/users/me

Update user profile fields.

- **Auth required:** Yes

**Request body** (all fields optional)

```json
{
  "firstName": "John",
  "lastName": "Doe",
  "timezone": "Europe/London",
  "theme": "dark",
  "language": "he",

  "weekStart": 0,
  "workdays": [0, 1, 2, 3, 4],
  "dayStartHour": 8,
  "dayEndHour": 18
}
```

The four calendar fields (NIC-1890) are **flat on the request**, not nested under a `calendar` object, and each is independently optional — a client changing only the day window never has to echo back a `weekStart` it did not read. They are echoed back **nested**, inside the `calendar` object of the returned `IUser`.

`email` and `username` are **immutable via this path** and are ignored if sent (an unknown `email` field is silently dropped at decode). Allowing email change here is an account-takeover vector — combined with the unauthenticated forgot-password flow, an attacker with a live session could change the address to their own and reset. Correct email change (verify-new + notify-old + password re-auth) is a separate security-reviewed epic; `username` is a login credential.

`language` must be one of `en`, `he`, `ru` (validated in the service layer → `INVALID_INPUT` otherwise). Drives the UI language for logged-in users (and, later, localized emails). See §10.

`timezone` must be a valid IANA name resolvable by `time.LoadLocation` (e.g. `Europe/London`); an invalid value (garbage, an offset like `UTC+3`, or empty) returns `INVALID_INPUT` (422) and the stored value is left unchanged — it is **never** silently coerced to UTC, since a wrong stored zone makes every reminder fire at the wrong hour. `'UTC'` remains the column default for a brand-new row; that is distinct from overwriting explicit input. The client sends `Intl.DateTimeFormat().resolvedOptions().timeZone` on every login, so an existing `'UTC'` row self-heals on next login.

**Calendar preference validation** — each rule returns `INVALID_INPUT` (422) and leaves the stored value unchanged. The columns carry matching `CHECK` constraints, but those are a backstop: a constraint violation surfaces as a 500 carrying a Postgres error string, which no client can act on.

- `weekStart` outside `0`–`6`.
- `workdays` empty, containing a value outside `0`–`6`, or containing a duplicate. Empty is rejected rather than meaning "hide everything" — a calendar with no days is a blank screen the user has no way to navigate back out of. Duplicates mean the client built the set wrongly, and collapsing them silently hides that.
- `dayStartHour` outside `0`–`23`, or `dayEndHour` outside `1`–`24`.
- `dayStartHour >= dayEndHour` — **including when only one end is sent**. A request carrying `dayStartHour: 20` against a stored `dayEndHour: 18` passes every per-field check and would land an empty window, so the absent end is resolved against the stored value before the comparison.

**Response — 200 OK** — Updated `IUser` object (echoes `timezone` and `calendar`)

---

#### PATCH /v1/users/me (push token)

Register a device push notification token (mobile). Pass `pushToken` and `platform` in the same PATCH body.

- **Auth required:** Yes

**Request body**

```json
{ "pushToken": "<expo-push-token>", "platform": "ios" }
```

**Response — 200 OK**

---

### 3.2 Areas

> **`IArea` shape** — all IDs are strings (UUID). Fields: `id: string`, `name: string`, `color: string`, `icon?: IconId`, `displayOrder?: number`, `createdAt: string`, `updatedAt: string`. `userId` is not returned on the wire.

#### GET /v1/areas

List areas for the authenticated user. Cursor-paginated.

- **Auth required:** Yes
- **Query params:** `q` (search), `limit` (1–100, default 50), `cursor` (opaque base64 page token)

**Response — 200 OK**

```json
{
  "items": [
    {
      "id": "01J...",
      "name": "Work",
      "color": "#3B82F6",
      "icon": "folder",
      "displayOrder": 0,
      "createdAt": "2026-01-01T00:00:00Z",
      "updatedAt": "2026-01-01T00:00:00Z"
    }
  ],
  "nextCursor": "MTo..."
}
```

`nextCursor` is `""` when there are no more pages.

---

#### GET /v1/areas/with-projects

List all areas with their nested projects (no pagination — returns full set).

- **Auth required:** Yes

**Response — 200 OK** — `AreaWithProjects[]` where each entry is `IArea & { projects: IProject[] }`.

---

#### GET /v1/areas/:id

Retrieve a single area.

- **Auth required:** Yes

**Response — 200 OK** — `IArea`

**Errors:** `AREA_NOT_FOUND` (404)

---

#### POST /v1/areas

Create a new area.

- **Auth required:** Yes
- **Plan limit:** Free plan allows a maximum of **3 areas**

**Request body**

```json
{ "name": "Personal", "color": "#3B82F6", "icon": "folder" }
```

| Field   | Type   | Required | Constraints                                   |
| ------- | ------ | -------- | --------------------------------------------- |
| `name`  | string | Yes      | 1–255 characters                              |
| `color` | string | No       | Hex colour e.g. `#3B82F6` — default `#3B82F6` |
| `icon`  | string | No       | Valid `IconId` — default `"folder"`           |

> **Icon set:** areas and projects share one backend-validated allowlist (`project.AllowedIcons`). It is a **superset** of the frontend's curated picker (`src/lib/types/icons.ts`) — every icon the UI can pick is accepted, plus extra options for headroom. An unrecognised icon → `INVALID_INPUT`. The two lists are kept in sync by a regression test (`internal/domain/project/icons_test.go`).

**Response — 201 Created** — `IArea`

**Errors:** `PLAN_LIMIT_EXCEEDED` (403), `INVALID_INPUT` (422), `DUPLICATE_NAME` (409)

---

#### PATCH /v1/areas/:id

Update an area. All fields optional.

- **Auth required:** Yes

**Request body**

```json
{ "name": "Personal Life", "color": "#10B981", "icon": "sprout" }
```

**Response — 200 OK** — Updated `IArea`

**Errors:** `AREA_NOT_FOUND` (404), `INVALID_INPUT` (422), `DUPLICATE_NAME` (409)

---

#### PATCH /v1/areas/reorder

Batch-update `displayOrder` for a set of areas (transactional).

- **Auth required:** Yes

**Request body**

```json
{
  "items": [
    { "id": "01J...", "displayOrder": 0 },
    { "id": "01K...", "displayOrder": 1 }
  ]
}
```

**Response — 200 OK**

```json
{ "updated": 2 }
```

**Errors:** `AREA_NOT_FOUND` (404), `INVALID_INPUT` (422)

---

#### DELETE /v1/areas/:id

Delete an area. Contained projects (and their tasks/subtasks) are deleted with it (`ON DELETE CASCADE`). A project always belongs to an area.

- **Auth required:** Yes

**Response — 204 No Content**

**Errors:** `AREA_NOT_FOUND` (404)

---

### 3.3 Projects

> **`IProject` shape** — all IDs are strings (UUID). A project always belongs to an area, so `areaId` is never null. Fields: `id: string`, `areaId: string`, `name: string`, `status: "active"|"completed"|"archived"`, `folderIcon: string`, `dueDate?: string | null` (RFC 3339), `isFavorite?: boolean`, `description?: string | null`, `displayOrder?: number`, `createdAt: string`, `updatedAt: string`. No embedded `area` object is returned.

#### GET /v1/projects

List all projects for the authenticated user. Cursor-paginated.

- **Auth required:** Yes
- **Query params:** `q` (search), `limit` (1–100, default 50), `cursor`, `areaId`, `status`, `isFavorite`

**Response — 200 OK**

```json
{
  "items": [
    {
      "id": "01J...",
      "areaId": "01K...",
      "name": "Q3 Launch",
      "status": "active",
      "folderIcon": "folder",
      "dueDate": "2026-09-30T00:00:00Z",
      "isFavorite": false,
      "description": null,
      "displayOrder": 0,
      "createdAt": "2026-01-01T00:00:00Z",
      "updatedAt": "2026-01-01T00:00:00Z"
    }
  ],
  "nextCursor": ""
}
```

---

#### GET /v1/areas/:areaId/projects

List projects within a specific area. Same cursor-pagination and query params as `GET /v1/projects`.

- **Auth required:** Yes

**Response — 200 OK** — same paginated envelope as above.

---

#### GET /v1/projects/:id

Retrieve a single project.

- **Auth required:** Yes

**Response — 200 OK** — `IProject`

**Errors:** `PROJECT_NOT_FOUND` (404)

---

#### POST /v1/areas/:areaId/projects

Create a new project inside an area.

- **Auth required:** Yes
- **Plan limit:** Free plan allows a maximum of **5 projects total** (across all areas)

**Request body**

```json
{
  "name": "Q3 Launch",
  "folderIcon": "folder",
  "status": "active",
  "dueDate": "2026-09-30T00:00:00Z",
  "isFavorite": false,
  "description": "Launch plan for Q3."
}
```

| Field         | Type    | Required | Constraints                                                      |
| ------------- | ------- | -------- | ---------------------------------------------------------------- |
| `name`        | string  | Yes      | 1–255 characters                                                 |
| `folderIcon`  | string  | No       | Valid `IconId` — default `"folder"`                              |
| `status`      | string  | No       | `"active"` \| `"completed"` \| `"archived"` — default `"active"` |
| `dueDate`     | string  | No       | RFC 3339 timestamp                                               |
| `isFavorite`  | boolean | No       | Default `false`                                                  |
| `description` | string  | No       | Max 2000 characters                                              |

**Response — 201 Created** — `IProject`

**Errors:** `PLAN_LIMIT_EXCEEDED` (403), `PROJECT_NOT_FOUND` (404 — area not found), `INVALID_INPUT` (422), `DUPLICATE_NAME` (409)

---

#### PATCH /v1/projects/:id

Update a project. All fields optional. Pass `areaId` to move the project to a different area (the target area must exist and belong to you). A project must always belong to an area — omit `areaId` to leave it unchanged; an empty `areaId` is rejected with `INVALID_INPUT`.

- **Auth required:** Yes

**Request body**

```json
{
  "name": "Q3 Launch — Updated",
  "folderIcon": "zap",
  "status": "completed",
  "dueDate": "2026-09-30T00:00:00Z",
  "isFavorite": true,
  "areaId": "01K...",
  "description": "Updated description."
}
```

**Response — 200 OK** — Updated `IProject`

**Errors:** `PROJECT_NOT_FOUND` (404), `AREA_NOT_FOUND` (404 — target `areaId` not found or not yours), `INVALID_INPUT` (422 — includes empty `areaId`), `INVALID_STATUS` (422), `DUPLICATE_NAME` (409)

---

#### PATCH /v1/projects/reorder

Batch-update `displayOrder` for a set of projects (transactional).

- **Auth required:** Yes

**Request body**

```json
{
  "items": [
    { "id": "01J...", "displayOrder": 0 },
    { "id": "01K...", "displayOrder": 1 }
  ]
}
```

**Response — 200 OK**

```json
{ "updated": 2 }
```

**Errors:** `PROJECT_NOT_FOUND` (404), `INVALID_INPUT` (422)

---

#### DELETE /v1/projects/:id

Delete a project and all its tasks.

- **Auth required:** Yes

**Response — 204 No Content**

**Errors:** `PROJECT_NOT_FOUND` (404)

---

### 3.4 Tasks

> **Calm / energy-aware contract.** Tasks carry an **`energy`** dimension (`low|medium|deep`) alongside `priority`, and a single **soft `scheduledFor`** intention (a date you *mean* to do it) — there is no hard deadline on a task. A past `scheduledFor` does **not** go overdue — with **`rollsOver: true`** (the default) it carries forward to today, no guilt.
>
> **⚠️ Status simplified (2026-08-09).** `status` is exactly three values: **`active | done | cancelled`**. A task is always created `active` — there is no separate "unprocessed" status (that's the `bucket`/Inbox capture table, a different entity entirely) and no "someday" parking state. **Scheduled-ness is orthogonal to status, not a status value**: whether a task is scheduled or not is derived client-side from `scheduledFor` being non-null — the frontend's "Scheduled / Unscheduled" filter is computed, never a stored field. Likewise there is no `missed` task status: a recurring occurrence whose window closes unfinished is reaped to `status: "cancelled"` (so it behaves like any other cancelled task in every list/count/search), with the "it lapsed" distinction recorded only in the backend's internal `occurrence_status` — that field never appears in `ITask` and the frontend has no reason to know it exists. A "carried over / overdue" indicator, if the UI wants one, is likewise derived — `active && scheduledFor < today` — never a stored status.

> **`ITask` shape** — all IDs are strings.
> ```ts
> interface ITask {
>   id: string;
>   projectId: string;
>   title: string;
>   notes?: string | null;
>   status: "active" | "done" | "cancelled";
>   recurrenceRuleId: string | null;  // set only on a materialized occurrence
>   occurrenceDate: string | null;    // YYYY-MM-DD
>   priority: "low" | "medium" | "high";          // default "medium"
>   energy: "low" | "medium" | "deep";            // default "medium"
>   rollsOver: boolean;                           // default true
>   scheduledFor?: string | null;                 // SOFT intention — ISO date "YYYY-MM-DD"
>   estimatedMinutes?: number | null;             // 1–1440
>   url?: string | null;
>   displayOrder: number;
>   completedAt?: string | null;                  // set server-side when status→done
>   createdAt: string;                            // RFC3339
>   updatedAt: string;                            // RFC3339
>   totalFocusSeconds: number;                    // SUM of closed focus segments (E-049); always present
>   subtaskCount: number;                         // total subtasks; always present
>   openSubtaskCount: number;                     // subtasks with done=false; always present
> }
> ```
>
> **`totalFocusSeconds`** is derived on read from `focus_sessions` (never cached) and **enriched only on `GET /v1/tasks/:id` (scalar) and `GET /v1/focus` (one batch query)**. On the project task-list it is always `0` — the list never renders it, and a per-row SUM would be pure cost. Zero-default, never null/omitted.
>
> **`subtaskCount` / `openSubtaskCount`** are derived on read from `subtasks` and — unlike `totalFocusSeconds` — are populated on **every** task read, list included. They are correlated scalar subqueries over an index on `subtasks(task_id)`, and the list needs them: the client blocks a complete behind a confirmation whenever `openSubtaskCount > 0`, and a task can be checked off straight from a list row. Zero-default, never null/omitted.
>
> **⚠️ `scheduledFor` is the task's only date.** It is a bare ISO **date string** `YYYY-MM-DD` (a soft, roll-forward intention) — **not** a timestamp and **not** an enum like `today|tomorrow|this_week`. Tasks have **no** hard `dueDate` (that field was removed; a hard deadline lives only on **projects**). The today/tomorrow/thisWeek grouping is *computed* server-side by `GET /v1/time-spread` (§3.7) from `scheduledFor` + `rollsOver`; it is never a stored value. See §3.7 for the bucketing rules.

> **List envelope.** List endpoints (`GET …/tasks`, `GET /focus`) return `{ "items": ITask[], "nextCursor": string }` inside the standard `data` envelope — i.e. `data.items` and `data.nextCursor`, **not** a bare `data: ITask[]`. The frontend `transformResponse` must unwrap to `.data`.

#### GET /v1/projects/:projectId/tasks

List all tasks within a project.

- **Auth required:** Yes

**Query parameters**

| Param       | Type   | Description                                                            |
| ----------- | ------ | --------------------------------------------------------------------- |
| `status`    | string | Filter by `active` \| `done` \| `cancelled`                            |
| `priority`  | string | Filter by `low` \| `medium` \| `high`                                 |
| `energy`    | string | Filter by `low` \| `medium` \| `deep`                                 |
| `search`    | string | Case-insensitive ILIKE over `title` + `notes`                         |
| `sortField` | string | `displayOrder` \| `scheduledFor` \| `priority` \| `title` \| `createdAt` \| `energy` (default `displayOrder`) |
| `sortOrder` | string | `asc` \| `desc` (default `asc`)                                       |
| `cursor`    | string | Opaque base64 page token from a prior `nextCursor`. Absent = first page. |
| `limit`     | int    | 1–100 (default 50)                                                    |

**Response — 200 OK** — `{ "items": ITask[], "nextCursor": string }`

`nextCursor` is `""` when there are no more pages. The cursor is keyset on `(created_at, id) DESC`, independent of `sortField` — display sort is preserved within each page but the cursor position is stable even when `displayOrder` changes (e.g. drag-reorder).

**Errors:** `INVALID_INPUT` / `INVALID_STATUS` / `INVALID_PRIORITY` (422), `PROJECT_NOT_FOUND` (404)

---

#### GET /v1/tasks/:id

Retrieve a single task.

- **Auth required:** Yes

**Response — 200 OK** — `ITask` (with `totalFocusSeconds` enriched — the SUM of the task's closed focus segments, `0` when none)

**Errors:** `TASK_NOT_FOUND` (404 — cross-user access returns 404, no existence leak)

---

#### POST /v1/projects/:projectId/tasks

Create a task inside a project. **Title-only is valid** (quick-add); everything else defaults server-side.

- **Auth required:** Yes
- **Plan limit:** Free plan allows **50 active tasks per project**. Only `active` counts — `done` and `cancelled` are free (a reaped recurring occurrence flips to `cancelled`, so an ignored daily recurrence never silently fills the cap). Exceeding it (or a PATCH that moves a task *into* active over the cap) returns `PLAN_LIMIT_EXCEEDED` (403).

**Request body**

```json
{
  "title": "Write spec",
  "notes": "Write the full API specification",
  "priority": "high",
  "energy": "deep",
  "rollsOver": true,
  "scheduledFor": "2026-05-02",
  "estimatedMinutes": 90,
  "url": "https://notion.so/..."
}
```

| Field              | Type    | Required | Constraints                                                       |
| ------------------ | ------- | -------- | ----------------------------------------------------------------- |
| `title`            | string  | Yes      | 1–255 characters (trimmed)                                        |
| `notes`            | string  | No       | ≤ 2000 characters                                                 |
| `status`           | string  | No       | `active` \| `done` \| `cancelled` — default `active`                  |
| `priority`         | string  | No       | `low` \| `medium` \| `high` — default `medium`                    |
| `energy`           | string  | No       | `low` \| `medium` \| `deep` — default `medium`                    |
| `rollsOver`        | boolean | No       | default `true` (a past `scheduledFor` carries forward)            |
| `scheduledFor`     | string  | No       | **Soft intention** (the task's only date) — ISO date `YYYY-MM-DD`, nullable to clear |
| `estimatedMinutes` | number  | No       | 1–1440                                                            |
| `url`              | string  | No       | ≤ 2048 characters                                                 |

**Response — 201 Created** — `ITask`

**Errors:** `PROJECT_NOT_FOUND` (404), `PLAN_LIMIT_EXCEEDED` (403), `INVALID_INPUT` / `INVALID_STATUS` / `INVALID_PRIORITY` (422)

---

#### PATCH /v1/tasks/:id

Partial update of any mutable field. `status→done` sets `completedAt` server-side; moving away from `done` clears it. A PATCH that moves a task into `active` is subject to the plan limit.

- **Auth required:** Yes

**Request body** (all fields optional; same types/constraints as create, plus `status` and `projectId`)

```json
{
  "title": "Write spec v2",
  "status": "active",
  "energy": "medium",
  "rollsOver": false,
  "scheduledFor": "2026-05-03",
  "projectId": "proj_9f3c2a"
}
```

> `completedAt` and `displayOrder` are **not** client-settable here — `completedAt` is derived from the status transition, and ordering is changed via `PATCH /tasks/:id/reorder`.
>
> `projectId` reassigns the task to a different project — the target must belong to the caller (`PROJECT_NOT_FOUND` on a missing or foreign project, same as create). It is a plain optional string, never nullable: a task's project can never be cleared, only swapped. Any other fields in the same PATCH still apply normally alongside a reassignment. Fires `task.updated` over WS like any other field-level PATCH.

**Response — 200 OK** — Updated `ITask`

**Errors:** `TASK_NOT_FOUND` (404), `PROJECT_NOT_FOUND` (404), `PLAN_LIMIT_EXCEEDED` (403), `INVALID_INPUT` / `INVALID_STATUS` / `INVALID_PRIORITY` (422)

---

#### PATCH /v1/tasks/:id/status

Status-only shorthand (checkbox toggle, cancel). Same `completedAt` side-effects and plan-limit semantics as the full PATCH.

- **Auth required:** Yes

**Request body**

```json
{ "status": "done" }
```

| Field    | Type   | Required | Values                                                       |
| -------- | ------ | -------- | ------------------------------------------------------------ |
| `status` | string | Yes      | `active` \| `done` \| `cancelled`                             |

**Response — 200 OK** — Updated `ITask`

**Errors:** `TASK_NOT_FOUND` (404), `PLAN_LIMIT_EXCEEDED` (403), `INVALID_INPUT` / `INVALID_STATUS` (422)

---

#### PATCH /v1/tasks/:id/schedule

Set (or clear) the **soft** `scheduledFor` intention and the `rollsOver` flag. `scheduledFor` null **or absent** unschedules the task.

- **Auth required:** Yes

**Request body**

```json
{ "scheduledFor": "2026-05-03", "rollsOver": true }
```

| Field          | Type            | Required | Constraints                                          |
| -------------- | --------------- | -------- | ---------------------------------------------------- |
| `scheduledFor` | string \| null  | No       | ISO date `YYYY-MM-DD`; `null`/absent = unschedule    |
| `rollsOver`    | boolean         | No       | Toggles roll-forward                                 |

**Response — 200 OK** — Updated `ITask`

**Errors:** `TASK_NOT_FOUND` (404), `INVALID_DATE` (400 — `scheduledFor` not a valid ISO date)

---

#### PATCH /v1/tasks/:id/reorder

Move a task to a target `displayOrder`; siblings within the project are repacked to a contiguous `0..n-1` sequence (transactional).

- **Auth required:** Yes

**Request body**

```json
{ "displayOrder": 0 }
```

| Field          | Type   | Required | Constraints        |
| -------------- | ------ | -------- | ------------------ |
| `displayOrder` | number | Yes      | ≥ 0                |

**Response — 200 OK** — The moved `ITask`

**Errors:** `TASK_NOT_FOUND` (404), `INVALID_INPUT` (422)

---

#### DELETE /v1/tasks/:id

Hard-delete a task; its subtasks cascade.

- **Auth required:** Yes

**Response — 204 No Content**

**Errors:** `TASK_NOT_FOUND` (404)

---

### 3.5 Subtasks

> **`ISubtask` shape** — `{ id: string, taskId: string, title: string, done: boolean, position: number, createdAt: string, updatedAt: string }`. Ordered by `position` ascending.

#### GET /v1/tasks/:taskId/subtasks

List subtasks for a task.

- **Auth required:** Yes

**Response — 200 OK** — `{ "items": ISubtask[] }`

```json
{
  "items": [
    {
      "id": "sub_01J...",
      "taskId": "01J...",
      "title": "Draft outline",
      "done": false,
      "position": 0,
      "createdAt": "...",
      "updatedAt": "..."
    }
  ]
}
```

---

#### POST /v1/tasks/:taskId/subtasks

Create a subtask.

- **Auth required:** Yes

**Request body**

```json
{ "title": "Draft outline", "position": 0 }
```

**Response — 201 Created** — `ISubtask`

**Errors:** `RESOURCE_NOT_FOUND` (404), `INVALID_INPUT` (422)

---

#### PATCH /v1/tasks/:taskId/subtasks/:id

Update a subtask.

- **Auth required:** Yes

**Request body** (all fields optional)

```json
{ "title": "Draft revised outline", "done": true, "position": 1 }
```

**Response — 200 OK** — Updated `ISubtask`

---

#### DELETE /v1/tasks/:taskId/subtasks/:id

Delete a subtask.

- **Auth required:** Yes

**Response — 204 No Content**

---

### 3.6 Inbox (Bucket)

The inbox is a quick-capture queue. Items have no project association until processed.

#### GET /v1/bucket

List all unprocessed inbox items for the authenticated user.

- **Auth required:** Yes

**Response — 200 OK** — `IBucket[]`

```json
[
  {
    "id": "01J...",
    "userId": "01K...",
    "content": "Buy groceries",
    "processedAt": null,
    "processingResult": null,
    "createdTaskId": null,
    "createdNoteId": null,
    "projectId": null,
    "createdAt": "...",
    "updatedAt": "..."
  }
]
```

---

#### GET /v1/bucket/:id

Retrieve a single inbox item.

- **Auth required:** Yes

**Response — 200 OK** — `IBucket`

---

#### POST /v1/bucket

Capture a new inbox item.

- **Auth required:** Yes

**Request body**

```json
{ "content": "Buy groceries" }
```

| Field     | Type   | Required | Constraints      |
| --------- | ------ | -------- | ---------------- |
| `content` | string | Yes      | 1–500 characters |

**Response — 201 Created** — `IBucket`

---

#### PATCH /v1/bucket/:id

Update an inbox item's content.

- **Auth required:** Yes

**Request body**

```json
{ "content": "Buy groceries and cook dinner" }
```

**Response — 200 OK** — Updated `IBucket`

---

#### POST /v1/bucket/:id/process

Process an inbox item — convert it to a task or note, or trash it.

- **Auth required:** Yes

**Request body**

```json
{
  "processingResult": "task",
  "projectId": "01J...",
  "taskDetails": {
    "title": "Buy groceries",
    "notes": "Weekly shop",
    "priority": "medium",
    "energy": "low",
    "rollsOver": true,
    "scheduledFor": "2026-05-05",
    "estimatedMinutes": 60,
    "url": "https://example.com"
  }
}
```

| Field              | Type   | Required | Values                                    |
| ------------------ | ------ | -------- | ----------------------------------------- |
| `processingResult` | string | Yes      | `"task"` \| `"note"` \| `"trash"`         |
| `projectId`        | string | No       | Required when `processingResult` is `"task"` **or** `"note"` |
| `taskDetails`      | object | No       | Required when `processingResult = "task"` |
| `noteDetails`      | object | No       | Required when `processingResult = "note"` |

Inside `noteDetails`, only `title` is required; `content` is optional and
omitting it means "use the note service default" (the empty doc). `contentText`
is never accepted — the search mirror is always server-derived (§3.17).

**Processing is ordered, NOT transactional** — for the note path exactly as for
the task path. The note is created first (its own transaction, so title
validation and project ownership abort before the bucket row is touched), and
only then is the item marked processed. This guarantees a processed inbox item
never exists without the thing it became: a processed-but-empty item would lose
the user's thought with no trace, whereas a benign orphan note (if the final
mark loses a race) is visible and user-fixable.

On success the response's **`createdNoteId`** carries the new note's id, the
mirror of `createdTaskId`. ⚠️ This is what lets a client link back to what the
thought became — a frontend that ignores it leaves the "view what this became"
affordance silently dead for notes.

Inside `taskDetails`, only `title` is required. Every other field is optional and
omitting it means "use the task service default" — the same defaults `POST
/v1/projects/:projectId/tasks` applies. `status` is not accepted here (the task
service owns it), and `scheduledTime` is not offered by the process flow. Values
are validated by the task service, so a malformed `scheduledFor` fails with
`INVALID_DATE` (400) **before** the inbox item is marked processed.

**Response — 200 OK** — Updated `IBucket` (with `processedAt` and `processingResult` populated)

**Errors:** `RESOURCE_NOT_FOUND` (404), `CONFLICT` (409 — already processed), `INVALID_INPUT` (422)

---

#### DELETE /v1/bucket/:id

Delete an inbox item.

- **Auth required:** Yes

**Response — 204 No Content**

---

### 3.7 Focus & Time Spread View

These two read-only endpoints derive their lists from the user's `active` tasks **across all projects**; `done`/`cancelled` are excluded at the source. Both read the clock once, server-side (the result is deterministic for a given "now"), so no client date is sent.

#### GET /v1/focus

"What can I do right now?" — a deterministically-ranked short list that fits the given time/energy. Candidate set spans all projects.

- **Auth required:** Yes

**Query parameters**

| Param       | Type   | Required | Description                                              |
| ----------- | ------ | -------- | -------------------------------------------------------- |
| `available` | number | No       | Minutes available; tasks whose `estimatedMinutes` exceed it are excluded. `0`/absent = no time filter. |
| `energy`    | string | No       | Current energy `low` \| `medium` \| `deep` (match boosts score). Absent = no energy preference. |
| `limit`     | number | No       | Max results — default `5`, clamped to max `20`.          |

Ranking (deterministic, Free baseline) blends: energy match, time-budget fit (over-budget excluded), `scheduledFor` proximity + escalation (a past-and-rolling-over schedule is the loudest signal, then today, then soon), and a small priority tiebreak. Ties break by `id`.

**Response — 200 OK** — `{ "items": ITask[] }` (each item's `totalFocusSeconds` is enriched via one batch SUM over closed focus segments; `0` when a task has none)

**Errors:** `INVALID_INPUT` (400 — non-integer `available`/`limit`, or bad `energy`)

> Phase 4 (Pro): a future `?explain=true` will let a Pro user get an AI re-rank with reasons. The deterministic engine here stays the Free baseline and the fallback.

---

#### GET /v1/time-spread

Tasks bucketed into today / tomorrow / this week, with the **no-guilt roll-forward**.

- **Auth required:** Yes

**Response — 200 OK**

```json
{
  "today":    [ /* ITask[] */ ],
  "tomorrow": [ /* ITask[] */ ],
  "thisWeek": [ /* ITask[] */ ]
}
```

**Bucketing rules** (per task, first match wins, evaluated in the server's timezone):

Bucketing keys off the task's soft `scheduledFor` (its only date):
   - in the past **and `rollsOver: true`** → **today** (carried over, no guilt);
   - in the past **and `rollsOver: false`** → **dropped** (no bucket);
   - today → **today**; tomorrow → **tomorrow**; within the next 6 days → **thisWeek**; further out → no bucket;
   - no `scheduledFor` → not in any bucket.

> A past `scheduledFor` never surfaces as "overdue" here — the calm tone (a neutral "carried over" chip, never red) is the frontend's job (E-014, NIC-1384).

---

#### Focus Timer — sessions (E-049)

Measures real time-on-task. One row per contiguous active run (a **segment**); a task's total is derived as the SUM over its closed segments — there is no cached total. **Server-authoritative:** every timestamp is stamped by the server, and a client-supplied duration is never accepted. **FREE on every plan.**

**One open segment per user.** Opening a segment closes any other the user has open, in one transaction. That is why close/heartbeat address `current` rather than an id.

**`SessionView`** — the response body and the WS payload for both events:

```json
{
  "id": "sess_123",
  "taskId": "task_456",
  "startedAt": "2026-07-29T10:00:00Z",
  "endedAt": null,
  "lastSeen": "2026-07-29T10:00:30Z",
  "durationSeconds": 0
}
```

`endedAt` is `null` while the segment is open; `durationSeconds` is `0` until it closes (the client renders the live tick from `startedAt`).

##### POST /v1/focus/sessions

Opens a segment, auto-closing any other open one for the user.

- **Auth required:** Yes
- **Body:** `{ "taskId": "task_456" }`

**Response — 201 Created** — `SessionView` (the newly-opened segment)

**Errors:** `INVALID_INPUT` (400 — missing/blank `taskId`, malformed body, unknown field) · `TASK_NOT_FOUND` (404 — not owned, missing, or terminal `done`/`cancelled`) · `UNAUTHORIZED` (401)

##### POST /v1/focus/sessions/current/close

Closes the user's open segment.

- **Auth required:** Yes

**Response — 200 OK** — `SessionView` with `endedAt` set

**Errors:** `RESOURCE_NOT_FOUND` (404 — no open segment) · `UNAUTHORIZED` (401)

##### POST /v1/focus/sessions/current/heartbeat

Bumps `lastSeen` on the open segment (~30s client cadence). **Silent — never broadcasts.**

- **Auth required:** Yes

**Response — 204 No Content** (empty body)

**Errors:** `RESOURCE_NOT_FOUND` (404 — no open segment) · `UNAUTHORIZED` (401)

> **`endedAt = lastSeen`, never `now()`.** Every close stamps the last proven heartbeat, so a browser that dies mid-run contributes the time it actually proved rather than the time until a sweep noticed. A stale sweep closes abandoned segments on the same rule.

##### POST /internal/jobs/focus-stale

Crash recovery: closes open segments whose client stopped heartbeating and never sent a close (tab crash, quit-and-left). **Not part of the public `/v1` contract** — `InternalToken`-guarded (`X-Internal-Token`, the shared `CRON_SECRET`), like every other internal job, and folded into `run-all` so the single Render cron already reaches it.

Each segment is closed at **its own `lastSeen`** — never the sweep's wall clock and never a fixed cap — so a stranded session contributes neither phantom time nor a truncated total, whether it was abandoned after two minutes or genuinely ran for three hours. `?dryRun=true` reports what would close without touching a row. Idempotent and per-item resilient: one row that fails to close does not strand the rest, and the next run retries it.

**Response — 200 OK** — `{ "considered": 3, "closed": 2, "dryRun": false }`

**Errors:** `401 UNAUTHORIZED` (missing/wrong token) · `503 SERVICE_UNAVAILABLE` (`CRON_SECRET` unset — the endpoint is disabled, never open) · `500` on a listing failure

Staleness threshold is **90s** (3× the ~30s client heartbeat, so one dropped beat never costs a live user their timer). A stranded session is therefore closed within roughly one sweep interval.

> ⚠️ **Cron cadence is not yet decided** (NIC-1711 open question). The endpoint is wired into `run-all`, which today runs hourly — so a stranded segment is currently reclaimed within the hour, not within 90s. Whether to shorten `run-all` or give focus-stale its own more frequent cron is an ops call; the sweep itself is correct either way, since it always closes at `lastSeen`.

---

### 3.8 NLP Smart Scheduling (Pro only)

#### POST /v1/nlp/parse

Parse a natural-language string and extract scheduling intent.

- **Auth required:** Yes
- **Plan required:** Pro

**Request body**

```json
{ "text": "remind me to review the spec next Monday afternoon" }
```

**Response — 200 OK**

```json
{
  "scheduledFor": "2026-05-04",
  "confidence": 0.92
}
```

**Errors:** `PLAN_LIMIT_EXCEEDED` (403), `RATE_LIMITED` (429)

---

### 3.9 AI Assistant

#### POST /v1/ai/sessions

Start a new AI assistant session.

- **Auth required:** Yes
- **Plan limit:** creating a session is free; the quota is metered per message
  (Free = 5 lifetime, Pro = 500/month — see `POST …/messages` and `GET /v1/ai/usage`).

**Request body**

```json
{ "title": "Sprint planning help" }
```

**Response — 201 Created**

```json
{
  "id": "sess_abc123",
  "title": "Sprint planning help",
  "createdAt": "...",
  "updatedAt": "..."
}
```

---

#### GET /v1/ai/sessions

List all AI sessions for the user.

- **Auth required:** Yes

**Response — 200 OK** — `IAISession[]`

---

#### GET /v1/ai/sessions/:id

Retrieve a session with its most recent 50 messages (oldest-first). Older history is not included here — fetch it via `GET /v1/ai/sessions/:id/messages`.

- **Auth required:** Yes

**Response — 200 OK** — `IAISession` with `messages: IAIMessage[]` and `messagesCursor: string`.

`messagesCursor` seeds "load older history": pass it as `?cursor=` to `GET /v1/ai/sessions/:id/messages` to fetch the page immediately before `messages`. Empty string when `messages` already covers the whole session (≤ 50 total).

---

#### GET /v1/ai/sessions/:id/messages

Paginated message history for a session — **"load older" semantics**. Returns messages oldest-first (ASC). The `nextCursor` — when non-empty — points to the next (older) page; pass it as `?cursor=` to go further back.

- **Auth required:** Yes
- A session the caller does not own → `404 RESOURCE_NOT_FOUND`.

**Query parameters**

| Param    | Type   | Description                                                                     |
| -------- | ------ | ------------------------------------------------------------------------------- |
| `cursor` | string | Opaque base64 page token from a prior `nextCursor`. Absent = most-recent page. |
| `limit`  | int    | 1–100 (default 50)                                                              |

**Response — 200 OK**

```json
{
  "items": [
    { "id": "msg_1", "role": "user", "content": "Hi", "createdAt": "…" },
    { "id": "msg_2", "role": "assistant", "content": "Hello!", "createdAt": "…" }
  ],
  "nextCursor": "eyJ…"
}
```

`nextCursor` is `""` when no older messages exist. Keyset on `(created_at DESC, id DESC)` internally, reversed to ASC before return.

**Errors:** `INVALID_INPUT` (422 — bad cursor or limit out of range), `RESOURCE_NOT_FOUND` (404)

---

#### POST /v1/ai/sessions/:id/messages

Send a message to the AI assistant and stream Claude's reply back as **SSE over
the POST response body** (not `EventSource` — that is GET-only and cannot carry a
Bearer token). Rate-limited `RateLimitUser(10, 10)`.

- **Auth required:** Yes
- **Plan limit:** reserves one AI request atomically before any provider call
  (Free = 5 lifetime, Pro = 500/month). Over quota → `429 AI_LIMIT_REACHED`, no
  Anthropic call.

**Request body** — `content`, 1..2000 chars after trim, else `422 INVALID_INPUT`.

```json
{ "content": "Help me break down the Q3 launch project into tasks" }
```

**Response — 200 OK, `Content-Type: text/event-stream`** (also
`Cache-Control: no-store`, `X-Accel-Buffering: no`). The status commits on the
first token. Each frame is a `data:` line with a type-discriminated JSON payload:

```
data: {"type":"delta","text":"Sure, "}
data: {"type":"delta","text":"here are..."}
data: {"type":"done","messageId":"msg_xyz","usage":{"used":3,"limit":500,"scope":"month","month":"2026-07"}}
```

Exactly one terminal event ends every stream: `done` (success) **or**
`{"type":"error","code":"AI_PROVIDER_ERROR"}` (mid-stream failure — the HTTP
status is already committed, so the error rides the stream).

**Pipeline (order is load-bearing):** feature check (503 `AI_UNAVAILABLE` if the
key is unset) → session ownership (`404`) → single-stream guard (`409
AI_STREAM_ACTIVE`) → quota reserve (`429`) → persist user message (+ first-turn
title, first 50 chars word-boundary, same tx) → stream Claude → persist assistant
message + bump `updated_at` + emit WS `ai.session.updated`.

**Prompt cache:** static system prompt (persona + scope guard) with a volatile
tail (today's date, user language, open-task count), marked ephemeral; history is
budgeted newest-first (≤20 msgs, ~20 000 chars) with a cache breakpoint on the
newest block.

**Cancellation:** client abort cancels the Anthropic stream; the partial
assistant text is persisted as a normal message and the quota charge is **kept**
(a zero-token failure is the only case that refunds).

**Errors:** `INVALID_INPUT` (422), `RESOURCE_NOT_FOUND` (404), `AI_STREAM_ACTIVE`
(409), `AI_LIMIT_REACHED` (429 — quota), `AI_UNAVAILABLE` (503 — feature off,
provider 429/529, or first-token timeout), `AI_PROVIDER_ERROR` (502 — provider
400/401), `RATE_LIMITED` (429).

---

#### DELETE /v1/ai/sessions/:id

Delete an AI session and all its messages.

- **Auth required:** Yes

**Response — 204 No Content**

---

#### GET /v1/ai/usage

Read the caller's current AI quota state — powers the usage meter and the
upgrade prompt.

- **Auth required:** Yes

**Response — 200 OK**

```json
{ "used": 3, "limit": 500, "scope": "month", "month": "2026-07" }
```

`scope` is `"lifetime"` for Free (`limit` = 5, `month` = `null`) and `"month"`
for Pro (`limit` = 500, `month` = the current `YYYY-MM`).

---

### 3.10 Search

#### GET /v1/search

Full-text search across tasks, projects, areas, and **notes**.

- **Auth required:** Yes
- Ranked by `ts_rank` over the STORED `search_vector` GIN columns, using the
  `'simple'` config (no stemming, so prefix/type-ahead matching works and every
  language is treated alike).

**Query parameters**

| Param   | Type   | Required | Description                                                       |
| ------- | ------ | -------- | ----------------------------------------------------------------- |
| `q`     | string | Yes      | Search query (2–100 characters)                                   |
| `types` | string | No       | Comma-separated subset of `task,project,area,note` — omit for all |
| `limit` | number | No       | Max results **per group**, 1–50 — default 10                      |

**Response — 200 OK**

```json
{
  "tasks":    [ { "id", "title", "excerpt", "projectId", "projectName" } ],
  "projects": [ { "id", "name", "areaName" } ],
  "areas":    [ { "id", "name" } ],
  "notes":    [ { "id", "title", "excerpt", "projectId", "projectName" } ]
}
```

Every group is always present (empty array when unrequested or unmatched), so a
client never needs a nil check.

⚠️ **`notes` is a response-shape change.** It is additive, but a frontend that
does not handle the new group silently drops results.

**Notes in search** match on **title _and_ body text**, because the note search
vector is generated from `title || content_text`. An **orphaned** note
(`project_id` NULL after its project was deleted) is still returned, with empty
`projectId`/`projectName` — search is user-scoped, not project-scoped, which is
precisely what keeps orphans reachable.

> **Drift note:** this section previously documented `type` (singular),
> `offset`, and a `_highlight` field. None of those exist in the shipped API —
> the parameter is `types`, there is no offset, and hits carry a plain
> `excerpt`. Corrected under NIC-1909.

---

### 3.11 Notifications

In-app notifications are created through a single idempotent funnel (dedupe by
`dedupeKey`) and delivered three ways: **in-app** (this list + the unread badge),
**WebSocket** (`notification.created`, §3.14 — FREE, instant), and — for Pro —
**email** (the due-digest) and **Web Push** (browser OS notification when the tab
is closed). WS delivery is free for every plan; the email + Web Push channels are
Pro-only.

#### Notification types

Every notification carries a `type`. FREE types reach all plans; Pro types are
only ever produced for Pro users.

| `type`                 | Plan | Producer                    |
| ---------------------- | ---- | --------------------------- |
| `task_due_soon`        | FREE | due-date cron sweep         |
| `task_overdue`         | FREE | overdue cron sweep          |
| `task_scheduled_today` | FREE | start-of-day sweep          |
| `task_completed`       | FREE | real-time (task mutation)   |
| `project_completed`    | FREE | real-time (task mutation)   |
| `system_announcement`  | FREE | (no producer yet)           |
| `day_plan_nudge`       | PRO  | start-of-day sweep          |
| `inbox_unprocessed`    | PRO  | inbox nudge sweep           |
| `inbox_stale`          | PRO  | inbox nudge sweep           |
| `inbox_zero`           | PRO  | real-time (bucket cleared)  |
| `daily_summary`        | PRO  | end-of-day sweep            |
| `streak_milestone`     | PRO  | end-of-day sweep            |

The client must render an unknown `type` gracefully (icon/label fallback) — the
set grows over time.

#### GET /v1/notifications

List the authenticated user's notifications, newest first. Cursor-paginated.

- **Auth required:** Yes
- **Query:** `isRead` (bool, optional filter) · `limit` (default/cap per server) · `cursor` (opaque, from a prior `nextCursor`)

**Response — 200 OK**

```json
{
  "items": [
    {
      "id": "01J...",
      "type": "task_due_soon",
      "title": "Task due soon",
      "body": "\"Ship the release\" is due in 24h.",
      "metadata": { "taskId": "01K...", "count": 3 },
      "isRead": false,
      "readAt": null,
      "createdAt": "2026-07-16T08:00:00Z"
    }
  ],
  "nextCursor": ""
}
```

`metadata` is a free-form object whose keys depend on `type` (e.g. `count` on the
inbox/summary families, `taskId` on task-scoped types). `nextCursor` is empty when
there are no more pages.

---

#### GET /v1/notifications/unread-count

Unread-notification count for the badge.

- **Auth required:** Yes

**Response — 200 OK** — `{ "count": 3 }`

---

#### PATCH /v1/notifications/:id

Mark a single notification read (idempotent). Row-scoped to the caller.

- **Auth required:** Yes
- **Request body:** `{ "isRead": true }`

**Response — 200 OK** — the updated notification view (same shape as a list item)

---

#### PATCH /v1/notifications/read-all

Mark all of the user's notifications read.

- **Auth required:** Yes

**Response — 200 OK** — `{ "count": <marked> }`

---

#### DELETE /v1/notifications/:id

Delete a notification. Row-scoped to the caller.

- **Auth required:** Yes

**Response — 204 No Content**

---

#### GET /v1/notifications/preferences

Get the user's notification preferences (defaults when no row exists).

- **Auth required:** Yes

**Response — 200 OK** — `INotificationPreferences`

```json
{
  "emailDigest": true,
  "pushEnabled": false,
  "smsEnabled": false,
  "beforeDueMinutes": 1440,
  "afterDueMinutes": 0,
  "overdueEnabled": true,
  "dailySummaryEnabled": true,
  "inboxNudgesEnabled": true,
  "streaksEnabled": true,
  "morningHour": 8,
  "eveningHour": 20
}
```

- `emailDigest` gates the Pro due-digest email. `pushEnabled` is the browser Web
  Push toggle (Pro-only — see §3.12-push below). `beforeDueMinutes` / `afterDueMinutes`
  are the due-reminder lead times (capped at 10080 = 7 days).
- The four `*Enabled` toggles silence individual proactive families independently:
  `overdueEnabled` → overdue sweep · `dailySummaryEnabled` → end-of-day summary ·
  `inboxNudgesEnabled` → inbox nudges · `streaksEnabled` → streak milestone. All
  default `true`; an absent preferences row means "all on".
- `morningHour` (5–11, default 8) is the local hour the morning sweeps fire at
  (day-start, inbox, overdue, due-notify); `eveningHour` (18–22, default 20) drives
  the end-of-day summary sweep. Both are validated server-side (out of range →
  `INVALID_INPUT`) and backed by a DB CHECK. Each sweep fires within a 3-hour
  catch-up window from its hour (insures against a missed hourly tick — DST, a
  failed cron run, a cold start), clamped so it never wraps past midnight.

---

#### PUT /v1/notifications/preferences

Partial or full update of the user's preferences (lazy upsert). Any omitted field
keeps its stored value; on first write, omitted fields take their default.

- **Auth required:** Yes
- **Request body** — any subset of `INotificationPreferences`

**Response — 200 OK** — the full resulting `INotificationPreferences`

---

#### POST /v1/notifications/push/subscribe

Store a browser Web Push subscription (upsert by endpoint). **Pro-only.**

- **Auth required:** Yes
- **Request body**

```json
{
  "endpoint": "https://fcm.googleapis.com/fcm/send/...",
  "p256dhKey": "<base64url>",
  "authKey": "<base64url>",
  "userAgent": "Mozilla/5.0 ..."
}
```

**Responses**
- **201 Created** — subscription stored (a repeat subscribe on the same endpoint refreshes it, no duplicate row).
- **403 `PLAN_LIMIT_EXCEEDED`** — free plan; nothing stored.
- **422 `INVALID_INPUT`** — missing `endpoint` / `p256dhKey` / `authKey`.

---

#### DELETE /v1/notifications/push/subscribe

Remove the user's subscription for an endpoint. Idempotent; **no plan gate** (a
downgraded user must still be able to unsubscribe).

- **Auth required:** Yes
- **Request body:** `{ "endpoint": "https://..." }`

**Response — 204 No Content**

---

### 3.12 File Attachments (S3) — E-024

Attachments use a **two-step presigned-PUT** pattern: the client uploads bytes **directly to object storage** (never through the API), then confirms. The owner is a **polymorphic-flat `{ownerType, ownerId}`** pair (only `task` today; notes later share the same table) — endpoints live under `/attachments`, not nested under the owner. Confirm **re-reads the object via HeadObject** and never trusts client-claimed size/type — this is the sole enforcement boundary for size and MIME. `s3Key` is a server-internal detail and is **never** returned.

> **Upload leg is PUT, not POST.** The original design used a presigned **POST policy** (which can pin size + type at the store). The NIC-1679 spike found **Cloudflare R2 returns `501 Not Implemented` for POST-policy form uploads**, so uploads use a **presigned PUT** instead: the client PUTs the raw file body to `url` with the `headers` returned (Content-Type). On R2 neither size nor type is enforceable at the upload leg — both are re-validated from the stored object at confirm via HeadObject.

**`AttachmentView`** (the shared response shape):

```json
{ "id": "…", "ownerType": "task", "ownerId": "…", "fileName": "report.pdf", "fileSize": 204800, "mimeType": "application/pdf", "createdAt": "…" }
```

**Gate order (writes):** `plan (Pro) → config (503 if storage unconfigured) → ownership → quota`. Reads and delete are **not** plan-gated (a downgraded user can still fetch and clean up).

**Config gate:** if the storage backend is unconfigured, every `/attachments*` endpoint returns `503 SERVICE_UNAVAILABLE` — never a silent no-op.

**Allowlist:** explicit MIME set (jpeg, png, gif, webp, pdf, plain, csv, zip, doc/docx, xls/xlsx). **No SVG, no globs.** Max **20 MB / file**, **20 files / owner**, **100 MB total / user**.

---

#### POST /v1/attachments/upload-url

Mint a presigned **PUT** URL for a new upload. **Pro-gated.** Ownership-checked. Cheap claimed-size/type pre-check (real enforcement is the HeadObject re-read at confirm — see the PUT note above).

- **Auth required:** Yes · **Plan:** Pro only

**Request body**

```json
{ "ownerType": "task", "ownerId": "01J…", "fileName": "report.pdf", "mimeType": "application/pdf", "fileSize": 204800 }
```

**Response — 200 OK** — **PUT** the raw file bytes to `url` with the returned `headers` (Content-Type), then confirm with `s3Key`.

```json
{ "url": "https://…r2.cloudflarestorage.com/nicoflow-attachments/attachments/…?X-Amz-Signature=…", "headers": { "Content-Type": "application/pdf" }, "s3Key": "attachments/{userId}/task/{ownerId}/{uuid}" }
```

---

#### POST /v1/attachments

Confirm an uploaded object. Body is `{ s3Key, fileName }` — **only** the key is trusted; `HeadObject` re-reads the real size/type. Disallowed type or > 20 MB → object deleted + `422 INVALID_INPUT` (no row). Over quota → object deleted + `403` (see below). **Pro-gated.** Broadcasts `attachment.created`.

- **Auth required:** Yes · **Plan:** Pro only

**Request body**

```json
{ "s3Key": "attachments/{userId}/task/{ownerId}/{uuid}", "fileName": "report.pdf" }
```

**Response — 201 Created** — an `AttachmentView`.

---

#### GET /v1/attachments?ownerType=&ownerId=

List a single owner's attachments. **Not** plan-gated. Ownership-checked (foreign owner → `404`, no existence leak).

- **Auth required:** Yes

**Response — 200 OK** — `[AttachmentView]`.

---

#### GET /v1/attachments/:id/download-url

Presigned S3 **GET** URL with forced-download disposition (`Content-Disposition: attachment`, never inline). **Not** plan-gated; ownership by `user_id`.

- **Auth required:** Yes

**Response — 200 OK**

```json
{ "url": "https://…s3…/…?X-Amz-Signature=…" }
```

---

#### DELETE /v1/attachments/:id

Delete an attachment: DB row first, then the S3 object (idempotent). **Not** plan-gated. Broadcasts `attachment.deleted` (`{ id, ownerType, ownerId }`).

- **Auth required:** Yes

**Response — 204 No Content**

---

### 3.13 Recurrence Rules — E-050

A recurrence rule is a **task template plus a schedule and a cursor**. The tasks it
produces are ordinary task rows carrying `recurrenceRuleId` + `occurrenceDate`.
Reads are open on every plan; **Free is capped at 3 rules** (`PLAN_LIMIT_EXCEEDED`).

The schedule is a deliberate subset of RFC 5545 stored as **columns, not an RRULE
string**, so the client can render a human summary without shipping a parser.
There is **no time-of-day** — `scheduledFor` is a `YYYY-MM-DD` date and `due_date`
was dropped in migration 026.

**`RecurrenceRuleView`** — all IDs are strings; all dates are `YYYY-MM-DD`.

```jsonc
{
  "id": "uuid", "projectId": "uuid",
  "title": "Water the plants", "notes": null,
  "priority": "medium", "energy": "medium", "estimatedMinutes": null,
  "scheduledTime": "09:00",    // HH:MM stamped onto every occurrence; null = all-day. Pro-only to SET
  "freq": "weekly",            // daily | weekly | monthly | yearly
  "interval": 1,               // 1..366
  "byWeekday": [1, 4],         // weekly only; 0=Sun..6=Sat; always an array, never null
  "byMonthday": null,          // monthly only; 1..31, or -1 = last day of month
  "startDate": "2026-03-02",
  "endDate": null,             // null = runs forever
  "nextOccurrence": "2026-03-09", // null = series exhausted
  "paused": false,
  "createdAt": "...", "updatedAt": "..."
}
```

**Monthly overflow clamps.** "The 31st" yields the 30th in a 30-day month and the
28th (29th in a leap year) in February — a monthly obligation never vanishes
because the month is short.

**`scheduledTime` lives on the rule, not the occurrence.** "Every weekday at 09:00"
is a property of the habit, so every materialized task inherits it and a rule edit
re-stamps the live instance. Same contract as `tasks.scheduledTime`: `HH:MM`, snapped
to 15 minutes, Pro-only to **set** (`PLAN_LIMIT_EXCEEDED` on Free; clearing to `null`
is open on every plan so a downgraded user is never trapped). `estimatedMinutes` is
clamped so `scheduledTime + estimate` cannot cross midnight.

#### POST /v1/projects/:projectId/recurrence-rules

Create a rule and **materialize instance #1 in the same transaction** — a rule can
never exist without its first task. Broadcasts `recurrence.created`.

- **Auth required:** Yes

**Request** — `title`, `freq`, and `startDate` are required; `interval` defaults to `1`.

**Response — 201 Created:** `RecurrenceRuleView`

**Errors:** `PROJECT_NOT_FOUND` (404 — checked *before* the plan count, so a foreign
project can't be used to probe the caller's rule count), `PLAN_LIMIT_EXCEEDED` (403 —
4th rule on Free, or a `scheduledTime` on Free), `INVALID_RECURRENCE` (422),
`INVALID_INPUT` (422), `INVALID_DATE` (422)

#### GET /v1/recurrence-rules

List the caller's rules, newest first. Optional `?projectId=` filter.

- **Auth required:** Yes

**Response — 200 OK:** `{ "items": RecurrenceRuleView[] }`

#### GET /v1/recurrence-rules/:id

**Response — 200 OK:** `RecurrenceRuleView` · **Errors:** `RECURRENCE_RULE_NOT_FOUND` (404)

#### PATCH /v1/recurrence-rules/:id

Edit the series. **Strict split, no third mode:** the edit changes the template for
future instances; already-materialized rows are untouched **except the single live
instance**, which is re-stamped (title/notes/priority/energy) and re-dated if the
schedule moved. Re-stamping is unconditional — no per-field dirty tracking — so a
manual rename of that instance can be overwritten. Editing an *instance*
(`PATCH /tasks/:id`) never propagates back to the rule.

A schedule change recomputes `nextOccurrence`. `endDate` accepts an explicit `null`
to clear it, which revives an exhausted series. Broadcasts `recurrence.updated`.

- **Auth required:** Yes

**Response — 200 OK:** `RecurrenceRuleView`

**Errors:** `RECURRENCE_RULE_NOT_FOUND` (404), `PLAN_LIMIT_EXCEEDED` (403 — setting a
`scheduledTime` on Free), `INVALID_RECURRENCE` (422), `INVALID_INPUT` (422), `INVALID_DATE` (422)

#### PATCH /v1/recurrence-rules/:id/pause

Body `{ "paused": true }`. A paused rule is excluded from the due scan. Broadcasts
`recurrence.updated`.

- **Auth required:** Yes

**Response — 200 OK:** `RecurrenceRuleView` · **Errors:** `RECURRENCE_RULE_NOT_FOUND` (404)

#### GET /v1/recurrence-rules/:id/stats

Derived history for one rule. **Never stored** — a counter column would count
materializations, not completions, and drift the moment either trigger retries.

- **Auth required:** Yes

**Response — 200 OK**

```jsonc
{ "done": 12, "missed": 3, "cancelled": 1, "streak": 4 }
```

`streak` walks occurrences newest-first and counts consecutive `done`. The
still-open instance (`active`) is skipped rather than breaking it — today being
unfinished is not a failure yet — and a user-cancelled occurrence is skipped too,
since opting out deliberately is not the same as letting the window lapse.
**⚠️ Implementation note (2026-08-09):** `missed` is no longer a `tasks.status`
value — `tasks.status` is exactly `active | done | cancelled`. A reaped
occurrence's `status` is `cancelled`; the "it lapsed rather than was cancelled"
distinction this endpoint needs lives in a backend-internal `occurrence_status`
column (`recurrence` domain only, never exposed on `ITask`). This endpoint's
JSON response is unaffected — `missed` still appears in the stats payload as a
derived label.

**Errors:** `RECURRENCE_RULE_NOT_FOUND` (404)

#### DELETE /v1/recurrence-rules/:id

End the series. The **pending** (un-done) occurrence is deleted; **past occurrences
are orphaned, not destroyed** (`ON DELETE SET NULL` on `tasks.recurrence_rule_id`) —
they are the user's record of what they did. Broadcasts `recurrence.deleted`.

- **Auth required:** Yes

**Response — 204 No Content** · **Errors:** `RECURRENCE_RULE_NOT_FOUND` (404)

**No skip endpoint.** Ignoring an occurrence yields the reap (see Materialization
below) — `status` becomes `cancelled` same as an explicit "no" via
`PATCH /tasks/:id/status`, but the reap additionally stamps `occurrence_status =
'missed'`. The two are deliberately distinguishable at the storage layer — the
streak calculation tells them apart — even though both read as `cancelled` on
the wire.

#### Materialization (E-050 / NIC-1773)

**Horizon: exactly one live instance per rule.** This is what bounds row growth and
stops a daily rule from eating a project's 50-task limit. The engine never
materializes ahead.

One routine, two triggers:

1. **Cron sweep** — `POST /internal/jobs/recurrence` (and folded into
   `run-all`, so the single hourly Render cron already reaches it; do **not**
   provision a second cron). `InternalToken`-guarded, `?dryRun=true` supported.
2. **Synchronous on completion** — when a task carrying a `recurrenceRuleId`
   transitions to `done`, its successor is created in the same request, so an
   active user sees the habit continue even if the cron is broken. The trigger
   hangs off the transition itself, not one route, so **both** `PATCH /tasks/:id`
   (the edit dialog) and `PATCH /tasks/:id/status` (the list checkbox) fire it —
   exactly once each.

Both are safe to race: the partial unique index on
`(recurrence_rule_id, occurrence_date)` means the loser inserts nothing.

Per due rule the materializer: skips if an un-done instance already exists →
**reaps** the lapsed instance (`status: active → cancelled`,
`occurrence_status → 'missed'`) → inserts the new task from the template
(`status='active'`, `scheduled_for = occurrence_date`) → advances
`next_occurrence`, nulling it when `end_date` is spent.

**"Due" is decided in the owner's timezone**, not UTC's — a rule fires on the
user's Monday.

**Plan-limit stall.** If the project is at its active-task limit the insert is
guarded away and `next_occurrence` is **deliberately not advanced**: silently
stepping past an occurrence the user never saw is data loss. It is counted as
`skippedPlanLimit` and retried on the next tick.

**Sweep response** (`considered`, `materialized`, `reaped`, `skippedPlanLimit`,
`skippedExisting`, `skippedNotDue`, `skippedBadTimezone`). A run that considered
due rules but materialized nothing logs at **warn** — a silent `generated:0` is
exactly what hid the earlier staging cron failure. Paused and exhausted rules
never enter the due scan, so the alarm cannot cry wolf.

#### Time Spread & list placement of occurrences

A recurring occurrence is **an appointment with a window, not a debt that follows
you**:

- on its `occurrenceDate` → **Today**, as normal;
- past its date and still `active` → **no bucket**. It stays completable from the
  project view and search, but stops occupying Today. `rollsOver` is not read on
  this path — the reap (see above) supersedes it once the window actually closes.

Non-recurring tasks keep their existing roll-forward behaviour, unchanged.

Completed (`status='done'`) and reaped (`occurrence_status='missed'`)
**occurrences** are excluded from the default project task list and from
search, so years of history never clutter the working views (a one-off `done`
task, or a user-cancelled occurrence with no `occurrence_status`, is
unaffected). History stays reachable from the rule detail view; occurrence
rows are kept forever — there is no purge job.

---

### 3.14 Billing & Subscriptions

#### GET /v1/billing/plan

Retrieve the user's current plan and usage.

- **Auth required:** Yes

**Response — 200 OK**

```json
{
  "plan": "free",
  "status": "active",
  "usage": {
    "areas": 2,
    "areasLimit": 3,
    "projects": 4,
    "projectsLimit": 5,
    "aiRequests": 7,
    "aiRequestsLimit": 10
  }
}
```

---

#### GET /v1/billing/checkout-url

Return the static Lemon Squeezy checkout URL for upgrading to Pro (with `checkout[custom][user_id]` appended).

- **Auth required:** Yes

**Response — 200 OK**

```json
{ "url": "https://nicoflow.lemonsqueezy.com/buy/<variant-id>?checkout[custom][user_id]=<uid>" }
```

---

#### GET /v1/billing/portal-url

Return the Lemon Squeezy customer portal URL for managing billing.

- **Auth required:** Yes

**Response — 200 OK**

```json
{ "url": "https://app.lemonsqueezy.com/billing/..." }
```

---

#### POST /v1/billing/webhook

Lemon Squeezy webhook receiver. Not called by the client — called by Lemon Squeezy servers.

- **Auth required:** No (HMAC-SHA256 signature in `X-Signature` header — invalid signature → 401)
- **Idempotent:** Yes — duplicate events are silently ignored via `webhook_events` table

**Response — 200 OK**

---

### 3.15 Real-Time Sync (WebSocket)

#### GET /v1/ws

Upgrade to a WebSocket connection for real-time push events. **FREE on every plan**
— the connection is not Pro-gated; the JWT identifies the user, it does not gate.

- **Auth required:** Yes — JWT is passed as the `?token=` query param (browsers
  can't set `Authorization` on the WS handshake). A missing/invalid/expired token
  completes the upgrade then closes with **`1008` Policy Violation**.

**Connection URL**

```
wss://api.nicoflow.app/v1/ws?token=<jwt>
```

**Heartbeat & limits** — the server pings every **30s**; a missed pong within the
**60s** read deadline closes the connection; per-write deadline is **10s**;
inbound frames are capped at **512 bytes** (clients are receive-only — inbound
frames are discarded). A user may hold multiple connections (tabs/devices); every
event fans out to all of them.

**Server-pushed event shape** (envelope for every message)

```json
{
  "event": "task.updated",
  "payload": { "...": "full resource — no diffs" },
  "timestamp": "2026-07-16T12:00:00Z"
}
```

**Event types**

| Event                  | Payload           |
| ---------------------- | ----------------- |
| `task.created`         | `ITask`           |
| `task.updated`         | `ITask`           |
| `task.deleted`         | `{ id }`          |
| `task.status_changed`  | `ITask`           |
| `project.created`      | `IProject`        |
| `project.updated`      | `IProject`        |
| `project.deleted`      | `{ id }`          |
| `area.created`         | `IArea`           |
| `area.updated`         | `IArea`           |
| `area.deleted`         | `{ id }`          |
| `bucket.processed`     | `{ id }`          |
| `recurrence.created`   | `RecurrenceRuleView` (§3.13) |
| `recurrence.updated`   | `RecurrenceRuleView` (§3.13) |
| `recurrence.deleted`   | `{ id }`          |
| `notification.created` | `INotification` (full `NotificationView`, §3.11) |
| `focus.session_started` | `SessionView` (§3.7) |
| `focus.session_ended`   | `SessionView` (§3.7, `endedAt` set) |

Focus events are **transition-only** — heartbeats never broadcast. When opening a
segment closes a prior one, `focus.session_ended` is emitted **before**
`focus.session_started`, so another tab stops the old timer before starting the
new one. Both fire only after the transaction commits.

The frontend maps `notification.created` to a tag-invalidation of the
notification list + unread count (prefer invalidation over cache-patching); the
badge rise then drives the bell animation and (Pro) the browser notification.

---

### 3.16 Google Calendar Overlay — E-052

A **read-only** overlay of the user's Google events on the Nicoflow calendar.
Nicoflow never writes to Google: the only scope requested is
`calendar.readonly`. Google is the source of truth and is called **live** — there
is no event table, no `syncToken` and no sync engine, because those reintroduce
staleness exactly when a meeting is rescheduled minutes before it starts.

The refresh token is encrypted at rest (AES-256-GCM) and **never appears in any
response**, encrypted or otherwise. The access token is not stored at all — it is
short-lived and re-derived from the refresh token on demand.

**Configuration:** `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`,
`GOOGLE_REDIRECT_URL` and `GOOGLE_TOKEN_ENC_KEY` are required **together**. Any
unset ⇒ the integration is off: connection endpoints return a typed `503` and the
events endpoint returns an empty overlay with `googleStatus: "disconnected"`.

#### The `googleStatus` contract

Every events response carries a status, because an empty list is ambiguous — a
user with no meetings and a user whose token just died look identical, and the
second must never be shown a clean calendar.

| Status | Meaning | Client action |
| ------ | ------- | ------------- |
| `ok` | Fetch succeeded; the list is complete | Render the overlay |
| `disconnected` | No usable connection — never connected, or the grant is dead and the token has been cleared | Show the reconnect prompt |
| `error` | Connection is fine; Google could not be reached or refused the request | Show the dismissible unavailability strip |

#### GET /v1/calendar/google/connect

Returns the Google consent URL. Returns JSON rather than a 302 because the caller
is an authenticated XHR — a redirect would be followed by `fetch` without the
browser ever navigating.

- **Auth required:** Yes
- **Query:** `next` (optional) — an in-app absolute path to return to. Anything
  with a scheme or authority is discarded (open-redirect defence).

```json
{ "data": { "authUrl": "https://accounts.google.com/o/oauth2/v2/auth?..." }, "error": null }
```

#### GET /v1/calendar/google/callback

**Unauthenticated** — the browser arrives from `accounts.google.com` with no
`Authorization` header, so the single-use `state` is what identifies the user and
proves they started the flow. Always **redirects** back into the SPA with a
`?google=connected|denied|failed` status; never returns JSON.

Rate-limited per IP (20/min) because it is public and makes a network call.

#### GET /v1/calendar/google/connection

- **Auth required:** Yes
- **Errors:** `409 GOOGLE_NOT_CONNECTED`

**`ConnectionView`** — no token field in any form.

```jsonc
{
  "googleAccountEmail": "user@example.com",
  "selectedCalendarIds": ["primary"],
  "scopes": ["https://www.googleapis.com/auth/calendar.readonly"],
  "connectedAt": "2026-08-03T09:00:00Z",
  "lastSyncAt": null,
  "lastError": null            // human-readable; never carries token material
}
```

#### DELETE /v1/calendar/google/connection

Revokes the grant **with Google first**, then deletes the local row — deleting
without revoking would leave a live grant the user believes they removed. A
revoke failure is logged but does **not** abort the delete, since refusing to
disconnect because Google is unreachable would trap the user in a connection they
have explicitly rejected.

- **Auth required:** Yes
- **Response:** `204 No Content` — idempotent

#### GET /v1/calendar/google/calendars

Lists the calendars the user can read, each flagged with whether it currently
overlays the Nicoflow calendar. A Google account is not one calendar — importing
everything would tint every day and invert the signal from "you are booked" into
"ignore this colour".

Selection state is merged server-side rather than left to the client, which would
otherwise have to intersect two lists and get the stale case wrong.

- **Auth required:** Yes
- **Errors:** `409 GOOGLE_NOT_CONNECTED` · `502 GOOGLE_AUTH_FAILED` (Google
  unreachable) · `503 GOOGLE_AUTH_FAILED` (integration not configured)

**`CalendarView`**

```jsonc
[
  { "id": "primary", "summary": "Personal", "backgroundColor": "#4285f4",
    "primary": true, "selected": true }
]
```

A connection with **no stored selection** reports the primary calendar as
`selected`, matching the overlay's own default — the picker must show what is
actually rendering. A selected calendar that no longer exists on Google's side
simply **disappears** from the list rather than rendering as a phantom entry.

Unlike the events endpoint, this one **does** surface a Google failure as an
error: it is an explicit user action, and an empty list would read as "you have
no calendars" rather than "we could not reach Google".

#### PUT /v1/calendar/google/calendars

Replaces the selection. Returns the full list with the new state applied, so the
client does not need a follow-up read.

- **Auth required:** Yes
- **Body:** `{ "calendarIds": ["primary", "team@example.com"] }`
- **Errors:** `409 GOOGLE_NOT_CONNECTED` · `422 INVALID_INPUT` (field omitted,
  empty ID, or more than 5 calendars)

**Capped at 5 calendars**, enforced in the service layer — each selected calendar
is a separate Google API call per ranged fetch, and a disabled checkbox in the UI
is a hint, never enforcement. Duplicates are **collapsed rather than rejected**
(double-sending is not an attempt to exceed the cap). An explicit `[]` is valid
and selects nothing; **omitting** the field is a `422`, so an accidental empty
body cannot silently clear a selection.

**IDs are stored, never display names** — names change, IDs do not. A successful
update invalidates the user's cached event ranges, so the overlay reflects the
new selection immediately rather than after the TTL.

#### GET /v1/calendar/google-events

The overlay for a date range. **Never returns 5xx for a Google-side failure** —
this endpoint is context on the user's own task calendar, and a Google outage
must not be able to break the view that shows them their work.

- **Auth required:** Yes
- **Query:** `from` (required, `YYYY-MM-DD`, inclusive) · `to` (required,
  inclusive) · `refresh` (optional, `true` bypasses the cache)
- **Errors:** `422 INVALID_INPUT` — missing bound, malformed date, inverted
  range, or a span over **62 days** (matching the task calendar cap in §3.4).
  These are the *only* errors; Google problems arrive as a status inside a `200`.

**`GoogleEventView`** — all IDs are strings.

```jsonc
{
  "id": "abc123",
  "title": "Standup",          // "(no title)" when Google withholds it (private event)
  "start": "2026-08-03T09:00:00+03:00",  // RFC3339 in the user's timezone…
  "end":   "2026-08-03T09:30:00+03:00",
  "allDay": false,
  "calendarId": "primary",
  "htmlLink": "https://calendar.google.com/...",

  // Detail fields (NIC-1880) — every one is `omitempty`. An event carrying none
  // of them costs nothing extra on the wire, and the client MUST treat a missing
  // key and an empty value identically rather than rendering a blank row.
  "location": "Room 4",              // free text: a room, an address, a meeting URL
  "description": "Agenda: ship it",  // plain text — see below
  "organizer": "Ada Lovelace",       // display name, falling back to the email
  "attendeeCount": 3,                // includes the organizer
  "responseStatus": "accepted"       // accepted | declined | tentative | needsAction
}
```

**`description` is plain text, never HTML.** Google returns descriptions as HTML
(`<br>`, `<a href>`, whatever was pasted in). The tags are stripped **server-side**
so the browser, the mobile app and every test see the same string and no consumer
is ever handed markup it might be tempted to render. It is a display transform,
not a security boundary — clients must still put it in a text node. It is
truncated to **300 runes** (not bytes, so a Hebrew or Russian agenda is never cut
mid-codepoint) with a trailing `…`; a 62-day window across five calendars would
otherwise carry kilobytes of agenda text per event for what renders as a preview.

**`responseStatus` is the VIEWER's own RSVP**, matched on Google's `self` flag
rather than by email — a shared calendar may carry an alias or a group address,
so email matching returns nothing for exactly the users most likely to have
shared calendars. It is absent when the event has no attendee list (a personal
entry the user simply owns), which is distinct from `needsAction`.

**`attendeeCount` is a count, not the list.** Names and addresses of other people
are considerably more personal data than an at-a-glance overlay justifies
fetching, caching and shipping. A count of `1` means the organizer alone.

**Times are returned in the user's `users.timezone`, not UTC.** The client places
events on an hour grid; shipping UTC would make every consumer re-derive the
local hour — the exact conversion that goes wrong across a DST boundary. When
`allDay` is `true`, `start`/`end` are **plain `YYYY-MM-DD` dates** with no time:
a birthday is the 4th everywhere, and giving it an instant would drift it across
the day boundary for anyone in another zone. `end` is **exclusive**, matching
Google.

**Response — 200 OK**

```jsonc
{
  "data": {
    "events": [ /* GoogleEventView[] — always an array, never null */ ],
    "googleStatus": "ok"
  },
  "error": null
}
```

**Behaviour that the contract depends on:**

- **Recurring events are pre-expanded** (`singleEvents=true`), so a response
  never contains an RRULE — the client is not expected to implement RFC 5545.
- **Caching:** results are cached in-process for ~3 minutes, keyed by
  `(userId, calendarIds, from, to)`, to absorb the burst from view switching.
  `refresh=true` bypasses it. **Failures are never cached** — a blip must not
  force minutes of forced emptiness after Google recovers.
- **`invalid_grant` is terminal.** It means the user revoked access or changed
  their password; retrying is guaranteed to fail. The connection is deleted (which
  clears the token), the status becomes `disconnected`, and there is **no retry
  loop**. Every other failure leaves the connection intact.
- **A stale calendar degrades, it does not fail.** A deleted or unshared calendar
  is dropped from the results and the overall fetch still succeeds.
- **Fan-out is bounded** by the 5-calendar selection cap; each selected calendar
  is one Google call per ranged fetch.

---

### 3.17 Project Notes — E-053

Rich-text reference material filed under a project. **Notes are FREE and
unlimited** — this surface never returns `PLAN_LIMIT_EXCEEDED`. That is a
deliberate product decision, not an oversight: notes are where reference
material lives, and capping them would push users to keep it outside the app.

Routes are **flat and top-level** (`/notes`), consistent with `/attachments`,
not nested under the project.

#### Two view shapes — the list never carries the body

```jsonc
// NoteView — LIST shape. NO content field at all.
{ "id", "projectId", "title", "excerpt", "version", "createdAt", "updatedAt" }

// NoteDetailView — SCALAR shape.
{ "id", "projectId", "title", "content", "version", "createdAt", "updatedAt" }
```

`excerpt` is `content_text` truncated to **200 characters**. A project with 30
notes each holding a large document would otherwise make the list response
enormous — the same principle as `totalFocusSeconds` being scalar-only (E-049).
**Never render a note body from a list response.**

#### GET /v1/notes?projectId=

Lists one project's notes, ordered `updated_at DESC`. Returns `NoteListView`.

- **Auth required:** Yes
- A project the caller does not own → `404 RESOURCE_NOT_FOUND` (never an empty
  list, which would still confirm the project is reachable).

**Query parameters**

| Param    | Type   | Description                                                       |
| -------- | ------ | ----------------------------------------------------------------- |
| `projectId` | string | Required. Project to list notes for.                           |
| `cursor` | string | Opaque base64 page token from a prior `nextCursor`. Absent = first page. |
| `limit`  | int    | 1–100 (default 50)                                                |

**Response — 200 OK**

```json
{
  "items": [ { "id": "…", "projectId": "…", "title": "…", "excerpt": "…", "version": 1, "createdAt": "…", "updatedAt": "…" } ],
  "nextCursor": ""
}
```

Keyset on `(updated_at DESC, id DESC)`. A note being edited mid-scroll can appear on two consecutive pages (updated_at moves it to the head of a new page); this is documented behaviour, not a bug.

**Errors:** `RESOURCE_NOT_FOUND` (404), `INVALID_INPUT` (422 — `projectId` missing or bad cursor/limit)

#### POST /v1/notes → 201 `NoteDetailView`

```json
{
  "projectId": "p_abc",
  "title": "GTD structure thread",
  "content": { "type": "doc", "content": [] }
}
```

- `content` is **optional**; omitted ⇒ the empty-doc default `{"type":"doc","content":[]}`.
  It is `NOT NULL` in the database so the client never branches on null-vs-empty.
- `contentText` is **never accepted from the client** — sending it is a `422`.
  The mirror is always derived server-side by `flattenDoc` (see below).
- A project the caller does not own → `404`, never `403`.

**Errors:** `RESOURCE_NOT_FOUND` (404), `INVALID_INPUT` (422 — missing
`projectId`, empty title, title > 255, malformed `content`, or a body over 1 MB)

#### GET /v1/notes/{id} → 200 `NoteDetailView`

The full document. Cross-user access → `404`, never `403`.

#### PATCH /v1/notes/{id} → 200 `NoteDetailView` | 409

**`version` is REQUIRED.** This is optimistic concurrency: the client sends the
version it last read, and the guarded `UPDATE` applies only if the row is still
at it. A stale version returns **`409 CONFLICT`** and writes nothing.

```json
{ "title": "…", "content": { … }, "version": 7 }
```

- A successful save increments `version` and returns the new value.
- `title` and `content` are both optional — an omitted field keeps its stored
  value (PATCH is partial).
- ⚠️ **On 409 a client must surface a conflict state and STOP autosaving.**
  Never retry silently: a blind retry loop spins forever against a newer
  document and can clobber the other session's work.

**Errors:** `RESOURCE_NOT_FOUND` (404), `CONFLICT` (409 — stale version),
`INVALID_INPUT` (422 — missing `version`, bad title/content, body over 1 MB)

#### DELETE /v1/notes/{id} → 204

Hard delete. Best-effort reaps the note's attachments; a cleanup failure never
blocks the delete. Cross-user access → `404`.

#### GET /v1/notes/search?q=&excludeId= → 200 `MentionResult[]` (NIC-1962)

Search-as-you-type source for the `@note` mention autocomplete (E-057). Not
the same endpoint as `/v1/search?types=note`: that endpoint enforces a 2-char
minimum term and has no `excludeId`, both wrong for keystroke-level typeahead,
so this is a dedicated, purpose-built endpoint.

```json
[{ "id": "…", "title": "…" }]
```

- `q` — prefix-matched against the same `notes_search_idx` GIN column as full
  search (migration 044). A blank/non-alphanumeric `q` returns `[]`, never an
  error.
- `excludeId` — optional; omits that note id from results (the note currently
  open for editing — it cannot usefully mention itself).
- Row-isolated to the requesting user. Capped at 10 results, unordered by any
  client param — always rank then recency. Empty array, not null, when there
  are no matches.
- No plan gate — notes are Free and unlimited.

**Errors:** none beyond the standard auth 401. No 404/422 — an empty or
unmatched query is a valid empty result, not an error.

#### GET /v1/notes/{id}/backlinks → 200 `NoteView[]` (NIC-1963)

"Which notes mention this one" — powers the backlinks panel (E-057). Reads
`note_links` (migration 049, NIC-1961) via `target_note_id = :id` and returns
the source notes in the same list-shape `NoteView` as the notes list endpoint
— `{ id, projectId, title, excerpt, version, createdAt, updatedAt }`, **no
`content`**.

- Ownership of `:id` is checked first: a missing or foreign note is
  `404 RESOURCE_NOT_FOUND`, distinct from (and checked before) an empty
  backlink list — collapsing the two would let a guessed id be confirmed to
  exist by the empty-vs-404 distinction alone.
- Empty array, not null, when nothing links to the note.
- No plan gate — notes are Free and unlimited.

**Errors:** `RESOURCE_NOT_FOUND` (404)

#### Server-derived search text (`flattenDoc`)

`content_text` is the flattened plain text of the document, written alongside it
on **every** write. It is **mandatory, not an optimization**: extracting text
from arbitrary JSONB is not `IMMUTABLE`, so Postgres cannot generate the
`tsvector` from the `content` column directly — the mirror is what makes notes
searchable at all.

The walker reads only `{type, content[], text}`, so it is schema-agnostic: a new
node type added on the client needs no server change. Text leaves are joined
with a space so words from adjacent blocks never fuse into one token.

#### Attachments on notes

A note is a valid polymorphic attachment owner (`ownerType: "note"`). Quotas are
**inherited and shared**: 20 files per note, and the **100 MB byte budget is one
pool spanning tasks and notes**. Attachment _writes_ remain Pro-only
(`403 PLAN_LIMIT_EXCEEDED`); reads and deletes are open on any plan.

#### Real-time events

| Event          | Payload                       |
| -------------- | ----------------------------- |
| `note.created` | full `NoteView` (list shape)  |
| `note.updated` | `NoteView` — **no `content`** |
| `note.deleted` | `{ id }`                      |

`note.updated` deliberately breaks the full-payload convention. Autosave fires
every ~1–2s while typing; shipping a whole rich-text body per save would be
wasteful and racy. A client that needs the body refetches the scalar. Beyond
cache sync the event has a correctness role: a second tab can notice a change
and refetch _before_ the user types, defusing conflicts before `version` has to
reject them.

#### Project delete orphans notes, never destroys them

`notes.project_id` is nullable with `ON DELETE SET NULL`, mirroring
`tasks.project_id`. Deleting a project **orphans** its notes rather than
destroying reference material. The API still requires `projectId` on create;
nullability exists only to survive the delete. Orphans stay reachable through
**search**, which is user-scoped rather than project-scoped — that is the only
surface that can still find them.

---

## §4 Error Code Reference

All API errors return a consistent envelope:

```json
{
  "data": null,
  "error": {
    "code": "RESOURCE_NOT_FOUND",
    "message": "The requested resource was not found."
  }
}
```

These are the exact constants defined in `internal/apperror/errors.go` of the Go API:

| Code                      | HTTP Status | Description                                                                       |
| ------------------------- | ----------- | --------------------------------------------------------------------------------- |
| `INVALID_INPUT`           | 400         | Request body or query parameters failed validation                                |
| `INVALID_TOKEN`           | 401         | JWT is malformed, tampered, or not recognised                                     |
| `UNAUTHORIZED`            | 401         | No valid token provided, or credentials are incorrect                             |
| `EMAIL_NOT_VERIFIED`      | 403         | Credentials valid but email unverified (login gate; `REQUIRE_EMAIL_VERIFICATION`) |
| `FORBIDDEN`               | 403         | Authenticated but not permitted to access the resource                            |
| `PLAN_LIMIT_EXCEEDED`     | 403         | Action blocked by the user's plan (areas/projects/AI quota/recurrence rules; attachments: Free write, or > 20 files/owner) |
| `STORAGE_LIMIT_EXCEEDED`  | 403         | Attachment upload would exceed the 100 MB per-user storage cap (used/limit in message) |
| `PERMISSION_DENIED`       | 403         | Resource belongs to another user                                                  |
| `RESOURCE_NOT_FOUND`      | 404         | Generic — resource does not exist or is not visible to the requesting user        |
| `TASK_NOT_FOUND`          | 404         | Specific task resource not found                                                  |
| `PROJECT_NOT_FOUND`       | 404         | Specific project resource not found                                               |
| `AREA_NOT_FOUND`          | 404         | Specific area resource not found                                                  |
| `USER_NOT_FOUND`          | 404         | Specific user not found                                                           |
| `SESSION_NOT_FOUND`       | 404         | AI session not found                                                              |
| `MESSAGE_NOT_FOUND`       | 404         | AI message not found                                                              |
| `RECURRENCE_RULE_NOT_FOUND` | 404       | Recurrence rule not found, or belongs to another user (no existence leak)         |
| `CONFLICT`                | 409         | A resource with the same unique field already exists                              |
| `EMAIL_ALREADY_EXISTS`    | 409         | Registration attempted with an email already in use                               |
| `USERNAME_ALREADY_EXISTS` | 409         | Registration attempted with a username already taken                              |
| `DUPLICATE_NAME`          | 409         | Area or project name already exists for this user                                 |
| `IDEMPOTENCY_CONFLICT`    | 409         | Duplicate webhook event already processed                                         |
| `RATE_LIMITED`            | 429         | Too many requests — back off and retry after `Retry-After` header                 |
| `AI_LIMIT_REACHED`        | 429         | AI quota exhausted (Free 5 lifetime · Pro 500/month)                              |
| `AI_UNAVAILABLE`          | 503         | AI feature disabled (no key), provider 429/529, or first-token timeout            |
| `AI_PROVIDER_ERROR`       | 502         | AI provider rejected the request (400/401) — our fault, logged with `request_id`  |
| `AI_STREAM_ACTIVE`        | 409         | A response is already streaming for this session                                  |
| `INVALID_PROJECT_ID`      | 400         | Project ID provided is not valid or does not belong to this user                  |
| `INVALID_STATUS`          | 400         | Unrecognised status value for the resource type                                   |
| `INVALID_RECURRENCE`      | 422         | Malformed schedule: bad `freq`, `interval` outside 1..366, `byWeekday`/`byMonthday` out of range or set on the wrong `freq`, `endDate` before `startDate`, or a window containing no occurrence |
| `INVALID_DATE`            | 400         | Date string failed parsing or is out of acceptable range                          |
| `INVALID_PRIORITY`        | 400         | Unrecognised priority value                                                       |
| `INVALID_AI_CONTEXT`      | 400         | AI request payload is structurally invalid                                        |
| `INVALID_EMAIL`           | 400         | Email address failed format validation                                            |
| `WEAK_PASSWORD`           | 400         | Password fails policy: 8–72 chars with ≥1 uppercase and ≥1 lowercase              |
| `REQUIRED`                | 400         | A required field is missing from the request                                      |
| `DATABASE_ERROR`          | 500         | Unhandled database error                                                          |
| `INTERNAL_SERVER_ERROR`   | 500         | Unhandled server error                                                            |
| `SERVICE_UNAVAILABLE`     | 503         | Downstream service (AI provider, S3, etc.) is unreachable                         |

> **Frontend note:** All RTK Query error responses will have `error.data.error.code` set to one of the above strings. Use these constants (not HTTP status codes) for conditional error handling in the UI.

---

## §10 Internationalization (i18n)

> Code-canonical summary of the i18n architecture. Product rationale and the deferred email-localization epic live in Confluence space `NI`.

Nicoflow ships in **English (`en`)**, **Hebrew (`he`)**, and **Russian (`ru`)**. `en` is the source-of-record and the fallback (`fallbackLng: 'en'`). Hebrew is **RTL**.

### Ownership

- **The frontend owns ~all user-facing copy.** It uses `react-i18next` with namespaced JSON locale files; UI strings, form labels, and toast/error messages are resolved client-side via `t('...')`.
- **The backend emits almost no user-facing prose.** API error `message` fields are **developer-facing only** — the frontend localizes by mapping `error.code` (§4) to its own string and ignores the backend `message`. The only user-facing prose the API produces is the **2 transactional emails** (verify, reset), which remain **English-only for now** (see "Deferred").

### Frontend architecture (`nicoflow-frontend`, live app in `src/`)

- Library: `react-i18next` + `i18next` + `i18next-browser-languagedetector`. Config: `src/lib/i18n/index.ts`.
- **Namespaces:** `common`, `auth`, `area`, `project`, `task`, `bucket`, `nav`, `errors` (`defaultNS: 'common'`). Locale files: `src/lib/i18n/locales/{en,he,ru}/<ns>.json`.
- **Type-safe keys:** `src/lib/i18n/i18next.d.ts` derives the key type from the EN resource shape, so a missing/typo'd key fails `tsc` (consistent with the no-`any` rule). he/ru barrels are checked `satisfies Record<keyof Resources, unknown>` (permits CLDR plural variants like Russian `_few`/`_many`).
- **`errors.json` ≡ error codes.** The `errors` namespace keys are exactly the §4 error-code strings (plus success-toast keys). `showErrorToast`/`showSuccessToast` (`src/lib/utils/utils/helpers.ts`) resolve `error.code` → `errors:<CODE>` with a `GENERAL_ERROR` fallback. **`error.code` (§4) is therefore the localization key** — adding/renaming an error code is a cross-repo change that must also land in the three `errors.json` files.
- **Language preference:** stored in `localStorage('nicoflow-lang')` (mirrors the `next-themes` `nicoflow-theme` convention); detected via `localStorage` → `navigator`. **No server-side persistence** (see Deferred).
- **RTL:** on language change an `i18n.on('languageChanged')` listener sets `<html lang>` and `<html dir>` from `i18n.dir(lng)` (`'rtl'` for `he`). Layout mirroring uses **logical Tailwind properties** (`ms-/me-/ps-/pe-/start-/end-/text-start/text-end`), not physical `left/right`. Directional icons use `rtl:rotate-180`.
- **Switcher:** `src/components/LanguageSwitcher` (en/he/ru in native script), mounted in the Topbar.

### Deferred (documented, not built)

- **Localized transactional emails.** Requires a `users.language` column (migration), exposing it on the profile `PATCH`/`UserView`, plumbing stored language (or `Accept-Language`) into `pkg/emailutil/email.go`, and translating the 2 templates. Until then emails are English. This also enables **cross-device** language persistence (vs. the current `localStorage`-only).
- Locale-aware number/date/currency formatting beyond i18next defaults (revisit with billing).
- Languages beyond en/he/ru.

---

## §11 Observability & Error Tracking

> Pointer only — product rationale and the full PRD (E-038, Phase 5) live in Confluence §2 (`2.38 PRD: E-038`, page `50462730`) · Jira epic NIC-1441.

Today = **backend logging only**: `zerolog` → stdout → Render dashboard tail (ephemeral, no alerting), plus `request_id` middleware and `/v1/health`. No error tracking, no frontend observability, no persistent/alertable logs.

**Plan (E-038): Sentry-first, OTel-ready.** Committed build = Sentry error tracking on the Go API (panic + `>=500` capture, PII scrubbing, `release`/`APP_ENV` tags, `request_id`) and the React SPA (ErrorBoundary + source maps); `request_id` surfaced to the client so FE errors ↔ BE logs correlate. DSNs are env-driven (`SENTRY_DSN` / `VITE_SENTRY_DSN`) — **absent DSN = no-op** (safe local dev/CI). Deliberately deferred: OpenTelemetry tracing, Datadog APM/metrics, and a Render Log Stream → external drain (fast-follow) — instrument via OTel if/when added so the vendor stays swappable.

---

## §12 Accessibility

> Pointer only — full PRD (E-039, Phase 5) lives in Confluence §2 (`2.39 PRD: E-039`, page `50626563`) · Jira epic NIC-1442.

**Target: WCAG 2.1 AA by Web v1.** Accessibility was never scoped into the design-system/component epics; this epic is a one-time **audit + fix** of shipped components plus a **cross-cutting DoD amendment** (§2.4) so new work stays compliant. Committed: `axe`/`jest-axe` + `@axe-core/playwright` gating covered surfaces at 0 violations in CI; full keyboard operability + visible focus + dialog focus-trap/restore; AA contrast (4.5:1 text / 3:1 UI) on tokens + `ColorField`; `prefers-reduced-motion` honored across Framer Motion; labeled controls + landmarks + live-region; DnD reorder operable by keyboard (dnd-kit `KeyboardSensor` + announcements) with a "Move up / Move down" menu alternative. RTL (he) already done. Deferred: WCAG 2.2 net-new criteria, AAA, native/mobile audit (Phase 6), paid certification/VPAT.

---

## §13 Launch-Readiness Epics (Phase 5)

> Pointers only — full PRDs live in Confluence §2. Four epics that make the product legally + operationally shippable, added alongside E-029–032 (Billing · E2E · Web v1).

| Epic | Jira | Confluence | What |
| --- | --- | --- | --- |
| **E-040 Legal & Compliance** | NIC-1443 | 2.40 (`50593797`) | ToS + Privacy + cookie/consent + signup consent; **GDPR** account-delete UX (wires existing `DELETE /v1/users/me` soft-delete) + data-export endpoint. Hard gate on Web v1; consent gates E-041. |
| **E-041 Product Analytics (PostHog)** | NIC-1444 | 2.41 (`50921473`) | Behavioral event tracking (cross-platform web+mobile+desktop). Typed event taxonomy, funnels (signup→activation, free→Pro), consent-gated, `VITE_POSTHOG_KEY` (absent = no-op). Mobile SDK deferred to E-034+. |
| **E-042 Help, Support & Static Content** | NIC-1445 | 2.42 (`50823171`) | Help/FAQ, support/contact, onboarding empty-states, marketing/public pages, **SEO/meta/OG** on shareable pages, README polish. |
| **E-043 Uptime, Logs & Status (Betterstack)** | NIC-1446 | 2.43 (`50692109`) | Render Log Stream → Betterstack Logs (persistent/searchable/alertable), `/v1/health` uptime monitor + alerts, public status page. Infra layer — complements Sentry (E-038), no overlap. |

**Three monitoring layers, no overlap:** Sentry (E-038) = errors · PostHog (E-041) = behavior · Betterstack (E-043) = infra logs/uptime. **Deferred (not now):** PWA/offline (native app covers mobile, Phase 6); error/empty-state polish folds into the E-039 a11y DoD, not a separate epic.

---

## §14 Production-Hardening Epics (Phase 5)

> Pointers only — full PRDs in Confluence §2. Five epics that close operational + security gaps before Web v1. Two (⚠️) are launch-blockers to pull earlier.

| Epic | Jira | Confluence | What |
| --- | --- | --- | --- |
| **⚠️ E-044 Email Deliverability** | NIC-1447 | 2.44 (`50659337`) | Mailtrap is a **dev sandbox — mail never delivers**. Swap a real ESP (Resend/Postmark) + SPF/DKIM/DMARC. Blocks verify/reset email in prod. |
| **E-045 Bot & Abuse Defense** | NIC-1448 | 2.45 (`50626587`) | Cloudflare **Turnstile** on register + forgot-password (BE verify). IP rate-limits alone don't stop a distributed bot pool. No-op without a key. |
| **⚠️ E-046 Backup & DR** | NIC-1449 | 2.46 (`50626609`) | Render free PG **expires + deletes ~30d**. Persistent plan + PITR + a *tested* restore runbook + retention policy. |
| **E-047 Security Hardening** | NIC-1450 | 2.47 (`50659358`) | Dependabot + Trivy/govulncheck/pnpm-audit + gitleaks in CI + a pre-v1 security review. Lands the deferred vuln-rescan item. |
| **E-048 Web Performance & Feature Flags** | NIC-1451 | 2.48 (`51019777`) | Lighthouse CI + bundle-size gate + code-split + **PostHog feature flags** (kill-switch/dark-launch) + cache/CDN headers. |

**Green (post-launch, documented not built):** admin panel (user lookup / impersonate / moderation) · hard-purge job for soft-deleted accounts (GDPR retention, ties E-040/E-046) · in-app product tour. Revisit once there are real users.
