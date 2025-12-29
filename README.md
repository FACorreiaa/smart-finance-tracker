# Echo — Smart Finance Tracker (Alive Money OS)

Echo is a personal finance product built to feel **alive**: it doesn’t just show charts, it proactively turns your transaction data into **clear next actions**, **monthly insights**, and a shareable **“Money Wrapped”** story.

This repo currently represents the **Echo API** (backend). Frontend is **TBA**.

## Product Vision

- **Overview that matters:** where your money goes, what’s changing, and what to do next.
- **Objective-driven insights:** progress against your goals, pacing, and gentle correction.
- **Story-driven summaries:** monthly “mini-wrapped” and a year-end “Money Wrapped” (Spotify Wrapped-style).
- **Automate good decisions:** recommend and (eventually) execute the highest-leverage actions.
- **Bring your own spreadsheet:** drop in your existing Excel/Sheets-style model and have Echo power it everywhere.

## The 6 Pillars (mapped to features)

Echo is designed to explicitly hit these 6 outcomes:

1. **Your Money Wrapped (Hook):** personalized monthly/yearly recaps with fun + useful stats.
2. **Yearly Audit (Insight):** trend + anomaly detection (“why is this 40% higher than last year?”).
3. **Financial Foundation (Logic):** net worth, runway, emergency fund, debt visibility.
4. **Free Money (Optimization):** subscription hunting, fees, interest optimization, missed benefits.
5. **Clear Goals (Target):** goal/bucket tracking with pace alerts (“ahead/behind plan”).
6. **Money Operating System (Automation):** rules + tasks now; true automation later (when banking rails allow).

## Bring Your Own Spreadsheet (BYOS)

Echo is built to work for people who already have a spreadsheet-based system.

- Upload an `.xlsx` (or select a saved template) that contains *your* categories, budgets, and formulas.
- Map Echo’s canonical fields (transactions, accounts, categories, goals) to named ranges/tables in the sheet.
- Echo keeps the sheet “fresh” across devices by re-computing outputs as new data syncs in.

Design constraints (intentional):

- No macros/VBA for safety and portability.
- Prefer deterministic, auditable calculations (Echo can show “why” a number changed).

## MVP (Prove “Alive” + Wrapped)

Focus: make something people *want to come back to* and *share*.

[x]- **Auth + sessions:** account creation, login, refresh, logout.
[x]- **Data ingestion v1:** CSV upload/import (bank exports) to avoid early aggregator complexity.
- **Document import v1:** import CSV/XLSX (and later invoices) into a canonical transaction model.
- **Transaction normalization:** merchant cleanup + categorization with user overrides.
- **Spending overview:** top categories, merchants, trends, month-to-date vs last month.
- **Foundation v1:** optional manual net worth entries + basic runway metric.
- **Goals/buckets v1:** targets, timelines, progress pacing, “behind plan” nudges.
- **Subscriptions v1:** detect recurring charges; one-tap “review/cancel checklist”.
- **Monthly insights:** “3 things changed this month” + “1 action to take this week”.
- **Mini-wrapped:** shareable monthly summary cards (no sensitive details by default).
- **BYOS v1:** upload a spreadsheet template + field mapping; export computed views as shareable/read-only.
- **Payments (optional in MVP):** Stripe Checkout for premium plan (behind feature flags).
- **XLSX Import:** Extend the import service to handle Excel files
- **Category Assignment:** Auto-categorize transactions or let users assign categories, add more metadata to transactions
- **Monthly Insights:** Aggregated spending by category/month
- **Empty State Dashboard:** Prompt new users to import

## Post‑MVP (Moat: Operating System + Trust)

- **Bank connections:** Plaid / GoCardless / Teller (region dependent), incremental sync, webhooks.
- **Invoice ingestion:** parse PDFs/images into transactions (receipts/invoices), reconcile against bank data.
- **Anomaly detection:** category/merchant deviation alerts; fee discovery; duplicate charges.
- **Net worth engine:** assets/liabilities snapshots, runway, debt payoff projections.
- **Automation engine:** durable tasks + “if this then that” rules, scheduled nudges, and (where possible) transfers.
- **Money Wrapped v2:** deeper storytelling, archetypes, comparisons to your own history, goal outcomes.
- **Notifications:** push + email digests, configurable, low-noise by design.
- **Sharing & virality:** privacy-preserving “wrapped” templates, referral loop, creator-friendly exports.
- **BYOS v2:** richer spreadsheet integration (templates, versioning, collaboration), more functions coverage, offline-first caching.
- **Billing maturity:** Stripe Billing, proration, coupons, taxes, and durable entitlement logic.

---

## Integrations Roadmap

Echo's power grows as it connects to more data sources. All integrations normalize into a canonical schema — the UI doesn't care where data came from.

### Banking Data Aggregators

| Provider | Regions | What They Offer | Notes |
|----------|---------|-----------------|-------|
| **Plaid** | US, Canada, UK, EU | Accounts, transactions, balances, identity | Industry standard, best US coverage |
| **TrueLayer** | UK, EU | Open Banking API, PSD2 compliant | Strong in Europe |
| **GoCardless (Bank Account Data)** | EU, UK | Free tier available, Open Banking | Good for starting in EU |
| **Teller** | US | Direct bank connections (no screen scraping) | Developer-friendly, newer |
| **Nordigen** (now GoCardless) | EU | Free Open Banking access | Great for EU MVP |
| **Yapily** | UK, EU | Open Banking, payments | Enterprise-focused |
| **Salt Edge** | Global (5000+ banks) | Wide coverage including non-Open Banking regions | Good for emerging markets |

### Investment & Broker Data

| Provider | What They Connect | Notes |
|----------|-------------------|-------|
| **Plaid (Investments)** | US brokerages (Fidelity, Schwab, Robinhood, etc.) | Holdings, transactions, cost basis |
| **Yodlee** | Broad investment coverage | Enterprise pricing, older but comprehensive |
| **Finicity** (Mastercard) | US investments + banking | Strong investment data |
| **Snaptrade** | US/Canada brokerages | Developer-friendly, investment-focused |
| **Vezgo** | Crypto exchanges + wallets | If crypto tracking is desired |

### Neobanks (Revolut, N26, Wise, Monzo)

- **No direct APIs** for personal accounts (only business APIs available)
- **Access via Open Banking aggregators** (TrueLayer, Plaid UK/EU, GoCardless)
- **CSV export** as fallback (manual but reliable)

### Payment Platforms

| Platform | API Access | What You Can Get | Notes |
|----------|------------|------------------|-------|
| **PayPal** | ✅ Transaction Search API | Transaction history, balances, payouts | OAuth 2.0, 3-year history, requires app approval |
| **Stripe** | ✅ Full API | Charges, payouts, balances, subscriptions | Best for business/creator income tracking |
| **Venmo** | ❌ No public API | — | Access via Plaid or CSV export only |
| **Cash App** | ❌ No public API | — | Access via Plaid or CSV export only |
| **Wise (TransferWise)** | ✅ API available | Balances, transactions, multi-currency | Good for international transfers |
| **Payoneer** | ✅ API available | Balances, transactions | Freelancer/contractor focus |

**PayPal Integration Notes:**
- Use the [Transaction Search API](https://developer.paypal.com/docs/api/transaction-search/v1/) for pulling history
- Requires OAuth 2.0 authentication with user consent
- Can retrieve up to 3 years of transaction data
- Webhooks available for real-time transaction notifications

### Integration Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    EchoOS Backend                       │
├─────────────────────────────────────────────────────────┤
│                 Canonical Data Model                    │
│   (Accounts, Transactions, Holdings, Balances)          │
├──────────┬──────────┬──────────┬──────────┬────────────┤
│  CSV     │  Plaid   │ TrueLayer│ Snaptrade│  Manual    │
│  Import  │  (US)    │  (EU/UK) │ (Invest) │  Entry     │
└──────────┴──────────┴──────────┴──────────┴────────────┘
```

### Phased Rollout

| Phase | Scope | Why |
|-------|-------|-----|
| **MVP** | CSV import only | Zero dependencies, fast iteration, validates core value |
| **Post-MVP v1** | Plaid (US) or GoCardless/TrueLayer (EU) | Automated bank sync for primary market |
| **Post-MVP v2** | Investment tracking (Plaid Investments or Snaptrade) | Complete net worth picture |
| **Scale** | Multi-provider support, fallback chains, manual reconciliation | Reliability + global coverage |

---

## Pre‑MVP Enhancements (strengthen the "Alive" hook)

These add minimal complexity but amplify differentiation:

| Feature | Why It Matters |
|---------|----------------|
| **Intent Tagging** | Let users flag transactions with *intent* ("splurge", "necessary", "regret", "investment"). Enables emotional/behavioral insights beyond pure categories. |
| **Spending Pulse** | Daily/weekly lightweight digest: "Your wallet today: €47 across 3 places." Keeps Echo top-of-mind without requiring dashboard visits. |
| **Quick Capture Mode** | One-tap log for cash/offline spending. Essential in regions with mixed cash/card usage. |
| **Goal Burn Rate** | "At current pace, you'll hit Vacation in 4.2 months (3 weeks early)." Real-time pacing beats static progress bars. |
| **Merchant Emoji/Icon System** | Auto-assign or user-pick icons per merchant. Makes overviews scannable and fun. |
| **"Oops" Alert Config** | User-defined thresholds for instant alerts: "Tell me if I spend > €50 on dining in a day." |
| **Streak Mechanics** | "7 days under budget" or "30 days no impulse spending" — gamification for habit building. |
| **Tag Bundles** | Group merchants/categories into user-defined "bundles" (e.g., "Self-care" = gym + therapy + spa). |

---

## Post‑MVP Feature Deep Dive

### Automation & Orchestration

Building the "Operating System" layer that makes finance hands-off:

| Feature | Description |
|---------|-------------|
| **Money Flows Canvas** | Visual node editor showing how money moves: Income → Buckets → Bills → Goals. Drag-and-drop rule creation. |
| **Scheduled Sweeps** | Auto-move "surplus" (income − bills − buffer) into designated goal accounts monthly. |
| **If/Then Rule Engine** | "If checking drops below €1,000, pause non-essential subscriptions." User chooses: execute or notify. |
| **Deferral Queue** | "I want X but not now" wishlist. Echo reminds when the purchase won't disrupt goals. |
| **Bill Negotiation Executor** | For supported services, Echo can initiate rate reduction requests via integrated APIs. |
| **Smart Payoff Optimizer** | Given debts + interest rates, auto-suggest (or execute) optimal extra payments (avalanche vs snowball). |

### Intelligence & Coaching

Turn passive data into proactive guidance:

| Feature | Description |
|---------|-------------|
| **Financial Health Score** | Custom index combining runway, debt ratio, savings rate, and goal progress. Track trends over time. |
| **Scenario Planner** | "What if I increase rent by €200?" → instant impact on runway, goals, and timelines. |
| **Negotiation Toolkit** | Echo drafts email/chat templates for subscription cancellations, rate reductions, and fee reversals. |
| **Tax Prep Helper** | Flag potentially deductible categories; export receipts/invoices grouped by type for tax submission. |
| **Carbon/Impact Tracker** | Estimate spending-linked emissions ("Your flights this year ≈ 1.2 tons CO₂"). Opt-in only. |
| **Life Event Advisor** | Context-aware suggestions for major events: moving, having a baby, job change, retirement. |
| **Spending Forecast** | Predict next month's spending based on recurring patterns + seasonality + upcoming known expenses. |
| **Category Insights NLP** | "Why did groceries spike in March?" → "You had 4 Whole Foods trips vs 2 normally, totaling €180 extra." |

### Social & Community

Optional sharing features that build virality and accountability:

| Feature | Description |
|---------|-------------|
| **Anonymous Benchmarks** | "People your age/location spend 18% less on dining." Opt-in, differential privacy, no PII exposure. |
| **Shared Goals** | Couples/roommates contribute to joint buckets with visibility and contribution controls. |
| **Accountability Partners** | Invite a friend to see your goal progress (not amounts). Mutual encouragement. |
| **Template Marketplace** | Users share BYOS spreadsheet templates: budgets, debt payoff trackers, FIRE calculators, etc. |
| **Community Challenges** | Monthly opt-in challenges: "No-spend weekend", "Pack lunch all week", "Save €100 extra". |

### Premium & Power User

Features for advanced users and monetization:

| Feature | Description |
|---------|-------------|
| **Multi-Currency Native** | Full support for travelers and remote workers paid in multiple currencies. Auto-convert using live rates. |
| **Business Lite Mode** | Freelancers: separate personal/business views, invoice matching, quarterly VAT/tax estimates. |
| **API Access** | Let power users push Echo data to Notion, Obsidian, personal dashboards, or automations. |
| **Offline-First Sync** | Complete local storage + conflict resolution. Appeals to privacy-maximalist users. |
| **Investment Tracking Lite** | Manual or synced portfolio tracking with basic allocation views. Not a replacement for dedicated apps. |
| **Real Estate Module** | Property value estimates, rental income tracking, mortgage payoff projections. |
| **Family Dashboard** | Aggregate views for family finances with role-based access (parent/child/partner). |

---

## "Wrapped" Expansion Ideas

The Wrapped mechanic is a powerful retention + virality lever. Lean into it:

| Idea | Details |
|------|---------|
| **Quarterly Mini-Wrap** | Lighter version: top 3 changes, 1 win, 1 watch-out. Keeps engagement between annuals. |
| **Wrapped Archetypes** | Personality-style summaries: "Cautious Saver", "Spontaneous Spender", "Goal Crusher", "Optimizer". |
| **Milestone Wraps** | Celebrate goal completions: "You paid off your credit card!" as shareable cards with confetti. |
| **Comparison to Past Self** | "You spent 12% less on coffee than 2024 You." Avoid peer comparison for privacy-first positioning. |
| **Wrapped for Couples** | Opt-in joint summary for partners sharing budgets. Highlights combined wins. |
| **Category Deep Dives** | Optional per-category wraps: "Your Travel Year", "Your Food Story", "Your Subscription Stack". |
| **Streak Celebrations** | Shareable badges for streaks: "90 days under budget", "1 year no overdraft fees". |
| **Year-over-Year Trends** | Multi-year view: "Your savings rate over the last 3 years" with trajectory visualization. |

---

## Prioritization Framework

When evaluating what to build, filter through these lenses:

### Primary Filters

1. **Retention First** — Does it bring people back weekly/daily? (Pulse, streaks, insights)
2. **Action-Oriented** — Does it tell users *what to do*, not just what happened? (Nudges, checklists)
3. **Shareable** — Can users show it off without leaking sensitive data? (Wrapped, badges)
4. **Automation Potential** — Will this unlock hands-off behavior later? (Rules, sweeps)

### Effort/Impact Matrix

| | Low Effort | High Effort |
|---|---|---|
| **High Impact** | Quick wins: Intent tags, Spending Pulse, Streaks | Strategic bets: Rule Engine, Money Flows, Bank Sync |
| **Low Impact** | Nice-to-haves: Emoji icons, bonus Wrapped themes | Avoid: Complex features with niche appeal |

### Trust & Security Lens

Every feature must pass:
- Does it require new PII? If so, is it worth the compliance burden?
- Can it be implemented with minimal data exposure?
- Does it maintain user control and transparency?

---

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
- **Spreadsheet engine:** XLSX template parsing + formula evaluation (no macros), plus field mapping
- **Payments:** Stripe (Checkout + Billing)
- **Bank data (later):** Plaid (items, accounts, transactions) with a normalization layer into Echo’s canonical models
- **Clients (multiplatform):** Web, Android, iOS, Desktop (frontend/framework TBA)

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

## Study
Echo Pillar	App to Study	Key Insight to "Steal"			
Pillar 1: Wrapped	Monzo / Spotify	Use "Archetypes" and non-sensitive stats to make it shareable.			
Pillar 3: Foundation	Monarch Money	The way they handle "Sanity Checks" on net worth data.			
Pillar 6: Automation	Sequence	The visual "Nodes and Edges" UI for money movement.			
BYOS Feature	Tiller Money	Their field-mapping logic (Canonical Data → User Sheet).			
Subscriptions	Rocket Money	The "One-tap" cancellation checklist UI.

## Status

This repository is an early-stage scaffold. The README defines the **product + technical direction** for EchoAPI.

## Local Development (TBD)

Once implementation lands, this section will include exact commands. Expected prerequisites:

- Go (toolchain version TBD)
- Postgres
- Buf (for Protobuf/Connect generation)
