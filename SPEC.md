# Nicoflow SPEC (index)

> **Canonical product, PRD, architecture, and engineering docs live in Confluence space `NI`.**
> This file holds only the code-canonical API contract (§3) and error codes (§4) for offline and skill use.
> Do not hand-edit §3/§4 here — regenerate from the code after running `contract-check`.

---

## Product, PRDs & Architecture → Confluence

| Section | Confluence link |
| ------- | --------------- |
| 1. Product Overview (What is Nicoflow, Hierarchy, Plan Tiers, Roadmap) | [Confluence §1](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/21037057) |
| 2. Product Requirements / PRDs (E-001 … E-037) | [Confluence §2](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/21200935) |
| 3. Architecture & Technical Design (System, DB Schema, Backend, Frontend, Auth, WS, S3, Billing, Mobile, Design System) | [Confluence §3](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/21037111) |
| 5. Engineering Operations (Local Dev, CI/CD, Branching, Deployment, Env Vars, Migrations, Testing) | [Confluence §5](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/21070021) |
| 6. Architecture Decision Records (ADR-001 … ADR-006) | [Confluence §6](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/21561366) |
| 7. Sprints (Overview + Sprint 01 … 25) | [Confluence §7](https://nicoflow.atlassian.net/wiki/spaces/NI/pages/21168185) |

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

| Field      | Type   | Required | Constraints                                     |
| ---------- | ------ | -------- | ----------------------------------------------- |
| `email`    | string | Yes      | Valid email format                              |
| `password` | string | Yes      | 8–72 chars, ≥1 uppercase, ≥1 lowercase          |
| `username` | string | Yes      | 3–20 chars, alphanumeric only                   |
| `platform` | string | No       | `"web"` \| `"mobile"` — defaults to `"web"`     |

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
    "imageUrl": "",
    "status": "regular"
  }
}
```

**Errors**

| Code                      | HTTP | Meaning                                |
| ------------------------- | ---- | -------------------------------------- |
| `EMAIL_ALREADY_EXISTS`    | 409  | Email already in use                   |
| `USERNAME_ALREADY_EXISTS` | 409  | Username already taken                 |
| `INVALID_EMAIL`           | 422  | Email failed format validation         |
| `WEAK_PASSWORD`           | 400  | Password fails the policy above        |
| `INVALID_INPUT`           | 422  | Other validation failed (e.g. username)|
| `RATE_LIMITED`            | 429  | Too many registration attempts         |

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
  "platform": "web"
}
```

| Field        | Type    | Required | Notes                                                        |
| ------------ | ------- | -------- | ------------------------------------------------------------ |
| `identifier` | string  | Yes      | Email address **or** username. (Legacy `email` still accepted.) |
| `password`   | string  | Yes      |                                                              |
| `remember`   | boolean | Yes      | `true` → 7-day refresh token; `false` → 24-hour              |
| `platform`   | string  | No       | `"web"` \| `"mobile"`                                        |

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

Confirm a user's email address using the token from the verification email. *(Login enforcement is gated by the `REQUIRE_EMAIL_VERIFICATION` config flag — when enabled, unverified accounts are rejected at login with `EMAIL_NOT_VERIFIED`.)*

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
  "imageUrl": "https://...",
  "plan": "free"
}
```

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
  "email": "new@example.com",
  "timezone": "Europe/London",
  "theme": "dark"
}
```

**Response — 200 OK** — Updated `IUser` object

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

Delete an area. Contained projects have their `area_id` set to NULL (SET NULL cascade).

- **Auth required:** Yes

**Response — 204 No Content**

**Errors:** `AREA_NOT_FOUND` (404)

---

### 3.3 Projects

> **`IProject` shape** — all IDs are strings (UUID). Fields: `id: string`, `areaId: string | null`, `name: string`, `status: "active"|"completed"|"archived"`, `folderIcon: string`, `dueDate?: string | null` (RFC 3339), `isFavorite?: boolean`, `description?: string | null`, `displayOrder?: number`, `createdAt: string`, `updatedAt: string`. No embedded `area` object is returned.

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

Update a project. All fields optional. Pass `areaId` to move the project to a different area; pass `areaId: null` to detach it from any area.

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

**Errors:** `PROJECT_NOT_FOUND` (404), `INVALID_INPUT` (422), `INVALID_STATUS` (422), `DUPLICATE_NAME` (409)

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

> **`ITask` shape** — all IDs are strings. Fields: `id: string`, `projectId: string`, `title: string`, `notes?: string | null`, `status: "inbox"|"active"|"done"|"cancelled"`, `priority: "low"|"medium"|"high"`, `dueDate?: string | null`, `scheduledFor?: string | null`, `estimatedMinutes?: number | null`, `url?: string | null`, `displayOrder?: number`, `completedAt?: string | null`, `createdAt: string`, `updatedAt: string`.

#### GET /v1/projects/:projectId/tasks

List all tasks within a project.

- **Auth required:** Yes

**Query parameters**

| Param       | Type   | Description                                               |
| ----------- | ------ | --------------------------------------------------------- |
| `status`    | string | Filter by `inbox` \| `active` \| `done` \| `cancelled`   |
| `priority`  | string | Filter by `low` \| `medium` \| `high`                    |
| `sortField` | string | `dueDate` \| `priority` \| `title` \| `createdAt`        |
| `sortOrder` | string | `asc` \| `desc`                                          |

**Response — 200 OK** — `ITask[]`

---

#### GET /v1/tasks/:id

Retrieve a single task.

- **Auth required:** Yes

**Response — 200 OK** — `ITask`

**Errors:** `RESOURCE_NOT_FOUND` (404), `PERMISSION_DENIED` (403)

---

#### POST /v1/projects/:projectId/tasks

Create a task inside a project.

- **Auth required:** Yes

**Request body**

```json
{
  "title": "Write spec",
  "notes": "Write the full API specification",
  "priority": "high",
  "dueDate": "2026-05-10",
  "estimatedMinutes": 90,
  "url": "https://notion.so/...",
  "scheduledFor": "2026-05-02"
}
```

| Field              | Type   | Required | Constraints                                            |
| ------------------ | ------ | -------- | ------------------------------------------------------ |
| `title`            | string | Yes      | 1–255 characters                                       |
| `notes`            | string | No       | Free-form text                                         |
| `priority`         | string | No       | `"low"` \| `"medium"` \| `"high"` — default `"medium"` |
| `dueDate`          | string | No       | ISO 8601 date                                          |
| `estimatedMinutes` | number | No       | 1–1440                                                 |
| `url`              | string | No       | Valid URL                                              |
| `scheduledFor`     | string | No       | ISO 8601 date — for time-spread view                   |

**Response — 201 Created** — `ITask`

**Errors:** `RESOURCE_NOT_FOUND` (404 — project not found), `PERMISSION_DENIED` (403), `INVALID_INPUT` (422)

---

#### PATCH /v1/tasks/:id

Update a task.

- **Auth required:** Yes

**Request body** (all fields optional)

```json
{
  "title": "Write spec v2",
  "notes": "...",
  "status": "active",
  "priority": "high",
  "dueDate": "2026-05-15",
  "estimatedMinutes": 120,
  "url": "https://...",
  "scheduledFor": "2026-05-03",
  "displayOrder": 0,
  "completedAt": "2026-05-10T14:30:00Z"
}
```

**Response — 200 OK** — Updated `ITask`

**Errors:** `RESOURCE_NOT_FOUND` (404), `PERMISSION_DENIED` (403), `INVALID_INPUT` (422)

---

#### DELETE /v1/tasks/:id

Delete a task.

- **Auth required:** Yes

**Response — 204 No Content**

**Errors:** `RESOURCE_NOT_FOUND` (404), `PERMISSION_DENIED` (403)

---

### 3.5 Subtasks

#### GET /v1/tasks/:taskId/subtasks

List subtasks for a task.

- **Auth required:** Yes

**Response — 200 OK**

```json
[
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
    "dueDate": "2026-05-05",
    "estimatedMinutes": 60
  }
}
```

| Field              | Type   | Required | Values                                    |
| ------------------ | ------ | -------- | ----------------------------------------- |
| `processingResult` | string | Yes      | `"task"` \| `"note"` \| `"trash"`         |
| `projectId`        | string | No       | Required when `processingResult = "task"` |
| `taskDetails`      | object | No       | Required when `processingResult = "task"` |
| `noteDetails`      | object | No       | Required when `processingResult = "note"` |

**Response — 200 OK** — Updated `IBucket` (with `processedAt` and `processingResult` populated)

**Errors:** `RESOURCE_NOT_FOUND` (404), `CONFLICT` (409 — already processed), `INVALID_INPUT` (422)

---

#### DELETE /v1/bucket/:id

Delete an inbox item.

- **Auth required:** Yes

**Response — 204 No Content**

---

### 3.7 Time Spread View

#### GET /v1/time-spread

Retrieve tasks bucketed into today / tomorrow / this week for the authenticated user.

- **Auth required:** Yes

**Response — 200 OK**

```json
{
  "today": [ ...ITask[] ],
  "tomorrow": [ ...ITask[] ],
  "thisWeek": [ ...ITask[] ]
}
```

Tasks are included based on their `scheduledFor` or `dueDate` field relative to the server's evaluation of the current date in the user's timezone.

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
  "dueDate": null,
  "confidence": 0.92
}
```

**Errors:** `PLAN_LIMIT_EXCEEDED` (403), `RATE_LIMITED` (429)

---

### 3.9 AI Assistant

#### POST /v1/ai/sessions

Start a new AI assistant session.

- **Auth required:** Yes
- **Plan limit:** Free users have 10 AI requests/month total across all sessions

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

Retrieve a session with its full message history.

- **Auth required:** Yes

**Response — 200 OK** — `IAISession` with `messages: IAIMessage[]`

---

#### POST /v1/ai/sessions/:id/messages

Send a message to the AI assistant.

- **Auth required:** Yes
- **Plan limit:** Counted against monthly AI request quota

**Request body**

```json
{ "content": "Help me break down the Q3 launch project into tasks" }
```

**Response — 200 OK**

```json
{
  "message": {
    "id": "msg_xyz",
    "role": "assistant",
    "content": "Sure, here are...",
    "createdAt": "..."
  },
  "usage": {
    "promptTokens": 420,
    "completionTokens": 180,
    "requestsThisMonth": 3,
    "requestsLimit": 10
  }
}
```

**Errors:** `AI_LIMIT_REACHED` (403 — monthly quota exceeded), `RATE_LIMITED` (429)

---

#### DELETE /v1/ai/sessions/:id

Delete an AI session and all its messages.

- **Auth required:** Yes

**Response — 204 No Content**

---

### 3.10 Search

#### GET /v1/search

Full-text search across tasks, projects, and areas.

- **Auth required:** Yes

**Query parameters**

| Param    | Type   | Required | Description                                          |
| -------- | ------ | -------- | ---------------------------------------------------- |
| `q`      | string | Yes      | Search query (min 2 characters)                      |
| `type`   | string | No       | `"tasks"` \| `"projects"` \| `"areas"` — default all |
| `limit`  | number | No       | Max results per type — default 10                    |
| `offset` | number | No       | Pagination offset — default 0                        |

**Response — 200 OK**

```json
{
  "tasks":    [ { ...ITask,    "_highlight": { "title": "Write <mark>spec</mark>" } } ],
  "projects": [ { ...IProject, "_highlight": { "name": "..." } } ],
  "areas":    [ { ...IArea,    "_highlight": { "name": "..." } } ]
}
```

---

### 3.11 Notifications

#### GET /v1/notifications

List all notifications for the authenticated user.

- **Auth required:** Yes

**Response — 200 OK** — `INotification[]`

```json
[
  {
    "id": "01J...",
    "userId": "01K...",
    "type": "push",
    "trigger": "before_due",
    "status": "pending",
    "message": "Task due soon",
    "read": false,
    "createdAt": "..."
  }
]
```

---

#### PATCH /v1/notifications/:id

Mark a notification as read or unread.

- **Auth required:** Yes

**Request body**

```json
{ "read": true }
```

**Response — 200 OK** — Updated `INotification`

---

#### PATCH /v1/notifications/read-all

Mark all notifications as read.

- **Auth required:** Yes

**Response — 204 No Content**

---

#### DELETE /v1/notifications/:id

Delete a notification.

- **Auth required:** Yes

**Response — 204 No Content**

---

#### GET /v1/notifications/preferences

Get the user's notification preferences.

- **Auth required:** Yes

**Response — 200 OK** — `INotificationPreferences`

```json
{
  "email": true,
  "push": true,
  "sms": false,
  "beforeDueMinutes": 30,
  "afterDueMinutes": 60
}
```

---

#### PUT /v1/notifications/preferences

Replace the user's notification preferences.

- **Auth required:** Yes

**Request body** — Full `INotificationPreferences` object (same shape as GET response)

**Response — 200 OK** — Updated `INotificationPreferences`

---

### 3.12 File Attachments (S3)

Attachments follow a two-step presigned-URL pattern — the client uploads directly to S3.

#### POST /v1/tasks/:taskId/attachments

Initiate an attachment upload. Returns a presigned S3 PUT URL.

- **Auth required:** Yes
- **Plan limit:** Free = 5 attachments/task · Pro = 20 attachments/task · 25 MB per file (both plans)

**Request body**

```json
{ "filename": "screenshot.png", "mimeType": "image/png", "fileSize": 204800 }
```

**Response — 201 Created**

```json
{
  "uploadUrl": "https://s3.amazonaws.com/nicoflow-uploads/...?X-Amz-Signature=...",
  "attachmentId": "att_abc123"
}
```

The client performs `PUT <uploadUrl>` with the file bytes and `Content-Type` header. After upload completes, the attachment becomes visible via the GET endpoint.

---

#### GET /v1/tasks/:taskId/attachments

List all attachments for a task.

- **Auth required:** Yes

**Response — 200 OK**

```json
[
  {
    "id": "att_abc123",
    "taskId": "01J...",
    "filename": "screenshot.png",
    "mimeType": "image/png",
    "fileSize": 204800,
    "createdAt": "..."
  }
]
```

---

#### GET /v1/attachments/:id/download

Get a presigned S3 download URL (valid for 15 minutes).

- **Auth required:** Yes

**Response — 200 OK**

```json
{
  "downloadUrl": "https://s3.amazonaws.com/nicoflow-uploads/...?X-Amz-Signature=..."
}
```

---

#### DELETE /v1/attachments/:id

Delete an attachment. The S3 object is also removed.

- **Auth required:** Yes

**Response — 204 No Content**

---

### 3.13 Billing & Subscriptions

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

### 3.14 Real-Time Sync (WebSocket)

#### GET /v1/ws

Upgrade to a WebSocket connection for real-time push events.

- **Auth required:** Yes (JWT passed as query param — invalid → close `1008`)

**Connection URL**

```
wss://api.nicoflow.app/v1/ws?token=<jwt>
```

**Server-pushed event shape**

```json
{
  "event": "task.updated",
  "payload": { ...ITask },
  "timestamp": "2026-05-01T12:00:00Z"
}
```

**Event types**

| Event             | Payload         |
| ----------------- | --------------- |
| `task.created`    | `ITask`         |
| `task.updated`    | `ITask`         |
| `task.deleted`    | `{ id }`        |
| `project.created` | `IProject`      |
| `project.updated` | `IProject`      |
| `project.deleted` | `{ id }`        |
| `area.created`    | `IArea`         |
| `area.updated`    | `IArea`         |
| `area.deleted`    | `{ id }`        |
| `notification`    | `INotification` |

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

| Code                    | HTTP Status | Description                                                                |
| ----------------------- | ----------- | -------------------------------------------------------------------------- |
| `INVALID_INPUT`         | 400         | Request body or query parameters failed validation                         |
| `INVALID_TOKEN`         | 401         | JWT is malformed, tampered, or not recognised                              |
| `UNAUTHORIZED`          | 401         | No valid token provided, or credentials are incorrect                      |
| `EMAIL_NOT_VERIFIED`    | 403         | Credentials valid but email unverified (login gate; `REQUIRE_EMAIL_VERIFICATION`) |
| `FORBIDDEN`             | 403         | Authenticated but not permitted to access the resource                     |
| `PLAN_LIMIT_EXCEEDED`   | 403         | Action blocked by the user's plan (areas/projects/AI quota)                |
| `PERMISSION_DENIED`     | 403         | Resource belongs to another user                                           |
| `RESOURCE_NOT_FOUND`    | 404         | Generic — resource does not exist or is not visible to the requesting user |
| `TASK_NOT_FOUND`        | 404         | Specific task resource not found                                           |
| `PROJECT_NOT_FOUND`     | 404         | Specific project resource not found                                        |
| `AREA_NOT_FOUND`        | 404         | Specific area resource not found                                           |
| `USER_NOT_FOUND`        | 404         | Specific user not found                                                    |
| `SESSION_NOT_FOUND`     | 404         | AI session not found                                                       |
| `MESSAGE_NOT_FOUND`     | 404         | AI message not found                                                       |
| `CONFLICT`              | 409         | A resource with the same unique field already exists                       |
| `EMAIL_ALREADY_EXISTS`  | 409         | Registration attempted with an email already in use                        |
| `USERNAME_ALREADY_EXISTS` | 409       | Registration attempted with a username already taken                       |
| `DUPLICATE_NAME`        | 409         | Area or project name already exists for this user                          |
| `IDEMPOTENCY_CONFLICT`  | 409         | Duplicate webhook event already processed                                  |
| `RATE_LIMITED`          | 429         | Too many requests — back off and retry after `Retry-After` header          |
| `AI_LIMIT_REACHED`      | 403         | Free-tier AI monthly quota (10 requests) exhausted                         |
| `INVALID_PROJECT_ID`    | 400         | Project ID provided is not valid or does not belong to this user           |
| `INVALID_STATUS`        | 400         | Unrecognised status value for the resource type                            |
| `INVALID_DATE`          | 400         | Date string failed parsing or is out of acceptable range                   |
| `INVALID_PRIORITY`      | 400         | Unrecognised priority value                                                |
| `INVALID_AI_CONTEXT`    | 400         | AI request payload is structurally invalid                                 |
| `INVALID_EMAIL`         | 400         | Email address failed format validation                                     |
| `WEAK_PASSWORD`         | 400         | Password fails policy: 8–72 chars with ≥1 uppercase and ≥1 lowercase       |
| `REQUIRED`              | 400         | A required field is missing from the request                               |
| `DATABASE_ERROR`        | 500         | Unhandled database error                                                   |
| `INTERNAL_SERVER_ERROR` | 500         | Unhandled server error                                                     |
| `SERVICE_UNAVAILABLE`   | 503         | Downstream service (AI provider, S3, etc.) is unreachable                  |

> **Frontend note:** All RTK Query error responses will have `error.data.error.code` set to one of the above strings. Use these constants (not HTTP status codes) for conditional error handling in the UI.
