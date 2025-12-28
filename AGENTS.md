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