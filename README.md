# Echo — Smart Finance Tracker (Alive Money OS)

Echo is a personal finance product built to feel **alive**: it doesn’t just show charts, it proactively turns your transaction data into **clear next actions**, **monthly insights**, and a shareable **“Money Wrapped”** story.

This repo currently represents the **Echo API** (backend). Frontend is **TBA**.

## Product Vision

- **Overview that matters:** where your money goes, what’s changing, and what to do next.
- **Objective-driven insights:** progress against your goals, pacing, and gentle correction.
- **Story-driven summaries:** monthly “mini-wrapped” and a year-end “Money Wrapped” (Spotify Wrapped-style).
- **Automate good decisions:** recommend and (eventually) execute the highest-leverage actions.

## The 6 Pillars (mapped to features)

Echo is designed to explicitly hit these 6 outcomes:

1. **Your Money Wrapped (Hook):** personalized monthly/yearly recaps with fun + useful stats.
2. **Yearly Audit (Insight):** trend + anomaly detection (“why is this 40% higher than last year?”).
3. **Financial Foundation (Logic):** net worth, runway, emergency fund, debt visibility.
4. **Free Money (Optimization):** subscription hunting, fees, interest optimization, missed benefits.
5. **Clear Goals (Target):** goal/bucket tracking with pace alerts (“ahead/behind plan”).
6. **Money Operating System (Automation):** rules + tasks now; true automation later (when banking rails allow).

## MVP (Prove “Alive” + Wrapped)

Focus: make something people *want to come back to* and *share*.

- **Data ingestion v1:** CSV upload/import (bank exports) to avoid early aggregator complexity.
- **Transaction normalization:** merchant cleanup + categorization with user overrides.
- **Spending overview:** top categories, merchants, trends, month-to-date vs last month.
- **Goals/buckets v1:** targets, timelines, progress pacing, “behind plan” nudges.
- **Subscriptions v1:** detect recurring charges; one-tap “review/cancel checklist”.
- **Monthly insights:** “3 things changed this month” + “1 action to take this week”.
- **Mini-wrapped:** shareable monthly summary cards (no sensitive details by default).

## Post‑MVP (Moat: Operating System + Trust)

- **Bank connections:** Plaid / GoCardless / Teller (region dependent), incremental sync, webhooks.
- **Anomaly detection:** category/merchant deviation alerts; fee discovery; duplicate charges.
- **Net worth engine:** assets/liabilities snapshots, runway, debt payoff projections.
- **Automation engine:** “If this then that” rules, scheduled tasks, and (where possible) transfers.
- **Money Wrapped v2:** deeper storytelling, archetypes, comparisons to your own history, goal outcomes.
- **Notifications:** push + email digests, configurable, low-noise by design.
- **Sharing & virality:** privacy-preserving “wrapped” templates, referral loop, creator-friendly exports.

## AI Usage (Post‑MVP, opt‑in)

AI is valuable *after* Echo has strong deterministic foundations (clean data + rules). Planned uses:

- **Merchant + category enrichment:** better normalization of messy bank strings.
- **“Explain this” insights:** natural language explanations of trends/anomalies with citations to transactions.
- **Personal finance coach:** goal strategy suggestions, tradeoff analysis, and “next best action”.
- **Natural language queries:** “how much did I spend on eating out in Lisbon?”.
- **Story generation:** personalized Wrapped narratives from precomputed stats (no raw PII in prompts).

Guardrails (non-negotiable): user consent, data minimization, no model training on user data by default, and strong redaction for shareables.

## Startup Notes (Why this could work)

Personal finance is crowded, but most products are **passive dashboards**. Echo’s differentiation is:

- **Retention hook:** frequent mini-wraps + monthly insights, not just budgeting spreadsheets.
- **Actionable automation:** turn data into a prioritized checklist, then progressively automate.
- **Viral surface area:** Wrapped-style artifacts people can share (privacy-first).

Hard parts (and the opportunity): bank data quality, trust/security, compliance, and automation rails.

## Tech Stack (planned)

- **Backend:** Go
- **API:** Connect RPC (Buf) over HTTP (type-safe contracts via Protobuf)
- **DB:** Postgres (via `pgx`)
- **Jobs:** background workers for ingestion, normalization, insights, and notifications
- **Migrations:** Goose or `migrate` (TBD)
- **Frontend:** TBA (likely React Native or Next.js)

### Why Connect RPC is a good fit

- **Type safety for money:** strict contracts reduce edge-case bugs across clients.
- **Great DX:** works cleanly over HTTP without heavy gRPC browser constraints.
- **Streaming-ready:** useful for large transaction histories and incremental sync UX.

## Suggested Backend Architecture

- **Ingestion:** raw import (CSV now, bank aggregators later) → canonical transaction model
- **Normalization:** merchant resolution + category mapping with user overrides
- **Insights pipeline:** monthly stats, anomalies, goal pacing, subscription detection
- **Delivery:** API endpoints for dashboards + Wrapped; notification scheduler

## Security & Privacy (baseline)

- Encrypt sensitive data at rest (where applicable) and always in transit (TLS).
- Minimize storage of tokens/secrets; prefer aggregator tokenization patterns.
- Default shareables to aggregate stats; never include merchant names without explicit user choice.

## Status

This repository is an early-stage scaffold. The README defines the **product + technical direction** for EchoAPI.

## Local Development (TBD)

Once implementation lands, this section will include exact commands. Expected prerequisites:

- Go (toolchain version TBD)
- Postgres
- Buf (for Protobuf/Connect generation)
