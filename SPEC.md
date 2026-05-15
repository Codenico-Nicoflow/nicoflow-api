# Nicoflow — Master Product & API Specification

> **Version:** 2.0 — 2026-05-09
> **Status:** Draft
> **Source of truth for:** product scope, feature gating, epic roadmap, API contract, data shapes, error codes, billing, and mobile architecture.

---

## Table of Contents

1. [Product Overview](#1-product-overview)
2. [Feature Status & Plan Gating](#2-feature-status--plan-gating)
3. [API Endpoint Reference](#3-api-endpoint-reference)
4. [Error Code Reference](#4-error-code-reference)
5. [Plan Limits](#5-plan-limits)
6. [Billing Architecture](#6-billing-architecture)
7. [Mobile App Architecture](#7-mobile-app-architecture-planned)
8. [Backend Architecture](#8-backend-architecture-canonical-reference)
9. [Frontend Architecture](#9-frontend-architecture)

---

## 1. Product Overview

Nicoflow is a task management platform inspired by GTD (Getting Things Done) principles — not a strict GTD implementation. It provides structured capture, organisation, and execution of tasks within projects that live inside areas of responsibility.

### Infrastructure

| Component    | Technology                                   | Hosting          |
| ------------ | -------------------------------------------- | ---------------- |
| Web App      | React 19, Vite 7, TypeScript 5.8, Tailwind 4 | Vercel           |
| Mobile App   | Expo ~52, React Native (planned)             | EAS / App Stores |
| API          | Go (REST)                                    | Render.com       |
| Database     | PostgreSQL 15                                | Render.com       |
| File Storage | AWS S3                                       | AWS              |
| Auth Tokens  | JWT (Bearer) + HttpOnly refresh cookie       | —                |
| Billing      | Lemon Squeezy                                | —                |
| Real-Time    | WebSocket (`/v1/ws`)                         | —                |

### Hierarchy

```
User
└── Area (max 3 on Free, unlimited on Pro)
    └── Project (max 5 total on Free, unlimited on Pro)
        └── Task
            └── Subtask
```

Inbox items (bucket) are user-level and have no project until processed.

---

## 2. Feature Status & Plan Gating

This section is the canonical roadmap for Nicoflow v1 + mobile phase. All work is broken into **37 epics** across **25 two-week sprints** (~12 months) for a solo developer. Engineering/tooling work is treated as first-class epics — not assumed done.

> **Sprint cadence:** 2 weeks. Sprint 01 = project start (Week 1–2).
> **Status values:** `Draft` → `In Progress` → `Done`

---

### 2.1 Epic Inventory

#### Phase 0 — Foundation & Engineering Setup (Sprints 01–04)

_Goal: Both repos fully bootstrapped, CI/CD green, design system tokens defined, core component library built, Storybook running, and the test harness in place. No product features yet. "Done" = a new engineer can clone, run, and contribute in under 30 minutes._

| Epic  | Title                                                                                                                                                                                                                                                                                                      | Sprints | Status | Free | Pro |
| ----- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- | ------ | ---- | --- |
| E-001 | Frontend Repo Setup & Tooling (Vite, TypeScript strict, ESLint flat config, Prettier, Husky, lint-staged, path aliases, pnpm workspace)                                                                                                                                                                    | 01      | Draft  | —    | —   |
| E-002 | Backend Repo Setup & Tooling (Go modules, chi router, pgx, zerolog, .env loading, project structure, Makefile, golangci-lint)                                                                                                                                                                              | 01      | Draft  | —    | —   |
| E-003 | CI/CD Pipeline — GitHub Actions (ci.yml: lint + type-check + test + build; deploy-staging.yml; deploy-production.yml; Vercel + Render integration; required secrets documented)                                                                                                                            | 02      | Draft  | —    | —   |
| E-004 | Database Setup & Migration Tooling (golang-migrate CLI, migrations/ directory, 000_init baseline, Makefile targets: migrate-up / migrate-down / migrate-version, local PostgreSQL setup documented)                                                                                                        | 02      | Draft  | —    | —   |
| E-005 | Design System Foundation (Tailwind 4 CSS custom properties, shadcn/ui New York + neutral, colour tokens, spacing scale, typography scale, dark/light theme via next-themes, ModeToggle component)                                                                                                          | 03      | Draft  | —    | —   |
| E-006 | Component Library — Phase 1 (NameField, DescriptionField, PriorityField, DueDateField, EstimatedTimeField, UrlField, IconField, CheckboxField, FormDialog, ConfirmDialog, CustomDialog, DialogFieldGrid, AnimatedListItem, ListItemCard, ItemActionsMenu, EmptyState, Timestamp, LazyIcon, LoadingOverlay) | 03–04   | Draft  | —    | —   |
| E-007 | Storybook Setup & Stories — Phase 1 (Storybook 10 + @storybook/react-vite + @storybook/addon-vitest; global decorator withStoryProviders; storybookStore; StoryFormWrapper; mock factories; stories for every E-006 component)                                                                             | 04      | Draft  | —    | —   |
| E-008 | Testing Infrastructure (Vitest 3 + jsdom + globals; @testing-library/react + user-event + jest-dom; MSW 2 node interceptors; renderComponent helper; setup.ts browser API stubs; Playwright config + webServer; first smoke test proving the harness works)                                                | 04      | Draft  | —    | —   |

---

#### Phase 1 — Authentication & Core Data Model (Sprints 05–08)

_Goal: A user can register, log in, create areas and projects, and manage tasks — end-to-end, with the sidebar fully functional. "Done" = happy path E2E test passes for auth + areas + projects + tasks._

| Epic  | Title                                                                                                                                                                                                                                                                                                                                 | Sprints | Status | Free                 | Pro       |
| ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- | ------ | -------------------- | --------- |
| E-009 | Authentication — Backend (DB migrations 001–002; register, login, logout, logout-all, refresh-token, forgot-password, reset-password, get/update profile, change-password endpoints; bcrypt cost 12; JWT HS256 15 min; refresh token SHA-256 hash + 7-day TTL + single-use rotation + reuse detection; rate limits on auth endpoints) | 05      | Draft  | ✅                   | ✅        |
| E-010 | Authentication — Frontend (authApi RTK slice; redux-persist auth key; async-mutex baseQuery reauth; SignIn, SignUp, ForgotPassword, ResetPassword pages; AuthLayout; PrivateRoutes guard with state.from redirect; useAppUser hook; token stored in localStorage; SignForm shared layout component)                                   | 06      | Draft  | ✅                   | ✅        |
| E-011 | Areas & Projects — Backend (DB migrations 003–004; areas CRUD + reorder + plan limit enforcement; projects CRUD + reorder + favourite + status + area_id nullable; GET /areas/with-projects; PLAN_LIMIT_EXCEEDED for Free tier; user-scoped row isolation)                                                                            | 07      | Draft  | 3 areas / 5 projects | Unlimited |
| E-012 | Areas & Projects — Frontend (areaApi + projectApi RTK slices; AppSidebar with collapsible area sections; DragAndDropContext for project reorder between areas; AreaDialog, AreaContextMenu; ProjectDialog, ProjectDeleteDialog, ProjectAreaField; ItemActionsMenu; sidebar Footer with user profile + theme toggle)                   | 07–08   | Draft  | 3 areas / 5 projects | Unlimited |
| E-013 | Task Management — Backend (DB migrations 005–006; tasks CRUD + reorder + scheduled_for + status + priority + subtasks; GET /tasks with filters; GET /time-spread; user-scoped; ON DELETE SET NULL for project_id)                                                                                                                     | 08      | Draft  | ✅                   | ✅        |

---

#### Phase 2 — Task UI, Inbox & Time Spread (Sprints 09–12)

_Goal: Full task management UI, inbox capture and processing, and time-spread views all working in browser. "Done" = user can capture a thought, process it into a task, and see it appear in Today._

| Epic  | Title                                                                                                                                                                                                                                                                                                                                   | Sprints | Status | Free | Pro |
| ----- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- | ------ | ---- | --- |
| E-014 | Task Management — Frontend (taskApi RTK slice; ProjectView page; TasksSection + AnimatedListItem; TaskItem with status toggle + priority badge + due date; TaskDialog with all field components; TaskFilters; TaskSearch; TaskDeleteDialog; subtask inline creation; drag-and-drop reorder within project)                              | 09–10   | Draft  | ✅   | ✅  |
| E-015 | Inbox / Quick Capture — Backend (DB migration for bucket table; GET/POST/PATCH /bucket; DELETE /bucket/:id; POST /bucket/:id/process → processing_result: task/note/trash; created_task_id FK on TASK processing; processed_at timestamp)                                                                                               | 10      | Draft  | ✅   | ✅  |
| E-016 | Inbox / Quick Capture — Frontend (bucketApi RTK slice; Bucket page with Tabs: Inbox / Archived; BucketQuickInput sticky card; BucketList; BucketProcessDialog with BucketProjectSelector + BucketProcessList + full task form; BucketEditDialog; BucketDeleteDialog; processing result badges)                                          | 11      | Draft  | ✅   | ✅  |
| E-017 | Time Spread View — Backend + Frontend (GET /time-spread returning today/tomorrow/thisWeek task arrays; Today, Tomorrow, NextSevenDays pages; QuickAccess sidebar section with nav items; timezone-aware evaluation on backend)                                                                                                          | 11      | Draft  | ✅   | ✅  |
| E-018 | Unit & Integration Tests — Phase 1 (Vitest unit tests for all utility functions, Zod schemas, RTK slice selectors; MSW integration tests for auth flows, area/project CRUD, task CRUD, bucket capture + process; Go table-driven tests for all service + repository layers for E-009–E-013; coverage targets: 90%+ utils, 80%+ service) | 12      | Draft  | —    | —   |

---

#### Phase 3 — Search, Notifications & User Settings (Sprints 13–14)

_Goal: Users can search across all content, manage notification preferences, and update their profile. "Done" = search returns highlighted results; notification list marks read; profile update persists._

| Epic  | Title                                                                                                                                                                                                                                           | Sprints | Status | Free | Pro |
| ----- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- | ------ | ---- | --- |
| E-019 | Search — Backend + Frontend (GET /search with q + type + limit + offset params; PostgreSQL tsvector full-text across tasks/projects/areas; \_highlight fields in response; search UI: command-palette style or dedicated page; debounced input) | 13      | Draft  | ✅   | ✅  |
| E-020 | Notifications — Backend + Frontend (GET/PATCH/DELETE notifications; PATCH /notifications/read-all; GET/PUT /notifications/preferences: email/push/sms toggles + timing; notification list UI; unread badge in sidebar)                          | 13–14   | Draft  | ✅   | ✅  |
| E-021 | User Settings — Frontend (Profile page: update firstName/lastName/timezone; Change Password form; Theme toggle (already in Footer but surfaced in Settings); avatar/imageUrl; links to Privacy Policy + Terms of Service pages; help page)      | 14      | Draft  | ✅   | ✅  |

---

#### Phase 4 — Real-Time Sync, File Attachments & AI (Sprints 15–18)

_Goal: Changes in one browser tab reflect instantly in another; users can attach files to tasks; Pro users can chat with the AI assistant. "Done" = WS events flow end-to-end; file upload completes via presigned URL; AI session returns a response._

| Epic  | Title                                                                                                                                                                                                                                                                                                                                                                                       | Sprints | Status | Free                  | Pro                    |
| ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- | ------ | --------------------- | ---------------------- |
| E-022 | Real-Time Sync — Backend (WebSocket hub: per-user registry, goroutine-per-client read/write pump; JWT auth via ?token= query param; heartbeat: 30s ping / 10s pong timeout; event emission wired into every service mutation: task/project/area/bucket CRUD → broadcast to user's connections; full payload messages not diffs)                                                             | 15      | Draft  | ✅                    | ✅                     |
| E-023 | Real-Time Sync — Frontend (useWebSocket custom hook: connect with auth token, exponential backoff reconnection 1s/2s/4s/8s/16s/30s cap, polling fallback after 3 failures; WS message handler → invalidateApiTags per event type; "Live updates paused" banner; pre-connection JWT expiry check)                                                                                            | 15–16   | Draft  | ✅                    | ✅                     |
| E-024 | File Attachments — Backend (DB migration 012 for file_attachments table; POST /tasks/:id/attachments → presigned S3 PUT URL; GET /tasks/:id/attachments; GET /attachments/:id/download → presigned S3 GET URL 15-min TTL; DELETE /attachments/:id; 25 MB limit enforced before presigned URL issue; S3 key: attachments/<user_id>/<task_id>/<uuid>.<ext>; status: pending→uploaded→deleted) | 16      | Draft  | ✅ 25 MB/file, 5/task | ✅ 25 MB/file, 20/task |
| E-025 | File Attachments — Frontend (attachment list in TaskDialog; file picker → POST to get presigned URL → PUT directly to S3; progress indicator; download button → GET presigned URL → browser download; delete with ConfirmDialog; file type + size validation before upload)                                                                                                                 | 17      | Draft  | ✅ 25 MB/file, 5/task | ✅ 25 MB/file, 20/task |
| E-026 | AI Assistant — Backend (DB migrations 009–011: ai_sessions, ai_messages, ai_usage_monthly; POST/GET/DELETE /ai/sessions; POST /ai/sessions/:id/messages; Anthropic API integration; Free limit: 10 req/month → AI_LIMIT_REACHED; usage count returned on every response; session title auto-generated from first message)                                                                   | 17      | Draft  | 10 req/month          | Unlimited              |
| E-027 | AI Assistant — Frontend (AI page or modal; session list with create + delete; chat message thread; streaming or non-streaming response rendering; quota indicator for Free users: "X of 10 requests used"; upgrade prompt on AI_LIMIT_REACHED)                                                                                                                                              | 18      | Draft  | 10 req/month          | Unlimited              |
| E-028 | Smart Scheduling NLP — Backend + Frontend (POST /nlp/parse: Pro-only; request: { text }; response: { scheduledFor, dueDate, confidence }; PLAN_LIMIT_EXCEEDED for Free; NLP input wired into DueDateField as a natural-language mode toggle; confidence score shown as hint)                                                                                                                | 18      | Draft  | ❌                    | ✅                     |

---

#### Phase 5 — Billing, E2E Polish & Web v1 Completion (Sprints 19–20)

_Goal: Pro upgrade works end-to-end; all plan limits enforced and visible; full Playwright E2E suite passing; Storybook stories complete for all features. "Done" = web v1 is shippable._

| Epic  | Title                                                                                                                                                                                                                                                                                                                                                                                                | Sprints | Status | Free           | Pro           |
| ----- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- | ------ | -------------- | ------------- |
| E-029 | Billing & Subscriptions — Backend (DB migrations 007–008: user_plans, webhook_events; GET /billing/plan; GET /billing/checkout-url → LS API; GET /billing/portal-url → LS API; POST /billing/webhook: HMAC-SHA256 verification + idempotency via webhook_events.lemon_squeezy_event_id UNIQUE; plan propagation via JWT plan claim at token refresh; graceful downgrade: excess resources read-only) | 19      | Draft  | Free (default) | Pro (monthly) |
| E-030 | Billing & Subscriptions — Frontend (Billing/Plan page: current plan + usage stats; "Upgrade to Pro" CTA → GET /billing/checkout-url → redirect; plan limit error handling: show upgrade prompt instead of generic error; "Manage billing" → GET /billing/portal-url → redirect; plan badge in sidebar Footer)                                                                                        | 19–20   | Draft  | Free (default) | Pro (monthly) |
| E-031 | E2E Test Suite — Playwright (critical user journeys: auth.spec.ts: register→login→logout; task.spec.ts: create→complete→delete; inbox.spec.ts: capture→process→verify in project; area-project.spec.ts: create area→create project→drag reorder; billing-smoke.spec.ts: plan page renders + upgrade CTA present; CI integration: Playwright runs on staging after deploy)                            | 20      | Draft  | —              | —             |
| E-032 | Storybook — Phase 2 (stories for all feature components: AreaDialog, AreaContextMenu, ProjectDialog, ProjectDeleteDialog, TaskItem, TaskDialog, TaskFilters, BucketList, BucketQuickInput, BucketProcessDialog, AppSidebar sections; visual regression baseline snapshots; Chromatic integration if desired)                                                                                         | 20      | Draft  | —              | —             |

---

#### Phase 6 — Mobile App (Sprints 21–25)

_Goal: iOS and Android apps ship to internal testing (TestFlight / Google internal track) with core features working. "Done" = EAS preview build installed and functional on a real device._

| Epic  | Title                                                                                                                                                                                                                                                                                                                                                                                          | Sprints | Status | Free                 | Pro          |
| ----- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- | ------ | -------------------- | ------------ |
| E-033 | Mobile — Shared Package Extraction (extract @nicoflow/shared pnpm workspace package: all RTK Query API slices, TypeScript types/interfaces, Zod schemas, constants, utility functions; configure Metro resolver aliases; both web app and mobile app import from @nicoflow/shared; TypeScript project references configured)                                                                   | 21      | Draft  | —                    | —            |
| E-034 | Mobile — Scaffolding & Auth (Expo ~52 managed workflow; Expo Router file-based tabs; EAS build profiles: development/preview/production; OTA via expo-updates; tab layout: Today/Inbox/Areas/AI/Settings; auth flow: login/register screens sharing @nicoflow/shared authApi; JWT + refresh token stored in expo-secure-store; push token registration via expo-notifications on first launch) | 21–22   | Draft  | ✅                   | ✅           |
| E-035 | Mobile — Core Screens: Today & Inbox (Today tab: task list for today using GET /time-spread; mark task done inline; pull-to-refresh; Inbox tab: bucket list + BucketQuickInput equivalent; swipe-to-process action; BucketProcessSheet (bottom sheet modal); empty states; loading skeletons)                                                                                                  | 22–23   | Draft  | ✅                   | ✅           |
| E-036 | Mobile — Areas & Projects Tab (area list with expand/collapse; project list per area; project screen with task list; task create/edit bottom sheet with all fields; drag-to-reorder (long press); swipe-to-delete with undo; plan limit errors surfaced inline)                                                                                                                                | 23–24   | Draft  | 3 areas / 5 projects | Unlimited    |
| E-037 | Mobile — AI, Settings & Release (AI tab: session list + chat; quota indicator for Free; Settings tab: profile edit, theme toggle, push notification preferences, change password, log out; deep link on notification tap via Expo Router; OTA update check on app foreground; EAS preview build submitted to TestFlight + Google internal track)                                               | 24–25   | Draft  | 10 req/month AI      | Unlimited AI |

---

### 2.2 Sprint Roadmap Calendar

| Sprint | Weeks | Epics                         | Phase Milestone                                          |
| ------ | ----- | ----------------------------- | -------------------------------------------------------- |
| 01     | 1–2   | E-001, E-002                  | Both repos cloneable and running locally                 |
| 02     | 3–4   | E-003, E-004                  | CI green; migrations running                             |
| 03     | 5–6   | E-005, E-006 (start)          | Design tokens set; first components in Storybook         |
| 04     | 7–8   | E-006 (finish), E-007, E-008  | Phase 0 complete — full dev toolchain operational        |
| 05     | 9–10  | E-009                         | Auth backend fully tested                                |
| 06     | 11–12 | E-010                         | End-to-end login flow working in browser                 |
| 07     | 13–14 | E-011, E-012 (start)          | Areas + projects CRUD working; sidebar renders real data |
| 08     | 15–16 | E-012 (finish), E-013         | Phase 1 complete — task CRUD end-to-end                  |
| 09     | 17–18 | E-014 (start)                 | ProjectView renders tasks; TaskDialog functional         |
| 10     | 19–20 | E-014 (finish), E-015         | Task UI polished; bucket backend ready                   |
| 11     | 21–22 | E-016, E-017                  | Inbox and Time Spread working end-to-end                 |
| 12     | 23–24 | E-018                         | Phase 2 complete — test coverage baseline established    |
| 13     | 25–26 | E-019, E-020 (start)          | Search returning results; notifications listing          |
| 14     | 27–28 | E-020 (finish), E-021         | Phase 3 complete — settings + notifications done         |
| 15     | 29–30 | E-022, E-023 (start)          | WS hub deployed; events flowing to backend               |
| 16     | 31–32 | E-023 (finish), E-024         | WS frontend reconnecting; S3 presigned URLs working      |
| 17     | 33–34 | E-025, E-026                  | File attachment UI done; AI backend integrated           |
| 18     | 35–36 | E-027, E-028                  | Phase 4 complete — AI chat + NLP working                 |
| 19     | 37–38 | E-029, E-030 (start)          | LS checkout flow working; webhook handler live           |
| 20     | 39–40 | E-030 (finish), E-031, E-032  | **Web v1 complete** — E2E suite green; Storybook done    |
| 21     | 41–42 | E-033, E-034 (start)          | @nicoflow/shared extracted; Expo app boots               |
| 22     | 43–44 | E-034 (finish), E-035 (start) | Auth working on device; Today tab renders                |
| 23     | 45–46 | E-035 (finish), E-036 (start) | Today + Inbox complete; Areas tab in progress            |
| 24     | 47–48 | E-036 (finish), E-037 (start) | Areas/Projects + tasks working on mobile                 |
| 25     | 49–50 | E-037 (finish)                | **Mobile Phase 1 complete** — TestFlight build submitted |

---

### 2.3 Phase Summaries

**Phase 0 (Sprints 01–04): Foundation & Engineering Setup**
All tooling, conventions, and infrastructure are in place before any product feature is written. The investment here prevents technical debt accumulation. A developer joining at Sprint 05 should be able to contribute immediately.

**Phase 1 (Sprints 05–08): Auth & Core Data Model**
The backbone of the application. Auth, areas, projects, and tasks are the data model everything else builds on. These must be solid and well-tested before moving on.

**Phase 2 (Sprints 09–12): Task UI, Inbox & Time Spread**
The core user workflow: capture a thought in the inbox, process it into a task in a project, and see it appear in the Today view. This is the GTD loop.

**Phase 3 (Sprints 13–14): Search, Notifications & Settings**
Supporting features that make the product feel complete. Search is especially high-leverage for power users.

**Phase 4 (Sprints 15–18): Real-Time, File Attachments & AI**
The differentiating features. Real-time sync makes the app feel alive. AI and NLP scheduling are Pro value drivers.

**Phase 5 (Sprints 19–20): Billing, E2E & Web v1 Completion**
Revenue plumbing and quality gate. No shipping until Playwright suite is green and billing works end-to-end. Web v1 milestone.

**Phase 6 (Sprints 21–25): Mobile App**
Mobile is a separate product phase. The shared code extraction (E-033) is the riskiest part — get that right first before building screens.

---

### 2.4 Definition of Done (cross-cutting)

Every epic is "Done" only when **all** of the following are true:

- **Code:** PR merged to `main`, CI green (lint + type-check + tests + build pass)
- **API (backend epics):** All endpoints return the shape defined in §3 of this document; error codes match §4; no 500s on any happy path
- **Frontend:** Feature is usable in browser without console errors; no TypeScript errors (`pnpm type-check` clean)
- **Tests:** Unit tests for all new service/utility logic; MSW integration test covering the happy path; Go table-driven tests for repository + service layers
- **Storybook:** A story exists for every new shared component introduced by the epic (Phases 0–5 only)
- **Documentation:** Epic marked `Done` in this table; relevant Confluence pages updated

---

## 3. API Endpoint Reference

**Base URL:** `https://api.nicoflow.app/v1/`
**Local dev:** `http://localhost:8080/v1/`

All authenticated endpoints require `Authorization: Bearer <jwt>` header.
All request and response bodies are `application/json`.

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

| Field      | Type   | Required | Constraints                                   |
| ---------- | ------ | -------- | --------------------------------------------- |
| `email`    | string | Yes      | Valid email format                            |
| `password` | string | Yes      | 8–20 chars, 1 uppercase, 1 lowercase, 1 digit |
| `username` | string | Yes      | 3–20 chars, alphanumeric only                 |
| `platform` | string | No       | `"web"` \| `"mobile"` — defaults to `"web"`   |

**Response — 201 Created**

```json
{
  "token": "<jwt>",
  "refreshToken": "<refresh-jwt>",
  "user": {
    "id": 1,
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

| Code            | HTTP | Meaning                          |
| --------------- | ---- | -------------------------------- |
| `CONFLICT`      | 409  | Email or username already exists |
| `INVALID_INPUT` | 422  | Validation failed                |
| `RATE_LIMITED`  | 429  | Too many registration attempts   |

---

#### POST /v1/auth/login

Authenticate and receive tokens.

- **Auth required:** No

**Request body**

```json
{
  "email": "user@example.com",
  "password": "Secret1234",
  "remember": true,
  "platform": "web"
}
```

| Field      | Type    | Required |
| ---------- | ------- | -------- |
| `email`    | string  | Yes      |
| `password` | string  | Yes      |
| `remember` | boolean | Yes      |
| `platform` | string  | No       |

**Response — 200 OK**

```json
{
  "token": "<jwt>",
  "refreshToken": "<refresh-jwt>",
  "user": { ...IUser }
}
```

**Errors:** `UNAUTHORIZED` (401), `INVALID_INPUT` (422), `RATE_LIMITED` (429)

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

**Errors:** `INVALID_TOKEN` (401), `INVALID_TOKEN` (401)

---

#### POST /v1/auth/logout

Invalidate the current session.

- **Auth required:** Yes

**Response — 204 No Content**

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
  "id": 1,
  "email": "user@example.com",
  "username": "johndoe",
  "firstName": "John",
  "lastName": "Doe",
  "theme": "light",
  "imageUrl": "https://...",
  "status": "regular"
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

#### POST /v1/users/push-token

Register a device push notification token.

- **Auth required:** Yes

**Request body**

```json
{ "token": "<expo-push-token>", "platform": "ios" }
```

**Response — 201 Created**

---

### 3.2 Areas

#### GET /v1/areas

List all areas for the authenticated user.

- **Auth required:** Yes

**Response — 200 OK**

```json
[
  {
    "id": 1,
    "name": "Work",
    "icon": "briefcase",
    "sortOrder": 0,
    "userId": 1,
    "createdAt": "...",
    "updatedAt": "..."
  }
]
```

---

#### GET /v1/areas/with-projects

List all areas with their nested projects.

- **Auth required:** Yes

**Response — 200 OK** — `IArea[]` each with a populated `projects: IProject[]` array.

---

#### GET /v1/areas/:id

Retrieve a single area.

- **Auth required:** Yes

**Response — 200 OK** — `IArea`

**Errors:** `RESOURCE_NOT_FOUND` (404), `PERMISSION_DENIED` (403)

---

#### POST /v1/areas

Create a new area.

- **Auth required:** Yes
- **Plan limit:** Free plan allows a maximum of **3 areas**

**Request body**

```json
{ "name": "Personal", "icon": "home" }
```

| Field  | Type   | Required | Constraints                            |
| ------ | ------ | -------- | -------------------------------------- |
| `name` | string | Yes      | 1–30 characters                        |
| `icon` | string | No       | Valid `IconId` — default `"briefcase"` |

**Response — 201 Created** — `IArea`

**Errors:** `PLAN_LIMIT_EXCEEDED` (403), `INVALID_INPUT` (422)

---

#### PATCH /v1/areas/:id

Update an area.

- **Auth required:** Yes

**Request body** (all fields optional)

```json
{ "name": "Personal Life", "icon": "home", "sortOrder": 2 }
```

**Response — 200 OK** — Updated `IArea`

**Errors:** `RESOURCE_NOT_FOUND` (404), `PERMISSION_DENIED` (403), `INVALID_INPUT` (422)

---

#### DELETE /v1/areas/:id

Delete an area. All contained projects are also deleted.

- **Auth required:** Yes

**Response — 200 OK**

```json
{ "message": "Area deleted successfully" }
```

**Errors:** `RESOURCE_NOT_FOUND` (404), `PERMISSION_DENIED` (403)

---

### 3.3 Projects

#### GET /v1/areas/:areaId/projects

List all projects within an area.

- **Auth required:** Yes

**Response — 200 OK** — `IProject[]`

---

#### GET /v1/projects/:id

Retrieve a single project.

- **Auth required:** Yes

**Response — 200 OK** — `IProject`

**Errors:** `RESOURCE_NOT_FOUND` (404), `PERMISSION_DENIED` (403)

---

#### POST /v1/areas/:areaId/projects

Create a new project inside an area.

- **Auth required:** Yes
- **Plan limit:** Free plan allows a maximum of **5 projects total** (across all areas)

**Request body**

```json
{
  "name": "Q3 Launch",
  "icon": "rocket",
  "status": "active",
  "dueDate": "2026-09-30",
  "isFavorite": false
}
```

| Field        | Type    | Required | Constraints                                                      |
| ------------ | ------- | -------- | ---------------------------------------------------------------- |
| `name`       | string  | Yes      | 1–50 characters                                                  |
| `icon`       | string  | No       | Valid `IconId`                                                   |
| `status`     | string  | No       | `"active"` \| `"completed"` \| `"archived"` — default `"active"` |
| `dueDate`    | string  | No       | ISO 8601 date, must not be in the past                           |
| `isFavorite` | boolean | No       | Default `false`                                                  |

**Response — 201 Created** — `IProject`

**Errors:** `PLAN_LIMIT_EXCEEDED` (403), `RESOURCE_NOT_FOUND` (404 — area not found), `INVALID_INPUT` (422)

---

#### PATCH /v1/projects/:id

Update a project. Use `areaId` to move the project to a different area.

- **Auth required:** Yes

**Request body** (all fields optional)

```json
{
  "name": "Q3 Launch — Updated",
  "icon": "star",
  "status": "completed",
  "dueDate": "2026-09-30",
  "isFavorite": true,
  "areaId": 2,
  "sortOrder": 1
}
```

**Response — 200 OK** — Updated `IProject`

**Errors:** `RESOURCE_NOT_FOUND` (404), `PERMISSION_DENIED` (403), `INVALID_INPUT` (422)

---

#### DELETE /v1/projects/:id

Delete a project and all its tasks.

- **Auth required:** Yes

**Response — 204 No Content**

**Errors:** `RESOURCE_NOT_FOUND` (404), `PERMISSION_DENIED` (403)

---

### 3.4 Tasks

#### GET /v1/projects/:projectId/tasks

List all tasks within a project.

- **Auth required:** Yes

**Query parameters**

| Param       | Type   | Description                                      |
| ----------- | ------ | ------------------------------------------------ |
| `status`    | string | Filter by `todo` \| `in-progress` \| `done`      |
| `priority`  | string | Filter by `low` \| `medium` \| `high`            |
| `sortField` | string | `dueDate` \| `priority` \| `name` \| `createdAt` |
| `sortOrder` | string | `asc` \| `desc`                                  |

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
  "name": "Write spec",
  "description": "Write the full API specification",
  "priority": "high",
  "dueDate": "2026-05-10",
  "estimatedMinutes": 90,
  "url": "https://notion.so/...",
  "scheduledFor": "2026-05-02"
}
```

| Field              | Type   | Required | Constraints                                            |
| ------------------ | ------ | -------- | ------------------------------------------------------ |
| `name`             | string | Yes      | 1–100 characters                                       |
| `description`      | string | No       | max 500 characters                                     |
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
  "name": "Write spec v2",
  "description": "...",
  "status": "in-progress",
  "priority": "high",
  "dueDate": "2026-05-15",
  "estimatedMinutes": 120,
  "url": "https://...",
  "scheduledFor": "2026-05-03",
  "sortOrder": 0,
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
    "id": 1,
    "taskId": 5,
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
    "id": 1,
    "userId": 1,
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
  "projectId": 3,
  "taskDetails": {
    "name": "Buy groceries",
    "description": "Weekly shop",
    "priority": "medium",
    "dueDate": "2026-05-05",
    "estimatedMinutes": 60
  }
}
```

| Field              | Type   | Required | Values                                    |
| ------------------ | ------ | -------- | ----------------------------------------- |
| `processingResult` | string | Yes      | `"task"` \| `"note"` \| `"trash"`         |
| `projectId`        | number | No       | Required when `processingResult = "task"` |
| `taskDetails`      | object | No       | Required when `processingResult = "task"` |
| `noteDetails`      | object | No       | Required when `processingResult = "note"` |

**Response — 200 OK** — Updated `IBucket` (with `processedAt` and `processingResult` populated)

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

**Errors:** `PLAN_LIMIT_EXCEEDED` (403 — monthly quota exceeded), `RATE_LIMITED` (429)

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
  "tasks":    [ { ...ITask,    "_highlight": { "name": "Write <mark>spec</mark>" } } ],
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
    "id": 1,
    "userId": 1,
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
- **Limit:** 25 MB per file (both plans)

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
    "taskId": 5,
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

Generate a Lemon Squeezy checkout URL for upgrading to Pro.

- **Auth required:** Yes

**Response — 200 OK**

```json
{ "url": "https://nicoflow.lemonsqueezy.com/checkout/..." }
```

---

#### GET /v1/billing/portal-url

Generate a Lemon Squeezy customer portal URL for managing billing.

- **Auth required:** Yes

**Response — 200 OK**

```json
{ "url": "https://app.lemonsqueezy.com/billing/..." }
```

---

#### POST /v1/billing/webhook

Lemon Squeezy webhook receiver. Not called by the client — called by Lemon Squeezy servers.

- **Auth required:** No (HMAC-SHA256 signature in `X-Signature` header)
- **Idempotent:** Yes — duplicate events are silently ignored via `webhook_events` table

**Response — 200 OK**

---

### 3.14 Real-Time Sync (WebSocket)

#### GET /v1/ws

Upgrade to a WebSocket connection for real-time push events.

- **Auth required:** Yes (JWT passed as query param)

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

## 4. Error Code Reference

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
| `WEAK_PASSWORD`         | 400         | Password does not meet strength requirements                               |
| `REQUIRED`              | 400         | A required field is missing from the request                               |
| `DATABASE_ERROR`        | 500         | Unhandled database error                                                   |
| `INTERNAL_SERVER_ERROR` | 500         | Unhandled server error                                                     |
| `SERVICE_UNAVAILABLE`   | 503         | Downstream service (AI provider, S3, etc.) is unreachable                  |

> **Frontend note:** All RTK Query error responses will have `error.data.error.code` set to one of the above strings. Use these constants (not HTTP status codes) for conditional error handling in the UI.

---

## 5. Plan Limits

| Resource                 | Free            | Pro             |
| ------------------------ | --------------- | --------------- |
| Areas                    | 3               | Unlimited       |
| Projects (total)         | 5               | Unlimited       |
| Tasks                    | Unlimited       | Unlimited       |
| Subtasks                 | Unlimited       | Unlimited       |
| AI Requests              | 10 / month      | Unlimited       |
| NLP Smart Scheduling     | ❌              | ✅              |
| File Attachments         | ✅ (25 MB/file) | ✅ (25 MB/file) |
| WebSocket Real-Time Sync | ✅              | ✅              |
| Search                   | ✅              | ✅              |
| Notifications            | ✅              | ✅              |

**Graceful downgrade policy:** If a Pro user downgrades to Free and their counts exceed the Free limits, excess areas and projects become **read-only** (no create, update, or delete until counts are within limits). Existing data is never deleted automatically.

---

## 6. Billing Architecture

### Provider

**Lemon Squeezy** — handles checkout, invoicing, subscription management, and refunds.

### Subscription Plans

| Plan | Billing Cycle | Notes                |
| ---- | ------------- | -------------------- |
| Free | —             | Default on signup    |
| Pro  | Monthly       | Annual billing in v2 |

### Checkout Flow

1. Client calls `GET /v1/billing/checkout-url` → receives Lemon Squeezy-hosted checkout URL.
2. User completes checkout on Lemon Squeezy.
3. Lemon Squeezy fires a webhook to `POST /v1/billing/webhook`.
4. API verifies `X-Signature` using HMAC-SHA256 with the Lemon Squeezy webhook secret.
5. API records the event in `webhook_events` (idempotency: unique on `lemon_squeezy_event_id`).
6. API updates `users.status = 'premium'` and commits.
7. The updated plan propagates to the client on the next JWT refresh (maximum 15-minute lag).

### Webhook Event Idempotency

```sql
CREATE TABLE webhook_events (
  id                      SERIAL PRIMARY KEY,
  lemon_squeezy_event_id  TEXT UNIQUE NOT NULL,
  event_name              TEXT NOT NULL,
  payload                 JSONB,
  processed_at            TIMESTAMPTZ DEFAULT now()
);
```

Duplicate webhook deliveries with the same `lemon_squeezy_event_id` are detected and return `200 OK` immediately without reprocessing.

### Plan Propagation

The user's `status` field (`"regular"` | `"premium"`) is embedded in the JWT claims at issue time. Plan changes take effect on the next token refresh (max 15-minute lag). The `/v1/billing/plan` endpoint always reflects the current database state in real time.

### Customer Portal

`GET /v1/billing/portal-url` returns a Lemon Squeezy customer portal URL where users can update payment methods, view invoices, and cancel their subscription.

---

## 7. Mobile App Architecture (Planned)

### Framework & Tooling

| Layer              | Package / Tool                              |
| ------------------ | ------------------------------------------- |
| Framework          | Expo ~52, React Native                      |
| Navigation         | Expo Router (file-based)                    |
| State / API        | `@reduxjs/toolkit` RTK Query (shared slice) |
| Push Notifications | `expo-notifications`                        |
| OTA Updates        | `expo-updates`                              |
| Build & Deploy     | EAS (Expo Application Services)             |
| Shared types       | `@nicoflow/shared` package (monorepo)       |

### Tab Structure

| Tab      | Route              | Description                          |
| -------- | ------------------ | ------------------------------------ |
| Today    | `/(tabs)/today`    | Time Spread View for the current day |
| Inbox    | `/(tabs)/inbox`    | Bucket quick-capture and processing  |
| Areas    | `/(tabs)/areas`    | Areas → Projects → Tasks drill-down  |
| AI       | `/(tabs)/ai`       | AI Assistant sessions                |
| Settings | `/(tabs)/settings` | Profile, theme, notifications, plan  |

### EAS Build Profiles

| Profile       | Purpose                                     |
| ------------- | ------------------------------------------- |
| `development` | Dev client build for local debugging        |
| `preview`     | Internal TestFlight / internal track builds |
| `production`  | App Store / Play Store releases             |

### Shared Code Strategy

RTK Query API slices (`areaApi`, `projectApi`, `taskApi`, `bucketApi`, `authApi`) are extracted into a `packages/@nicoflow/shared` package. Both the web app and the React Native app import from this package. Type definitions in `@nicoflow/shared/types` replace per-app copies of `IArea`, `IProject`, `ITask`, `IBucket`, `IUser`.

### Push Notifications

1. On first launch (post-auth), the app calls `Notifications.getExpoPushTokenAsync()`.
2. Token is registered via `POST /v1/users/push-token` with `platform: "ios"` or `"android"`.
3. The API stores the token and uses it to deliver task-due and general notifications via Expo's push service.
4. Deep-link on notification tap routes directly to the relevant task or project using Expo Router.

### OTA Updates

`expo-updates` is configured to check for updates on each app foreground. Update channels:

| Channel      | EAS Profile |
| ------------ | ----------- |
| `production` | production  |
| `preview`    | preview     |

Critical updates that require a native rebuild (e.g., new native dependency) follow a full EAS build + store submission cycle.

---

## 8. Backend Architecture (Canonical Reference)

> This section is the single source of truth for all backend implementation decisions.
> All Go API work must align with or update this section via PR.

---

### 8.1 Database Schema (PostgreSQL 15)

#### Design Principles

- All primary keys are `TEXT NOT NULL PRIMARY KEY` (application-generated UUIDs or NanoIDs). Go uses `string`.
- All timestamps are `TIMESTAMPTZ` (UTC). Never `TIMESTAMP WITHOUT TIME ZONE`.
- `deleted_at` soft-delete is present only on `users`. All other tables use hard delete with FK cascade/set-null as specified.
- `display_order` / `position` columns use `INT DEFAULT 0`. Gaps are intentional (sparse ordering).
- Every table that is user-scoped has an explicit index on `user_id`.
- A `UNIQUE` constraint implicitly creates a btree index in PostgreSQL — no redundant explicit `CREATE INDEX` on those columns.
- Migrations live in `migrations/` as numbered `.up.sql` / `.down.sql` pairs using `golang-migrate`. Never modify existing migrations — always add a new file.

---

#### 8.1.1 `users`

```sql
CREATE TABLE users (
  id            TEXT         NOT NULL PRIMARY KEY,
  email         VARCHAR(255) NOT NULL UNIQUE,
  password_hash VARCHAR(255) NOT NULL,
  name          VARCHAR(255),
  username      VARCHAR(20)  UNIQUE,
  first_name    VARCHAR(100),
  last_name     VARCHAR(100),
  theme         VARCHAR(20)  NOT NULL DEFAULT 'light',
  image_url     TEXT,
  status        VARCHAR(20)  NOT NULL DEFAULT 'regular',
  plan          VARCHAR(20)  NOT NULL DEFAULT 'free',
  timezone      VARCHAR(63)  NOT NULL DEFAULT 'UTC',
  created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  deleted_at    TIMESTAMPTZ
);

CREATE INDEX idx_users_username ON users(username) WHERE username IS NOT NULL;
```

| Field           | Notes                                                                    |
| --------------- | ------------------------------------------------------------------------ |
| `id`            | Application-generated UUID                                               |
| `email`         | Unique login email — `UNIQUE` constraint provides implicit index          |
| `password_hash` | bcrypt-hashed (cost 12)                                                  |
| `username`      | 3–20 alphanumeric chars, unique across all users (added migration 014)   |
| `first_name`    | Optional given name                                                      |
| `last_name`     | Optional family name                                                     |
| `theme`         | UI theme preference: `light` \| `dark`                                   |
| `image_url`     | Avatar URL (nullable)                                                    |
| `status`        | Account status: `regular` \| `premium`                                   |
| `plan`          | Denormalised plan: `free` \| `pro` — kept in sync by billing webhook     |
| `timezone`      | IANA timezone string (e.g. `America/New_York`) for scheduling            |
| `deleted_at`    | Soft-delete — `NULL` means active                                        |

> **`plan` denormalisation:** `users.plan` mirrors `user_plans.plan`. It is updated by the billing webhook handler alongside `user_plans`. The JWT `plan` claim is read from this column at token-issue time, avoiding a join to `user_plans` on every login/refresh.

---

#### 8.1.2 `refresh_tokens`

```sql
CREATE TABLE refresh_tokens (
  id                TEXT         NOT NULL PRIMARY KEY,
  user_id           TEXT         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash        VARCHAR(255) NOT NULL UNIQUE,
  token_fingerprint VARCHAR(64)  UNIQUE,
  expires_at        TIMESTAMPTZ  NOT NULL,
  created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_refresh_tokens_user_id    ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_expires_at ON refresh_tokens(expires_at);
```

| Field               | Notes                                                                        |
| ------------------- | ---------------------------------------------------------------------------- |
| `token_hash`        | bcrypt hash (cost 12) of the raw 32-byte hex token — used for verification   |
| `token_fingerprint` | SHA-256 hex of the raw token — used for O(1) DB lookup (added migration 014) |
| `expires_at`        | 7-day TTL — explicit index for expired-token cleanup                         |

> **Dual-hash pattern:** The raw token is never stored. Lookup uses `token_fingerprint` (SHA-256, fast). Verification uses `token_hash` (bcrypt, tamper-proof). This allows O(1) row lookup without scanning every hash.
>
> **Rotation:** On `POST /v1/auth/refresh-token`, the old row is atomically deleted (rows affected checked) and a new one inserted in the same request. If delete returns 0 rows the token was already consumed — reuse detected — all tokens for the user are revoked and 401 is returned. On logout, the fingerprint row is deleted. On `logout-all`, all rows for the user are deleted.

---

#### 8.1.3 `password_reset_tokens`

```sql
CREATE TABLE password_reset_tokens (
  id                TEXT         NOT NULL PRIMARY KEY,
  user_id           TEXT         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash        VARCHAR(255) NOT NULL UNIQUE,
  token_fingerprint VARCHAR(64)  NOT NULL UNIQUE,
  expires_at        TIMESTAMPTZ  NOT NULL,
  used_at           TIMESTAMPTZ,
  created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_prt_user_id    ON password_reset_tokens(user_id);
CREATE INDEX idx_prt_expires_at ON password_reset_tokens(expires_at);
```

| Field               | Notes                                                               |
| ------------------- | ------------------------------------------------------------------- |
| `token_hash`        | bcrypt hash of the raw 32-byte hex token — used for verification    |
| `token_fingerprint` | SHA-256 hex of the raw token — used for O(1) lookup                 |
| `expires_at`        | 1-hour TTL from generation time                                     |
| `used_at`           | Set when the token is consumed — prevents reuse                     |

> **Flow:** `POST /v1/auth/forgot-password` generates a raw 32-byte token, hashes it, and stores the row. The raw token is embedded in the reset link emailed via SMTP (`SMTP_DSN` env var). `POST /v1/auth/reset-password` looks up by fingerprint, bcrypt-verifies, checks `used_at IS NULL` and `expires_at > NOW()`, updates the password, marks the token used, and revokes all refresh tokens for the user.
>
> **No user enumeration:** `forgot-password` always returns `200 OK` regardless of whether the email exists.
>
> **One active token per user:** Storing a new reset token deletes any existing unused tokens for that user.

---

#### 8.1.4 `biometric_credentials` (v2 stub)

```sql
-- Reserved for FIDO2/WebAuthn biometric authentication (v2).
-- Routes POST /v1/auth/biometric/register and POST /v1/auth/biometric/verify
-- are registered but return 501 until this is active.
CREATE TABLE biometric_credentials (
  id            TEXT        NOT NULL PRIMARY KEY,
  user_id       TEXT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  credential_id TEXT        NOT NULL UNIQUE,
  public_key    BYTEA       NOT NULL,
  counter       BIGINT      NOT NULL DEFAULT 0,
  platform      VARCHAR(20) NOT NULL DEFAULT 'web',
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_used_at  TIMESTAMPTZ
);

CREATE INDEX idx_biometric_user_id ON biometric_credentials(user_id);
```

> **Status:** Migration exists (016), routes are registered returning `501 Not Implemented`. Full FIDO2/WebAuthn implementation is deferred to v2.

---

#### 8.1.6 `areas`

```sql
CREATE TABLE areas (
  id            TEXT         NOT NULL PRIMARY KEY,
  user_id       TEXT         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name          VARCHAR(255) NOT NULL,
  color         VARCHAR(7)   NOT NULL DEFAULT '#3B82F6',
  display_order INT          NOT NULL DEFAULT 0,
  created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_areas_user_id ON areas(user_id);
CREATE UNIQUE INDEX idx_areas_user_name ON areas(user_id, name);
```

| Field           | Notes                              |
| --------------- | ---------------------------------- |
| `color`         | Hex colour for UI (e.g. `#3B82F6`) |
| `display_order` | Sort order                         |

> **Plan limit:** Free plan — max 3 areas per user. Service checks `COUNT(*) WHERE user_id = $1` before insert and returns `PLAN_LIMIT_EXCEEDED` if the limit is reached.

---

#### 8.1.7 `projects`

```sql
CREATE TABLE projects (
  id            TEXT         NOT NULL PRIMARY KEY,
  user_id       TEXT         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  area_id       TEXT         REFERENCES areas(id) ON DELETE SET NULL,
  name          VARCHAR(255) NOT NULL,
  status        VARCHAR(63)  NOT NULL DEFAULT 'active',
  display_order INT          NOT NULL DEFAULT 0,
  created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_projects_user_id ON projects(user_id);
CREATE INDEX idx_projects_area_id ON projects(area_id);
CREATE UNIQUE INDEX idx_projects_user_name ON projects(user_id, name);
```

| Field     | Notes                                              |
| --------- | -------------------------------------------------- |
| `area_id` | Nullable — project can exist without an area       |
| `status`  | `active` \| `on_hold` \| `completed` \| `archived` |

> **Plan limit:** Free plan — max 5 projects per user (total, across all areas). Checked in service before insert.

---

#### 8.1.8 `tasks`

```sql
CREATE TABLE tasks (
  id            TEXT         NOT NULL PRIMARY KEY,
  user_id       TEXT         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  project_id    TEXT         REFERENCES projects(id) ON DELETE SET NULL,
  title         VARCHAR(255) NOT NULL,
  notes         TEXT,
  due_date      DATE,
  scheduled_for VARCHAR(31),
  status        VARCHAR(63)  NOT NULL DEFAULT 'inbox',
  created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_tasks_user_id        ON tasks(user_id);
CREATE INDEX idx_tasks_project_id     ON tasks(project_id);
CREATE INDEX idx_tasks_user_status    ON tasks(user_id, status);
CREATE INDEX idx_tasks_user_scheduled ON tasks(user_id, scheduled_for);
CREATE INDEX idx_tasks_user_due       ON tasks(user_id, due_date);
```

| Field           | Notes                                          |
| --------------- | ---------------------------------------------- |
| `project_id`    | `NULL` = inbox task                            |
| `notes`         | Freeform notes (nullable)                      |
| `due_date`      | Optional — `DATE`, no timezone                 |
| `scheduled_for` | `today` \| `tomorrow` \| `this_week` \| `NULL` |
| `status`        | `inbox` \| `active` \| `done` \| `cancelled`   |

---

#### 8.1.9 `subtasks`

```sql
CREATE TABLE subtasks (
  id         TEXT         NOT NULL PRIMARY KEY,
  task_id    TEXT         NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  title      VARCHAR(255) NOT NULL,
  done       BOOLEAN      NOT NULL DEFAULT FALSE,
  position   INT          NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_subtasks_task_id ON subtasks(task_id);
```

---

#### 8.1.10 `user_plans`

```sql
CREATE TABLE user_plans (
  id                            TEXT        NOT NULL PRIMARY KEY,
  user_id                       TEXT        NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
  plan                          VARCHAR(20) NOT NULL DEFAULT 'free',
  status                        VARCHAR(20) NOT NULL DEFAULT 'active',
  lemon_squeezy_subscription_id VARCHAR(255),
  lemon_squeezy_customer_id     VARCHAR(255),
  current_period_start          TIMESTAMPTZ,
  current_period_end            TIMESTAMPTZ,
  created_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_user_plans_user_id ON user_plans(user_id);
```

| Field    | Values                                           |
| -------- | ------------------------------------------------ |
| `plan`   | `free` \| `pro`                                  |
| `status` | `active` \| `cancelled` \| `expired` \| `paused` |

> **Source of truth:** The `plan` field in the JWT payload is derived from this table at token-generation time. After a billing webhook updates the plan, the JWT reflects the new plan on the next token refresh (max 15-minute lag).

---

#### 8.1.11 `webhook_events`

```sql
CREATE TABLE webhook_events (
  id                     TEXT         NOT NULL PRIMARY KEY,
  lemon_squeezy_event_id VARCHAR(255) NOT NULL UNIQUE,
  event_name             VARCHAR(100) NOT NULL,
  payload                JSONB        NOT NULL,
  processed_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  error                  TEXT
);
```

| Field                    | Notes                                                   |
| ------------------------ | ------------------------------------------------------- |
| `lemon_squeezy_event_id` | `UNIQUE` constraint provides implicit idempotency index |
| `error`                  | Nullable — populated only when processing failed        |

> **Usage:** Before processing any Lemon Squeezy webhook, check `lemon_squeezy_event_id` — if already present, return `200 OK` immediately without reprocessing.

---

#### 8.1.12 `ai_sessions`

```sql
CREATE TABLE ai_sessions (
  id         TEXT         NOT NULL PRIMARY KEY,
  user_id    TEXT         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title      VARCHAR(255) NOT NULL DEFAULT 'New Conversation',
  status     VARCHAR(31)  NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ai_sessions_user_id ON ai_sessions(user_id);
```

---

#### 8.1.13 `ai_messages`

```sql
CREATE TABLE ai_messages (
  id         TEXT        NOT NULL PRIMARY KEY,
  session_id TEXT        NOT NULL REFERENCES ai_sessions(id) ON DELETE CASCADE,
  role       VARCHAR(31) NOT NULL,
  content    TEXT        NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ai_messages_session_id ON ai_messages(session_id);
```

`role`: `user` | `assistant`

---

#### 8.1.14 `ai_usage_monthly`

```sql
CREATE TABLE ai_usage_monthly (
  id            TEXT NOT NULL PRIMARY KEY,
  user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  month         DATE NOT NULL,
  request_count INT  NOT NULL DEFAULT 0,
  UNIQUE (user_id, month)
);

CREATE INDEX idx_ai_usage_user_month ON ai_usage_monthly(user_id, month);
```

> **Upsert pattern:** `INSERT ... ON CONFLICT (user_id, month) DO UPDATE SET request_count = ai_usage_monthly.request_count + 1`. Reject with `AI_LIMIT_REACHED` (403) when `request_count >= 10` for free-tier users.

---

#### Relationships Overview

```
users (1) ──── (many) areas                    [cascade delete]
users (1) ──── (many) projects                 [area_id nullable → SET NULL on area delete]
users (1) ──── (many) tasks                    [project_id nullable → SET NULL on project delete; NULL = inbox]
tasks (1) ──── (many) subtasks                 [cascade delete]
users (1) ──── (1)    user_plans               [UNIQUE user_id; cascade delete]
users (1) ──── (many) refresh_tokens           [cascade delete]
users (1) ──── (many) password_reset_tokens    [cascade delete]
users (1) ──── (many) biometric_credentials    [cascade delete — v2 stub]
users (1) ──── (many) ai_sessions              [cascade delete]
ai_sessions (1) ── (many) ai_messages          [cascade delete]
users (1) ──── (many) ai_usage_monthly         [cascade delete]
webhook_events                                  [standalone — no FK to users]
```

#### Cascade Rules

| Delete       | Effect                                                                                                                               |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------------ |
| `user`       | Cascades to areas, projects, tasks, subtasks, user_plans, refresh_tokens, password_reset_tokens, ai_sessions, ai_usage_monthly       |
| `area`       | Sets `area_id = NULL` on child projects                                                                                              |
| `project`    | Sets `project_id = NULL` on child tasks (they become inbox tasks)                                                                    |
| `task`       | Cascades to subtasks                                                                                                                 |
| `ai_session` | Cascades to ai_messages                                                                                                              |

#### Migration File Convention

```
migrations/
  001_create_users.up.sql                        001_create_users.down.sql
  002_create_refresh_tokens.up.sql               002_create_refresh_tokens.down.sql
  003_create_areas.up.sql                        003_create_areas.down.sql
  004_create_projects.up.sql                     004_create_projects.down.sql
  005_create_tasks.up.sql                        005_create_tasks.down.sql
  006_create_subtasks.up.sql                     006_create_subtasks.down.sql
  007_create_user_plans.up.sql                   007_create_user_plans.down.sql
  008_create_webhook_events.up.sql               008_create_webhook_events.down.sql
  009_create_ai_sessions.up.sql                  009_create_ai_sessions.down.sql
  010_create_ai_messages.up.sql                  010_create_ai_messages.down.sql
  011_create_ai_usage_monthly.up.sql             011_create_ai_usage_monthly.down.sql
  012_users_soft_delete.up.sql                   012_users_soft_delete.down.sql
  013_folder_icons_project.up.sql                013_folder_icons_project.down.sql
  014_alter_users_add_profile_fields.up.sql      014_alter_users_add_profile_fields.down.sql
  015_create_password_reset_tokens.up.sql        015_create_password_reset_tokens.down.sql
  016_create_biometric_credentials.up.sql        016_create_biometric_credentials.down.sql
```

Rules: never modify deployed migration files — always add a new one. Roll back with `make rollback`.

---

### 8.2 Go Backend Architecture

#### 8.2.1 Package Structure

```
nicoflow-api/
├── cmd/
│   └── api/
│       └── main.go              — entry point: load config, init DB, wire deps, start server
├── internal/
│   ├── config/
│   │   └── config.go            — env-based config struct (envconfig or viper)
│   ├── db/
│   │   ├── db.go                — pgxpool setup, connection lifecycle
│   │   └── migrations/          — .sql files, applied via golang-migrate at startup
│   ├── middleware/
│   │   ├── auth.go              — JWT extraction + validation → inject Claims into ctx
│   │   ├── ratelimit.go         — in-memory token bucket (per-user + per-IP)
│   │   ├── cors.go              — CORS headers for Vercel origins
│   │   ├── logging.go           — structured request/response logging (zerolog)
│   │   ├── recover.go           — panic recovery → 500 JSON response
│   │   └── requestid.go         — inject/propagate X-Request-ID header
│   ├── domain/
│   │   ├── user/
│   │   │   ├── handler.go
│   │   │   ├── service.go
│   │   │   ├── repository.go
│   │   │   └── types.go
│   │   ├── auth/
│   │   │   ├── handler.go
│   │   │   ├── service.go
│   │   │   ├── repository.go
│   │   │   └── types.go
│   │   ├── area/
│   │   │   ├── handler.go
│   │   │   ├── service.go
│   │   │   ├── repository.go
│   │   │   └── types.go
│   │   ├── project/
│   │   │   ├── handler.go
│   │   │   ├── service.go
│   │   │   ├── repository.go
│   │   │   └── types.go
│   │   ├── task/
│   │   │   ├── handler.go
│   │   │   ├── service.go
│   │   │   ├── repository.go
│   │   │   └── types.go
│   │   ├── bucket/
│   │   │   ├── handler.go
│   │   │   ├── service.go
│   │   │   ├── repository.go
│   │   │   └── types.go
│   │   ├── ai/
│   │   │   ├── handler.go
│   │   │   ├── service.go
│   │   │   ├── repository.go
│   │   │   └── types.go
│   │   └── billing/
│   │       ├── handler.go       — webhook ingestion
│   │       ├── service.go       — plan mutation logic
│   │       ├── repository.go
│   │       └── types.go
│   ├── ws/
│   │   ├── hub.go               — in-process connection registry + broadcast
│   │   ├── client.go            — per-connection read/write pump goroutines
│   │   └── events.go            — typed event structs
│   ├── storage/
│   │   └── s3.go                — AWS S3 client, presigned URL generation
│   └── apperror/
│       └── errors.go            — AppError type, error code constants
├── pkg/
│   ├── jwtutil/
│   │   └── jwt.go               — issue/parse/validate JWT HS256
│   ├── hashutil/
│   │   └── hash.go              — bcrypt helpers (Hash, Compare)
│   └── respond/
│       └── respond.go           — JSON response envelope writer
└── go.mod
```

---

#### 8.2.2 Layer Pattern: Handler → Service → Repository

Dependencies flow inward; outer layers depend on inner-layer **interfaces**, never concrete types.

```
HTTP Request
     │
     ▼
┌──────────────────────────────────────────┐
│  Handler (net/http + chi router)         │
│  • Parse & validate request body/params  │
│  • Extract Claims from context           │
│  • Call Service method                   │
│  • Serialise response via respond.JSON() │
└────────────────────┬─────────────────────┘
                     │ calls interface
                     ▼
┌──────────────────────────────────────────┐
│  Service                                  │
│  • Business logic & validation           │
│  • Plan limit enforcement                │
│  • Emit WebSocket events via Hub         │
│  • Coordinate cross-domain operations    │
└────────────────────┬─────────────────────┘
                     │ calls interface
                     ▼
┌──────────────────────────────────────────┐
│  Repository                              │
│  • All SQL via pgx (parameterised)       │
│  • Returns domain structs or AppError    │
│  • Zero business logic                   │
└──────────────────────────────────────────┘
```

---

#### 8.2.3 Interface Definitions

```go
// internal/domain/area/repository.go
type Repository interface {
    GetAll(ctx context.Context, userID int64) ([]Area, error)
    GetAllWithProjects(ctx context.Context, userID int64) ([]Area, error)
    GetByID(ctx context.Context, userID, areaID int64) (Area, error)
    Create(ctx context.Context, userID int64, req CreateAreaRequest) (Area, error)
    Update(ctx context.Context, userID, areaID int64, req UpdateAreaRequest) (Area, error)
    Delete(ctx context.Context, userID, areaID int64) error
    Count(ctx context.Context, userID int64) (int, error)
}

// internal/domain/area/service.go
type Service interface {
    GetAll(ctx context.Context, userID int64) ([]Area, error)
    GetAllWithProjects(ctx context.Context, userID int64) ([]Area, error)
    GetByID(ctx context.Context, userID, areaID int64) (Area, error)
    Create(ctx context.Context, userID int64, req CreateAreaRequest) (Area, error)
    Update(ctx context.Context, userID, areaID int64, req UpdateAreaRequest) (Area, error)
    Delete(ctx context.Context, userID, areaID int64) error
}

// internal/domain/project/repository.go
type Repository interface {
    GetAll(ctx context.Context, userID int64) ([]Project, error)
    GetByID(ctx context.Context, userID, projectID int64) (Project, error)
    Create(ctx context.Context, userID int64, req CreateProjectRequest) (Project, error)
    Update(ctx context.Context, userID, projectID int64, req UpdateProjectRequest) (Project, error)
    Delete(ctx context.Context, userID, projectID int64) error
    Count(ctx context.Context, userID int64) (int, error)
    BulkUpdateSortOrder(ctx context.Context, userID int64, items []SortOrderItem) error
}

// internal/domain/project/service.go
type Service interface {
    GetAll(ctx context.Context, userID int64) ([]Project, error)
    GetByID(ctx context.Context, userID, projectID int64) (Project, error)
    Create(ctx context.Context, userID int64, req CreateProjectRequest) (Project, error)
    Update(ctx context.Context, userID, projectID int64, req UpdateProjectRequest) (Project, error)
    Delete(ctx context.Context, userID, projectID int64) error
    BulkUpdateSortOrder(ctx context.Context, userID int64, items []SortOrderItem) error
}

// internal/domain/task/repository.go
type Repository interface {
    GetAll(ctx context.Context, userID int64, filter TaskFilter) ([]Task, error)
    GetByID(ctx context.Context, userID, taskID int64) (Task, error)
    Create(ctx context.Context, userID int64, req CreateTaskRequest) (Task, error)
    Update(ctx context.Context, userID, taskID int64, req UpdateTaskRequest) (Task, error)
    Delete(ctx context.Context, userID, taskID int64) error
    BulkUpdateSortOrder(ctx context.Context, userID int64, items []SortOrderItem) error
}

// internal/domain/task/service.go
type Service interface {
    GetAll(ctx context.Context, userID int64, filter TaskFilter) ([]Task, error)
    GetByID(ctx context.Context, userID, taskID int64) (Task, error)
    Create(ctx context.Context, userID int64, req CreateTaskRequest) (Task, error)
    Update(ctx context.Context, userID, taskID int64, req UpdateTaskRequest) (Task, error)
    Delete(ctx context.Context, userID, taskID int64) error
    BulkUpdateSortOrder(ctx context.Context, userID int64, items []SortOrderItem) error
}

// internal/domain/bucket/repository.go
type Repository interface {
    GetAll(ctx context.Context, userID int64) ([]BucketItem, error)
    GetByID(ctx context.Context, userID, itemID int64) (BucketItem, error)
    Create(ctx context.Context, userID int64, req CreateBucketRequest) (BucketItem, error)
    Update(ctx context.Context, userID, itemID int64, req UpdateBucketRequest) (BucketItem, error)
    Delete(ctx context.Context, userID, itemID int64) error
    Process(ctx context.Context, userID, itemID int64, req ProcessBucketRequest) (BucketItem, error)
}

// internal/domain/bucket/service.go
type Service interface {
    GetAll(ctx context.Context, userID int64) ([]BucketItem, error)
    GetByID(ctx context.Context, userID, itemID int64) (BucketItem, error)
    Create(ctx context.Context, userID int64, req CreateBucketRequest) (BucketItem, error)
    Update(ctx context.Context, userID, itemID int64, req UpdateBucketRequest) (BucketItem, error)
    Delete(ctx context.Context, userID, itemID int64) error
    Process(ctx context.Context, userID, itemID int64, req ProcessBucketRequest) (BucketItem, error)
}

// internal/domain/auth/repository.go
type Repository interface {
    // User CRUD
    CreateUser(ctx context.Context, email, username, passwordHash string) (User, error)
    GetUserByEmail(ctx context.Context, email string) (User, error)
    GetUserByID(ctx context.Context, userID string) (User, error)
    UpdateUser(ctx context.Context, userID string, req UpdateMeRequest) (User, error)
    SoftDeleteUser(ctx context.Context, userID string) error
    UpdatePassword(ctx context.Context, userID, passwordHash string) error

    // Refresh tokens — dual-hash (fingerprint for lookup, bcrypt hash for verify)
    StoreRefreshToken(ctx context.Context, userID, tokenHash, tokenFingerprint string, expiresAt time.Time) error
    GetRefreshTokenByFingerprint(ctx context.Context, fingerprint string) (RefreshToken, error)
    DeleteRefreshToken(ctx context.Context, fingerprint string) (int64, error) // returns rows affected for reuse detection
    DeleteAllRefreshTokens(ctx context.Context, userID string) error

    // Password reset tokens — dual-hash (fingerprint for lookup, bcrypt hash for verify)
    StorePasswordResetToken(ctx context.Context, userID, tokenHash, tokenFingerprint string, expiresAt time.Time) error
    GetPasswordResetTokenByFingerprint(ctx context.Context, fingerprint string) (PasswordResetToken, error)
    MarkPasswordResetTokenUsed(ctx context.Context, fingerprint string) error
}

// internal/domain/auth/service.go
type Service interface {
    Register(ctx context.Context, req RegisterRequest) (AuthResponse, error)
    Login(ctx context.Context, req LoginRequest) (AuthResponse, error)
    Logout(ctx context.Context, userID, rawRefreshToken string) error
    LogoutAll(ctx context.Context, userID string) error
    RefreshToken(ctx context.Context, rawRefreshToken string) (AuthResponse, error)
    ForgotPassword(ctx context.Context, email string) error
    ResetPassword(ctx context.Context, req ResetPasswordRequest) error
    GetProfile(ctx context.Context, userID string) (UserView, error)
    UpdateMe(ctx context.Context, userID string, req UpdateMeRequest) (UserView, error)
    DeleteMe(ctx context.Context, userID string) error
    RegisterPushToken(ctx context.Context, userID string, req RegisterPushTokenRequest) error
}
```

---

#### 8.2.4 Middleware Chain Order

Every request passes through middleware in this exact order:

```
1.  recover          — catch panics → 500 JSON response
2.  logging          — structured log: method, path, status, latency, request_id
3.  request_id       — inject X-Request-ID (generate UUID4 if absent; echo back in response)
4.  cors             — set CORS headers; short-circuit OPTIONS preflight
5.  ratelimit_ip     — 100 req/min per source IP (token bucket, in-memory)
    │
    ├── PUBLIC routes (no JWT required):
    │   POST /v1/auth/login
    │   POST /v1/auth/register
    │   POST /v1/auth/forgot-password
    │   POST /v1/auth/reset-password
    │   POST /v1/auth/refresh-token
    │   POST /v1/billing/webhook        ← HMAC-verified separately in handler
    │   GET  /v1/ws                     ← JWT extracted from ?token= in handler
    │   GET  /health
    │
    └── PROTECTED routes (JWT required):
6.  auth             — validate JWT → inject Claims{UserID, Plan, Email} into ctx
7.  ratelimit_user   — 1000 req/min per user_id (token bucket, in-memory)
    │
    └── All /v1/users/*, /v1/areas/*, /v1/projects/*, /v1/tasks/*, /v1/bucket/*, /v1/ai/*
```

---

#### 8.2.5 JWT Payload Structure

```go
// pkg/jwtutil/jwt.go

// Claims is the JWT payload embedded in every access token.
type Claims struct {
    jwt.RegisteredClaims               // sub = user_id (string), exp, iat, iss = "nicoflow-api"

    Email string `json:"email"`
    Plan  string `json:"plan"`   // "free" | "pro"  — avoids per-request DB lookup
}
```

**Example decoded payload:**

```json
{
  "sub": "usr_abc123",
  "iss": "nicoflow-api",
  "iat": 1746057600,
  "exp": 1746058500,
  "email": "user@example.com",
  "plan": "free"
}
```

- **Algorithm:** HS256
- **Access token TTL:** 15 minutes
- **Signing secret:** `JWT_SECRET` env var (minimum 32 bytes, cryptographically random)
- **Plan claim staleness:** Updated at login and refresh. After a billing webhook upgrades a user, the plan claim in existing tokens remains stale until the next refresh (maximum 15-minute lag — acceptable for v1).

---

#### 8.2.6 Error Handling Pattern

All domain errors use string constants defined in `internal/apperror/errors.go`. Handlers call `respond.Error()` which serialises them into the standard envelope.

```go
// internal/apperror/errors.go — canonical error code constants

const (
    ErrInvalidInput        = "INVALID_INPUT"         // 400
    ErrInvalidToken        = "INVALID_TOKEN"         // 401
    ErrUnauthorized        = "UNAUTHORIZED"          // 401
    ErrForbidden           = "FORBIDDEN"             // 403
    ErrPlanLimitExceeded   = "PLAN_LIMIT_EXCEEDED"   // 403
    ErrResourceNotFound    = "RESOURCE_NOT_FOUND"    // 404
    ErrConflict            = "CONFLICT"              // 409
    ErrTaskNotFound        = "TASK_NOT_FOUND"        // 404
    ErrProjectNotFound     = "PROJECT_NOT_FOUND"     // 404
    ErrAreaNotFound        = "AREA_NOT_FOUND"        // 404
    ErrUserNotFound        = "USER_NOT_FOUND"        // 404
    ErrInvalidProjectId    = "INVALID_PROJECT_ID"    // 400
    ErrDuplicateName       = "DUPLICATE_NAME"        // 409
    ErrInvalidStatus       = "INVALID_STATUS"        // 400
    ErrInvalidDate         = "INVALID_DATE"          // 400
    ErrInvalidPriority     = "INVALID_PRIORITY"      // 400
    ErrDatabaseError       = "DATABASE_ERROR"        // 500
    ErrInternalServerError = "INTERNAL_SERVER_ERROR" // 500
    ErrServiceUnavailable  = "SERVICE_UNAVAILABLE"   // 503
    ErrRateLimited         = "RATE_LIMITED"          // 429
    ErrInvalidEmail        = "INVALID_EMAIL"         // 400
    ErrEmailAlreadyExists  = "EMAIL_ALREADY_EXISTS"  // 409
    ErrWeakPassword        = "WEAK_PASSWORD"         // 400
    ErrIdempotencyConflict = "IDEMPOTENCY_CONFLICT"  // 409
    ErrPermissionDenied    = "PERMISSION_DENIED"     // 403
    ErrSessionNotFound     = "SESSION_NOT_FOUND"     // 404
    ErrMessageNotFound     = "MESSAGE_NOT_FOUND"     // 404
    ErrInvalidAIContext    = "INVALID_AI_CONTEXT"    // 400
    ErrAILimitReached      = "AI_LIMIT_REACHED"      // 403
    ErrRequired            = "REQUIRED"              // 400
)
```

```go
// pkg/respond/respond.go — response envelope

type Envelope struct {
    Data  any        `json:"data"`
    Error *ErrorBody `json:"error"`
}

type ErrorBody struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}

func JSON(w http.ResponseWriter, status int, data any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(Envelope{Data: data, Error: nil})
}

func Error(w http.ResponseWriter, err *apperror.AppError) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(err.StatusCode)
    json.NewEncoder(w).Encode(Envelope{
        Data:  nil,
        Error: &ErrorBody{Code: err.Code, Message: err.Message},
    })
}
```

**Success response:**

```json
{
  "data": {
    "id": 7,
    "name": "Work",
    "icon": "briefcase",
    "sortOrder": 0,
    "...": "..."
  },
  "error": null
}
```

**Error response:**

```json
{
  "data": null,
  "error": { "code": "PLAN_LIMIT_EXCEEDED", "message": "Plan limit reached" }
}
```

---

### 8.3 Infrastructure & Services

#### 8.3.1 Overview

| Component     | Provider      | Details                                                               |
| ------------- | ------------- | --------------------------------------------------------------------- |
| Go API        | Render.com    | Web Service, Go buildpack, port `3001` (env: `PORT`)                  |
| PostgreSQL 15 | Render.com    | Managed PostgreSQL 15, internal hostname only (no public access)      |
| Frontend      | Vercel        | React 19 SPA; `staging` auto-deploy, `main` manual deploy             |
| File storage  | AWS S3        | Bucket `nicoflow-attachments`, presigned URLs only (no public access) |
| Billing       | Lemon Squeezy | Subscription webhooks → `POST /v1/billing/webhook`                    |
| Email         | Mailtrap SMTP | Password reset emails via stdlib `net/smtp`; configured via `SMTP_DSN` env var |

---

#### 8.3.2 Render.com — Go API

- **Health check:** `GET /health` — returns `200 OK` with `{"status":"ok","version":"<git_sha>"}`
- **Required environment variables:**

| Variable                       | Description                                                     |
| ------------------------------ | --------------------------------------------------------------- |
| `DATABASE_URL`                 | PostgreSQL connection string (provided by Render)               |
| `JWT_SECRET`                   | HS256 signing secret, min 32 bytes, cryptographically random    |
| `JWT_EXPIRY`                   | Access token TTL, e.g. `15m` (default: `15m`)                   |
| `REFRESH_TOKEN_EXPIRY`         | Refresh token TTL, e.g. `168h` (default: `168h` = 7 days)      |
| `SMTP_DSN`                     | SMTP connection string for password reset email, e.g. `smtp://user:pass@smtp.mailtrap.io:587` |
| `APP_BASE_URL`                 | Frontend base URL used to build reset-password links, e.g. `https://app.nicoflow.app` |
| `CORS_ORIGINS`                 | Comma-separated allowed CORS origins                            |
| `AWS_REGION`                   | S3 region                                                       |
| `AWS_ACCESS_KEY_ID`            | IAM key for S3                                                  |
| `AWS_SECRET_ACCESS_KEY`        | IAM secret for S3                                               |
| `S3_BUCKET_NAME`               | `nicoflow-attachments`                                          |
| `LS_WEBHOOK_SECRET`            | HMAC-SHA256 secret for Lemon Squeezy webhook verification       |
| `APP_ENV`                      | `staging` \| `production`                                       |
| `PORT`                         | `3001` (set by Render automatically)                            |

---

#### 8.3.3 AWS S3 — File Attachments

- **Bucket:** `nicoflow-attachments`
- **Access:** No public access. All access via presigned URLs only.
- **Key pattern:** `attachments/{user_id}/{task_id}/{uuid}.{ext}`
- **Upload flow:**
  1. Client calls `POST /v1/tasks/:id/attachments/presign` → receives `{ uploadUrl, key, expiresAt }`
  2. Client `PUT` directly to S3 with the presigned URL (TTL: **5 minutes**)
  3. Client calls `POST /v1/tasks/:id/attachments` with `{ key, filename, fileSize, mimeType }` to register the attachment row (status = `'uploaded'`)
- **Download:** `GET /v1/tasks/:id/attachments/:aid/url` → presigned GET URL (TTL: **15 minutes**)
- **Max file size:** 25 MB (enforced by API before issuing presigned URL)

---

#### 8.3.4 Lemon Squeezy — Billing Webhook Events

| Event name                     | Action                               |
| ------------------------------ | ------------------------------------ |
| `subscription_created`         | Set plan = `pro`, status = `active`  |
| `subscription_updated`         | Sync period dates and variant ID     |
| `subscription_cancelled`       | Set status = `cancelled`             |
| `subscription_resumed`         | Set status = `active`                |
| `subscription_payment_success` | Extend `current_period_end`          |
| `subscription_payment_failed`  | Set status = `past_due`              |
| `subscription_expired`         | Set plan = `free`, status = `active` |

---

#### 8.3.5 Plan Limits Summary

| Resource     | Free      | Pro       | Error code when exceeded |
| ------------ | --------- | --------- | ------------------------ |
| Areas        | 3         | Unlimited | `PLAN_LIMIT_EXCEEDED`    |
| Projects     | 5 total   | Unlimited | `PLAN_LIMIT_EXCEEDED`    |
| AI requests  | 10/month  | Unlimited | `PLAN_LIMIT_EXCEEDED`    |
| Tasks        | Unlimited | Unlimited | —                        |
| Bucket items | Unlimited | Unlimited | —                        |
| File uploads | 5/task    | 20/task   | `PLAN_LIMIT_EXCEEDED`    |

Plan is read from the JWT `plan` claim — no DB call on each request.

---

### 8.4 API Base URLs & Versioning

| Environment | Base URL                              |
| ----------- | ------------------------------------- |
| Production  | `https://api.nicoflow.app/v1`         |
| Staging     | `https://api-staging.nicoflow.app/v1` |
| Local       | `http://localhost:8080/v1`            |

All endpoints are prefixed with `/v1/`. The version prefix is part of the router mount, not individual handler paths. In v1, list endpoints return bare arrays (no pagination wrapper) to match existing RTK Query slice expectations.

---

### 8.5 Auth & Security

#### 8.5.1 Password Hashing

- **Algorithm:** bcrypt, cost factor **12**
- Applies to: `users.password_hash`, `refresh_tokens.token_hash`, `password_reset_tokens.token_hash`

---

#### 8.5.2 Token Flow

```
Login / Register
     │
     ▼
Generate opaque refresh token (32 bytes, crypto/rand → hex string)
Hash with bcrypt cost 12
Store hash in refresh_tokens (expires_at = now + 7 days)
Return: { token: <jwt_15min>, refreshToken: <raw_token>, user: {...} }
     │
     └── Client: store JWT in localStorage('authToken'),
                 refresh token in HttpOnly cookie (scoped to /v1/auth/refresh-token)

Every API request:
  Middleware reads Authorization: Bearer <jwt>
  Validates signature + expiry → inject Claims into ctx

On 401 (token expired):
     │
     ▼
POST /v1/auth/refresh-token  (refresh token from HttpOnly cookie)
     │
     ▼
Service: bcrypt compare presented token against stored hash
     │
  ┌──┴──────────────────────────────────────────┐
  │  Atomic DB transaction:                      │
  │  1. DELETE FROM refresh_tokens               │
  │     WHERE token_hash = $hash                 │
  │     (0 rows deleted → token reuse detected → │
  │      DELETE all tokens for user → 401)       │
  │  2. INSERT new refresh_tokens row            │
  └──────────────────────────────────────────────┘
     │
     ▼
Return new { token, refreshToken }
```

---

#### 8.5.3 Refresh Token Cookie

```
Set-Cookie: refresh_token=<raw_token>;
            HttpOnly;
            Secure;
            SameSite=Strict;
            Path=/v1/auth/refresh-token;
            Max-Age=604800
```

Scoped to the refresh endpoint only, preventing CSRF on all other routes. The frontend sends `credentials: 'include'` on the refresh call.

---

#### 8.5.4 Rate Limiting (In-Memory, No Redis in v1)

| Limiter    | Scope   | Limit        | Burst | Window |
| ---------- | ------- | ------------ | ----- | ------ |
| IP-based   | IP addr | 100 req/min  | 20    | 60s    |
| User-based | UserID  | 1000 req/min | 100   | 60s    |

**Auth-specific limits** (separate, stricter buckets):

| Endpoint                        | Limit         |
| ------------------------------- | ------------- |
| `POST /v1/auth/login`           | 10 req/min/IP |
| `POST /v1/auth/register`        | 5 req/min/IP  |
| `POST /v1/auth/forgot-password` | 3 req/min/IP  |

Exceeded → `429 Too Many Requests` with `RATE_LIMITED` code and `Retry-After` header.

---

#### 8.5.5 CORS Policy

Allowed origins (from `ALLOWED_ORIGINS` env var):

| Environment | Origin                         |
| ----------- | ------------------------------ |
| Production  | `https://app.nicoflow.app`     |
| Staging     | `https://staging.nicoflow.app` |
| Local dev   | `http://localhost:5173`        |

Allowed methods: `GET, POST, PATCH, DELETE, OPTIONS`
Allowed headers: `Content-Type, Authorization`
Credentials: `true` (required for HttpOnly refresh cookie)

---

#### 8.5.6 Row-Level Data Isolation

Every repository query that reads or mutates user data includes `AND user_id = $1` (or an equivalent join condition that constrains to the authenticated user). Enforced at the application layer. Integration tests assert that cross-user access returns 403/404.

---

### 8.6 Real-Time (WebSocket)

#### 8.6.1 Architecture

In-process hub — no Redis in v1. All connected clients share a single `Hub` instance in memory. Suitable for a single-instance Render deployment. A future multi-instance deployment will require extracting the hub to Redis Pub/Sub.

```
Client ── GET /v1/ws?token=<jwt> ──► Upgrader
                                          │
                                    JWT validated
                                          │
                                    client.go spawns:
                                      readPump goroutine
                                      writePump goroutine
                                          │
                                    Hub.register(client)
                                          │
                         ┌────────────────────────────────────┐
                         │               Hub                   │
                         │  clients  map[userID][]*Client      │
                         │  broadcast chan BroadcastMsg         │
                         │  register  chan *Client              │
                         │  unregister chan *Client             │
                         └────────────────────────────────────┘
                                          ▲
                         Service emits ws.Event → Hub.BroadcastToUser(userID, event)
```

---

#### 8.6.2 Connection & Upgrade

```
GET /v1/ws?token=<jwt_access_token>
Upgrade: websocket
Connection: Upgrade
```

- Auth: JWT extracted from `?token=` query parameter (not `Authorization` header — browser WebSocket APIs do not support custom headers on upgrade requests).
- On invalid/expired JWT: close with code `1008 Policy Violation`.
- On successful upgrade: client registered to hub keyed by `userID`.

---

#### 8.6.3 Message Format

All server-to-client messages are JSON:

```typescript
interface WSEvent {
  event: string; // e.g. "task.created"
  resource: string; // e.g. "task"
  data: unknown; // full resource payload (not a diff)
}
```

**Example:**

```json
{
  "event": "task.updated",
  "resource": "task",
  "data": {
    "id": 99,
    "name": "Write spec",
    "status": "done",
    "projectId": 12
  }
}
```

Full payloads are sent (not diffs). The frontend RTK Query receives the event and calls `dispatch(api.util.invalidateTags(...))` to refresh the relevant cache entries.

---

#### 8.6.4 Supported Events

| Event name        | Trigger                                   |
| ----------------- | ----------------------------------------- |
| `task.created`    | `POST /v1/tasks` success                  |
| `task.updated`    | `PATCH /v1/tasks/:id` success             |
| `task.deleted`    | `DELETE /v1/tasks/:id` success            |
| `project.created` | `POST /v1/projects` success               |
| `project.updated` | `PATCH /v1/projects/:id` success          |
| `project.deleted` | `DELETE /v1/projects/:id` success         |
| `area.created`    | `POST /v1/areas` success                  |
| `area.updated`    | `PATCH /v1/areas/:id` success             |
| `area.deleted`    | `DELETE /v1/areas/:id` success            |
| `inbox.created`   | `POST /v1/bucket` success                 |
| `inbox.updated`   | `PATCH /v1/bucket/:id` or process success |

Events are broadcast only to the client whose `userID` matches the resource owner. There is no multi-user collaboration in v1.

---

#### 8.6.5 Heartbeat & Timeout Parameters

| Parameter            | Value                       |
| -------------------- | --------------------------- |
| Server ping interval | 30 seconds                  |
| Pong timeout         | 10 seconds                  |
| Write timeout        | 10 seconds                  |
| Read timeout         | 60 seconds                  |
| Max message size     | 512 bytes (client → server) |

The server sends a `ping` frame every 30 seconds. If a `pong` is not received within 10 seconds, the server closes the connection and unregisters the client.

---

#### 8.6.6 Client-Side Reconnection (Frontend)

Implemented in `src/hooks/useWebSocket.ts`:

| Attempt | Delay before retry |
| ------- | ------------------ |
| 1       | 1 second           |
| 2       | 2 seconds          |
| 3       | 4 seconds          |
| 4       | 8 seconds          |
| 5       | 16 seconds         |
| 6+      | 30 seconds (cap)   |

**After 3 consecutive failures with no successful connection:**

- Switch to polling fallback: every 30 seconds call relevant RTK Query `refetch()` methods.
- Display "Live updates paused" indicator in UI.
- Continue background WebSocket reconnection attempts every 30 seconds.
- On successful reconnect: clear polling interval, remove indicator.

**Pre-connection JWT check:** Before initiating the WebSocket upgrade, the client checks the `exp` claim of the stored JWT. If expiry is within 60 seconds, the client refreshes the token first before connecting.

---

## 9. Frontend Architecture

> This section is the single source of truth for all frontend implementation decisions.

The frontend is a React 19 SPA built with Vite 7 and TypeScript 5.8 in strict mode. It communicates exclusively with the Nicoflow API (`http://localhost:8080/v1`) via RTK Query. All state, routing, theming, and drag-and-drop are managed by the libraries listed below.

---

### 9.1 Tech Stack

| Layer       | Package                                                             | Notes                                                              |
| ----------- | ------------------------------------------------------------------- | ------------------------------------------------------------------ |
| Framework   | `react@19`, `react-dom@19`                                          |                                                                    |
| Language    | `typescript@5.8`                                                    | strict + verbatimModuleSyntax                                      |
| Build       | `vite@7` + `@tailwindcss/vite`                                      | SVGs via `vite-plugin-svgr`                                        |
| Styling     | `tailwindcss@4`                                                     | Design tokens in `src/index.css` as CSS custom properties          |
| Components  | `shadcn/ui` (New York, neutral)                                     | Primitives in `src/components/ui/` — CLI-generated, edit carefully |
| Routing     | `react-router-dom@7`                                                | `useRoutes` pattern in `src/router.tsx`                            |
| State       | `@reduxjs/toolkit@2` + RTK Query                                    | All server state via RTK Query; never `useEffect + fetch`          |
| Persistence | `redux-persist@6`                                                   | Whitelist: `['auth']` only; RTK Query cache is never persisted     |
| Auth mutex  | `async-mutex`                                                       | Prevents parallel token-refresh races in `baseQuery.ts`            |
| Forms       | `react-hook-form@7` + `zod@4` + `@hookform/resolvers`               | All schemas in `src/lib/utils/utils/schemas.ts`                    |
| Animations  | `framer-motion@12`                                                  | Used in `AnimatedListItem` and page transitions                    |
| DnD         | `@dnd-kit/core` + `@dnd-kit/sortable`                               | Wrapped by `DragAndDropContext`                                    |
| Icons       | `lucide-react`                                                      | `LazyIcon` for dynamic-by-name loading                             |
| Toasts      | `sonner@2`                                                          | Never `react-toastify`                                             |
| Theme       | `next-themes`                                                       | Storage key: `"nicoflow-theme"`                                    |
| Testing     | `vitest@3` + `@testing-library/react@16` + `msw@2` + `playwright@1` |                                                                    |
| Linting     | `eslint@9` flat config + `typescript-eslint` + `simple-import-sort` |                                                                    |
| Storybook   | `storybook@10` + `@storybook/react-vite`                            | Stories co-located: `src/**/*.stories.tsx`                         |

---

### 9.2 Project Structure

```
src/
├── app/
│   ├── App.tsx                — mounts <AppRoutes />
│   └── Providers.tsx          — Redux store, BrowserRouter, ThemeProvider, Toaster, LoadingOverlay
├── assets/svgs/               — SVG files imported as React components via vite-plugin-svgr
├── components/                — Shared, domain-agnostic components
│   ├── ui/                    — shadcn/ui primitives (CLI-generated)
│   ├── NameField/
│   ├── DescriptionField/
│   ├── DueDateField/          — uses react-day-picker
│   ├── EstimatedTimeField/
│   ├── PriorityField/
│   ├── IconField/             — icon picker using LazyIcon
│   ├── CheckboxField/
│   ├── UrlField/
│   ├── FormDialog/            — standard create/edit modal shell
│   ├── ConfirmDialog/         — yes/no destructive action modal
│   ├── CustomDialog/          — flexible modal
│   ├── DialogFieldGrid/       — 1 or 2-column grid layout for dialog forms
│   ├── AnimatedListItem/      — Framer Motion enter/exit for list rows
│   ├── ListItemCard/          — card-style list row
│   ├── ItemActionsMenu/       — three-dot action menu
│   ├── EmptyState/
│   ├── Timestamp/
│   ├── Divider/
│   ├── LazyIcon/              — dynamic Lucide icon by name string
│   ├── DragAndDropContext/    — dnd-kit DndContext wrapper
│   ├── ModeToggle/
│   ├── ThemeProvider/
│   ├── Toaster/
│   └── LoadingOverlayProvider/
├── features/                  — Feature-first modules; each exports from index.tsx
│   ├── Area/                  — AreaDialog, AreaContextMenu, AreaSortOrderField
│   ├── Bucket/                — BucketList, BucketQuickInput, BucketProcessDialog
│   ├── Project/               — ProjectDialog, ProjectDeleteDialog, ProjectHeader, ProjectAreaField
│   ├── Sidebar/
│   │   ├── AppSidebar/        — root sidebar component
│   │   ├── Areas/             — collapsible area sections + draggable project items
│   │   ├── QuickAccess/       — Today / Tomorrow / Next 7 Days / Bucket nav items
│   │   └── Footer/            — user profile, theme toggle, Pro card
│   ├── SignForm/              — shared auth form layout
│   └── Tasks/                 — TaskDialog, TaskItem, TaskFilters, TaskSearch, TasksSection
├── hooks/
│   ├── useMobile.ts           — breakpoint detection
│   └── useCustomDialog.ts     — open/close state helper
├── layout/
│   ├── AuthLayout.tsx         — wraps public auth routes
│   ├── PrivateLayout.tsx      — sidebar + outlet for protected routes
│   ├── CustomSidebarTrigger.tsx
│   └── QuickAddButton.tsx     — floating action button
├── lib/
│   ├── store/
│   │   ├── store.ts           — configureStore, persistedReducer
│   │   ├── hooks.ts           — useAppDispatch, useAppSelector, useAppUser
│   │   ├── index.ts           — single barrel export for all store symbols
│   │   ├── utils/
│   │   │   └── invalidateTags.ts  — invalidateApiTags() helper
│   │   └── slices/
│   │       ├── baseQuery.ts       — fetchBaseQuery + mutex reauth logic
│   │       ├── auth/              — authApi (RTK Query) + authSlice (user state)
│   │       ├── area/              — areaApi
│   │       ├── project/           — projectApi
│   │       ├── tasks/             — taskApi
│   │       └── bucket/            — bucketApi
│   ├── types/
│   │   ├── interfaces/index.ts    — IUser, IArea, IProject, ITask, IBucket, etc.
│   │   ├── constants.ts           — TaskStatus, TaskPriority, PROJECT_STATUS, etc.
│   │   ├── endpoints.ts           — API path constants
│   │   ├── icons.ts               — IconId union type + ICON_IDS array
│   │   └── index.ts               — barrel export
│   └── utils/
│       └── utils/
│           ├── schemas.ts         — all Zod schemas + inferred FormData types
│           ├── messages.ts        — ToastMessages const object
│           ├── helpers.ts
│           └── get-icons.ts
├── pages/
│   ├── auth/                  — SignIn, SignUp, ForgotPassword, ResetPassword
│   ├── quick-access/          — Bucket, Today, Tomorrow, NextSevenDays
│   ├── project/               — ProjectView
│   ├── Profile.tsx
│   ├── ErrorPage.tsx
│   ├── HelpAndInformation.tsx
│   ├── PrivacyPolicy.tsx
│   └── TermsOfService.tsx
└── router.tsx                 — useRoutes config; PrivateRoutes guard reads useAppUser()

__tests__/
├── setup.ts                   — stubs: matchMedia, localStorage, sessionStorage,
│                                IntersectionObserver, ResizeObserver
└── renderComponent.tsx        — render helper: wraps with Redux store + Router providers
```

---

### 9.3 Routing

#### 9.3.1 Route Map

| Path                        | Layout        | Component          | Notes                               |
| --------------------------- | ------------- | ------------------ | ----------------------------------- |
| `/`                         | PrivateLayout | —                  | Redirects to `/quick-access/Bucket` |
| `/quick-access/Bucket`      | PrivateLayout | Bucket page        | Inbox                               |
| `/quick-access/today`       | PrivateLayout | Today page         |                                     |
| `/quick-access/tomorrow`    | PrivateLayout | Tomorrow page      |                                     |
| `/quick-access/next-7-days` | PrivateLayout | Next 7 Days page   |                                     |
| `/projects/:projectId`      | PrivateLayout | ProjectView        |                                     |
| `/projects/new`             | PrivateLayout | —                  | Placeholder `<div>`                 |
| `/profile`                  | PrivateLayout | Profile            |                                     |
| `/terms-of-service`         | PrivateLayout | TermsOfService     |                                     |
| `/privacy-policy`           | PrivateLayout | PrivacyPolicy      |                                     |
| `/help-information`         | PrivateLayout | HelpAndInformation |                                     |
| `/sign-in`                  | AuthLayout    | SignIn             | Public                              |
| `/sign-up`                  | AuthLayout    | SignUp             | Public                              |
| `/forgot-password`          | AuthLayout    | ForgotPassword     | Public                              |
| `/reset-password`           | AuthLayout    | ResetPassword      | Public                              |
| `*`                         | —             | ErrorPage          | Catch-all                           |

#### 9.3.2 Auth Guard

`PrivateRoutes` in `src/router.tsx` calls `useAppUser()`. If the return value is `null`, the user is redirected to `/sign-in` with `state.from` set to the attempted path for post-login redirect.

---

### 9.4 State Management

#### 9.4.1 Principles

- RTK Query is the only mechanism for fetching and caching server state.
- `useEffect + fetch` is forbidden.
- All RTK Query hooks are imported from the `@/lib/store` barrel, never from slice files directly.

```typescript
// Correct
import {
  useGetAreasWithProjectsQuery,
  useCreateProjectMutation,
} from "@/lib/store";

// Wrong — never import from slice files directly
import { areaApi } from "@/lib/store/slices/area/areaApi";
```

#### 9.4.2 API Slices

| Slice        | `reducerPath`  | Tags               | Key Endpoints                                                                                              |
| ------------ | -------------- | ------------------ | ---------------------------------------------------------------------------------------------------------- |
| `authApi`    | `'authApi'`    | `['Auth', 'User']` | register, login, logout, refresh, getProfile, updateProfile, changePassword, forgotPassword, resetPassword |
| `areaApi`    | `'areaApi'`    | `['Area']`         | getAreas (with projects), createArea, updateArea, deleteArea, reorderAreas                                 |
| `projectApi` | `'projectApi'` | `['Project']`      | getProject, createProject, updateProject, deleteProject, reorderProjects                                   |
| `taskApi`    | `'taskApi'`    | `['Task']`         | getTasks, getTask, createTask, updateTask, deleteTask, reorderTasks, getTimeSpread                         |
| `bucketApi`  | `'bucketApi'`  | `['Bucket']`       | getBucketItems, createBucketItem, updateBucketItem, processBucketItem, deleteBucketItem                    |

#### 9.4.3 Auth Slice

`authSlice` holds `user: IUser | null`. It is the only reducer key persisted via `redux-persist` (key: `'auth'`). RTK Query cache state is never persisted.

#### 9.4.4 Token Flow

1. `localStorage.getItem('authToken')` — attached as `Authorization: Bearer <token>` on every request in `baseQuery.ts`.
2. On `401`: `async-mutex` ensures only one refresh fires — calls `POST /v1/auth/refresh-token`.
3. Refresh failure → `localStorage.removeItem('authToken')` + `window.location.href = '/sign-in'`.
4. Refresh success → original request is retried automatically.

#### 9.4.5 Tag Invalidation

Use the typed helper. Never dispatch cache invalidation manually.

```typescript
import { invalidateApiTags } from "@/lib/store";
import { areaApi } from "@/lib/store";

invalidateApiTags(dispatch, areaApi, ["Area"] as const);
```

#### 9.4.6 Store Hooks

```typescript
const dispatch = useAppDispatch(); // typed AppDispatch
const value = useAppSelector((s) => s.auth); // typed RootState
const user = useAppUser(); // shorthand for auth.user
```

---

### 9.5 TypeScript Types

All types are defined in `src/lib/types/` and re-exported from `src/lib/types/index.ts`.

#### 9.5.1 Core Interfaces

```typescript
interface IUser {
  id: number;
  email: string;
  firstName: string;
  lastName: string;
  username: string;
  theme: "light" | "dark";
  imageUrl?: string;
  status: "premium" | "regular";
}

interface IArea {
  id: number;
  name: string;
  icon?: IconId;
  sortOrder?: number;
  userId: number;
  createdAt: string;
  updatedAt: string;
  projects?: IProject[];
}

interface IProject {
  id: number;
  name: string;
  area: IArea;
  areaId: number;
  status: "active" | "archived" | "completed";
  icon?: IconId;
  sortOrder?: number;
  isFavorite?: boolean;
  userId: number;
  createdAt: string;
  updatedAt: string;
}

interface ITask {
  id: number;
  name: string;
  projectId: number;
  description: string;
  status: TaskStatus;
  priority: TaskPriority;
  dueDate?: string;
  estimatedMinutes?: number;
  url?: string;
  recurrence?: ITaskRecurrence;
  notifications?: ITaskNotification[];
  sortOrder?: number;
  completedAt?: string;
  createdAt: string;
  updatedAt: string;
}

interface IBucket {
  id: number;
  userId: number;
  content: string;
  processingResult?: ProcessingResult;
  projectId?: number;
  createdTaskId?: number;
  processedAt?: string;
  createdAt: string;
  updatedAt: string;
}
```

#### 9.5.2 Constants

```typescript
type TaskStatus = "todo" | "in_progress" | "done";
type TaskPriority = "low" | "medium" | "high";
type PROJECT_STATUS = "active" | "archived" | "completed";
type ProcessingResult = "task" | "note" | "trash";
type USER_STATUS = "premium" | "regular";
```

#### 9.5.3 Icons

`IconId` is a union type of 30 Lucide icon name strings, defined in `src/lib/types/icons.ts`. The `ICON_IDS` array provides the full set for iteration (e.g. rendering an icon picker).

#### 9.5.4 verbatimModuleSyntax

`verbatimModuleSyntax` is enabled in `tsconfig.app.json`. All type-only imports must use `import type`:

```typescript
// Correct
import type { IArea } from "@/lib/types";

// Wrong — will fail type-check
import { IArea } from "@/lib/types";
```

---

### 9.6 Component Library

#### 9.6.1 Field Components

All field components accept `control: Control<T>` from `react-hook-form` and register directly into the form context. Use these inside dialogs — do not reinvent them.

| Component            | Purpose                        |
| -------------------- | ------------------------------ |
| `NameField`          | Text input for entity name     |
| `DescriptionField`   | Textarea for description       |
| `DueDateField`       | Date picker (react-day-picker) |
| `EstimatedTimeField` | Numeric input for minutes      |
| `PriorityField`      | Select: low / medium / high    |
| `IconField`          | Icon picker using `LazyIcon`   |
| `CheckboxField`      | Boolean toggle                 |
| `UrlField`           | URL text input                 |

Usage pattern:

```typescript
<NameField control={form.control} />
<PriorityField control={form.control} />
<DueDateField control={form.control} />
```

#### 9.6.2 Dialog Shells

| Component       | Use Case                         | Key Props                                                                                         |
| --------------- | -------------------------------- | ------------------------------------------------------------------------------------------------- |
| `FormDialog`    | Create / edit entity with a form | `title`, `description`, `open`, `onOpenChange`, `onSubmit`, `isLoading`, `hasChanges`, `maxWidth` |
| `ConfirmDialog` | Destructive yes/no confirmation  | `open`, `onOpenChange`, `onConfirm`, `isLoading`, `variant: 'danger' \| 'warning' \| 'info'`      |
| `CustomDialog`  | Freeform modal content           | `open`, `onOpenChange`, children                                                                  |

#### 9.6.3 Layout & List Primitives

| Component          | Purpose                                   | Key Props                                                                                       |
| ------------------ | ----------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `DialogFieldGrid`  | 1 or 2-column grid for dialog form fields | `columns: 1 \| 2`                                                                               |
| `AnimatedListItem` | Framer Motion enter/exit for list rows    | `index` (for stagger delay)                                                                     |
| `ListItemCard`     | Card-style list row                       | `padding: 'compact' \| 'comfortable'`, `border: 'primary' \| 'success' \| 'muted'`, `hoverable` |
| `ItemActionsMenu`  | Three-dot `DropdownMenu` for row actions  | `actions` array, `align: 'start' \| 'end'`                                                      |
| `EmptyState`       | Empty list placeholder                    | `icon`, `title`, `description`, `action`                                                        |
| `Timestamp`        | Relative or absolute date display         | `date`, `relative`                                                                              |
| `Divider`          | Semantic `<hr>`                           | `margin` variants                                                                               |
| `LazyIcon`         | Dynamic Lucide icon by name               | `name: IconId`                                                                                  |

#### 9.6.4 Infrastructure Components

| Component                | Purpose                                                                                                                                |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------- |
| `DragAndDropContext`     | Wraps `DndContext` from dnd-kit; handles `updateProject` mutation on drop to change `areaId`; droppable id pattern: `'area-${areaId}'` |
| `LoadingOverlayProvider` | Context provider + animated fullscreen overlay; exposes `useLoadingOverlay()` hook                                                     |
| `ThemeProvider`          | `next-themes` wrapper; `storageKey: 'nicoflow-theme'`                                                                                  |
| `ModeToggle`             | Light / dark / system toggle button                                                                                                    |
| `Toaster`                | Sonner `<Toaster>` configured for top-right position                                                                                   |

---

### 9.7 Feature Modules

Each feature lives under `src/features/` and exports its public API from `index.tsx`.

| Module      | Exported Components                                                                        |
| ----------- | ------------------------------------------------------------------------------------------ |
| `Area/`     | `AreaDialog` (create/edit), `AreaContextMenu` (right-click menu), `AreaSortOrderField`     |
| `Project/`  | `ProjectDialog`, `ProjectDeleteDialog`, `ProjectHeader`, `ProjectAreaField`                |
| `Sidebar/`  | `AppSidebar`, `Areas` (collapsible sections + draggable projects), `QuickAccess`, `Footer` |
| `Bucket/`   | `BucketList`, `BucketQuickInput`, `BucketProcessDialog`                                    |
| `Tasks/`    | `TaskDialog`, `TaskItem`, `TaskFilters`, `TaskSearch`, `TasksSection`                      |
| `SignForm/` | Shared auth form layout                                                                    |

---

### 9.8 Forms & Validation

All Zod schemas are defined in `src/lib/utils/utils/schemas.ts` and re-exported from `src/lib/utils/index.ts`.

#### 9.8.1 Usage Pattern

```typescript
import { projectSchema, type ProjectFormData } from "@/lib/utils";

const form = useForm<ProjectFormData>({
  resolver: zodResolver(projectSchema),
});
```

#### 9.8.2 Available Schemas

| Schema                 | Inferred Type            | Notes                              |
| ---------------------- | ------------------------ | ---------------------------------- |
| `loginSchema`          | `LoginFormData`          |                                    |
| `registerSchema`       | `RegisterFormData`       | Password + username rules enforced |
| `forgotPasswordSchema` | `ForgotPasswordFormData` |                                    |
| `resetPasswordSchema`  | `ResetPasswordFormData`  |                                    |
| `projectSchema`        | `ProjectFormData`        | Includes `areaId` field            |
| `areaSchema`           | `AreaFormData`           | Create                             |
| `updateAreaSchema`     | `UpdateAreaFormData`     | Edit (all fields optional)         |
| `taskSchema`           | `TaskFormData`           |                                    |
| `bucketSchema`         | `BucketFormData`         |                                    |
| `processBucketSchema`  | `ProcessBucketFormData`  |                                    |

#### 9.8.3 Validation Rules

**Password** (`passwordSchema`): minimum 8 characters, maximum 20, at least one digit, at least one lowercase letter, at least one uppercase letter.

**Username**: minimum 3 characters, maximum 20, alphanumeric only (no special characters).

---

### 9.9 Toast Messages

Use `sonner`. Import the `ToastMessages` constant from `@/lib/utils` for consistency across the codebase. Never import from `react-toastify`.

```typescript
import { toast } from "sonner";
import { ToastMessages } from "@/lib/utils";

toast.success(ToastMessages.PROJECT_CREATED);
toast.error(ToastMessages.UNEXPECTED_ERROR);
```

`ToastMessages` is defined in `src/lib/utils/utils/messages.ts` and covers: auth, project, task, area, bucket, rate-limit, and general error cases.

---

### 9.10 ESLint Rules

Rules are defined in `eslint.config.js` using the ESLint 9 flat config format.

| Rule                                 | Level   | Notes                                         |
| ------------------------------------ | ------- | --------------------------------------------- |
| `@typescript-eslint/no-explicit-any` | `error` | No exceptions — use `unknown` or proper types |
| `@typescript-eslint/no-unused-vars`  | `warn`  | Prefix variable name with `_` to suppress     |
| `simple-import-sort/imports`         | `error` | Auto-fix with `pnpm lint --fix`               |
| `import/no-duplicates`               | `error` |                                               |
| `import/first`                       | `error` |                                               |

#### 9.10.1 Import Order

Enforced by `simple-import-sort` in the following group order:

1. `react`, `react-dom`, `react-native`
2. `expo`, `@react-navigation`
3. All other external packages (`@?\\w`)
4. Internal `@/` path aliases
5. Parent-relative `../`
6. Same-folder `./`

---

### 9.11 Testing

#### 9.11.1 Commands

```bash
pnpm test              # vitest run (single pass)
pnpm test:watch        # vitest (watch mode)
pnpm test:ui           # vitest --ui (browser UI at localhost)
pnpm test:coverage     # vitest run --coverage (v8; outputs text/json/html/lcov/cobertura)
```

#### 9.11.2 Configuration

- **Environment:** `jsdom` (configured in `vite.config.ts`)
- **Globals:** enabled — no explicit `import { describe, it, expect }` needed
- **Path alias:** `@/` → `src/`

#### 9.11.3 Test Infrastructure

| File                            | Purpose                                                                                               |
| ------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `__tests__/setup.ts`            | Stubs `window.matchMedia`, `localStorage`, `sessionStorage`, `IntersectionObserver`, `ResizeObserver` |
| `__tests__/renderComponent.tsx` | `render` wrapper that provides Redux store + Router context                                           |

**MSW** (`msw@2`) is available for API mocking in integration tests.

**Test file location:** co-located with the component as `ComponentName.test.tsx`.

---

### 9.12 Storybook

Storybook 10 is configured in `.storybook/`. Stories are co-located next to their component: `src/components/ComponentName/ComponentName.stories.tsx`.

```bash
pnpm storybook    # → http://localhost:6006
```

#### 9.12.1 Story Infrastructure

| File                                            | Purpose                                                                                                                   |
| ----------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `src/stories/store/storybookStore.ts`           | Persist-free Redux store (no `redux-persist`), fresh instance per story                                                   |
| `src/stories/decorators/withStoryProviders.tsx` | Global decorator: Redux Provider → ThemeProvider (key: `'storybook-ui-theme'`) → LoadingOverlayProvider → Story → Toaster |
| `src/stories/helpers/StoryFormWrapper.tsx`      | Generic `Control<T>` render-prop for form field stories (avoids non-serializable `.args`)                                 |
| `src/stories/mocks/index.ts`                    | Typed mock factories: `mockUser`, `mockArea`, `mockProject`, `mockTask`                                                   |

---

### 9.13 CI/CD Pipeline

GitHub Actions workflows are in `.github/workflows/`.

| File                    | Trigger                                       | Jobs                                        |
| ----------------------- | --------------------------------------------- | ------------------------------------------- |
| `ci.yml`                | Push to any branch; PR to `main` or `staging` | lint → type-check → test (parallel) → build |
| `deploy-staging.yml`    | Push to `staging`                             | install → build → Vercel staging deploy     |
| `deploy-production.yml` | `workflow_dispatch` (type "DEPLOY")           | install → build → Vercel production deploy  |

**Required secrets:** `VERCEL_TOKEN`, `VERCEL_ORG_ID`, `VERCEL_PROJECT_ID_STAGING`, `VERCEL_PROJECT_ID_PRODUCTION`

#### 9.13.1 Branch Strategy

| Branch      | Purpose                  | Deploy                       |
| ----------- | ------------------------ | ---------------------------- |
| `main`      | Stable, production-ready | Manual (`workflow_dispatch`) |
| `staging`   | Integration branch       | Automatic on push            |
| `feature/*` | Feature development      | PR to `main`                 |

---

### 9.14 Local Development

```bash
# Prerequisites: Node 20+, pnpm 10.18.3+, nicoflow-api running on :8080
pnpm install
pnpm dev           # Vite dev server → http://localhost:5173
pnpm type-check    # tsc --noEmit
pnpm lint          # eslint .
pnpm lint --fix    # auto-fix import order + formatting
pnpm test
pnpm build         # → dist/
pnpm storybook     # → http://localhost:6006
```

`vercel.json` at the repo root configures the SPA fallback routing for Vercel deployments.
