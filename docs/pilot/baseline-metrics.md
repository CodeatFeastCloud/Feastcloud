# Pilot baseline and outcome measurement

Collect at least 14 comparable trading days before recommendations affect operations. Preserve raw extracts, transformation version, excluded days, and reasons for exclusion.

| Metric | Definition | Minimum evidence |
|---|---|---|
| Order reconciliation | Unique accepted source orders represented exactly once in FeastCloud / accepted source orders | Source export, import report, duplicate/rejection log |
| Waste value | Sum of recorded wasted quantity × effective ingredient cost | Waste events, cost layers, units/conversions |
| Unexplained variance | Absolute physical count minus ledger-expected quantity after documented movements, valued at cost | Opening count, purchases, recipe consumption, transfers, waste, closing count |
| Stockout rate | Accepted item-unavailable events / accepted order lines | Availability events and affected order lines |
| Order-to-ready time | Ready timestamp minus accepted timestamp, reported by service mode and peak/non-peak | Immutable order and ticket transitions |
| Manager administration | Minutes spent on counts, planning, purchases, reconciliation, and reporting | Time study using fixed activity categories |

Primary Phase 1 success is at least a 10% reduction in normalized waste value or unexplained variance without degrading order-to-ready time or food-safety completion.

Compare like-for-like weekdays and service periods. Report sample size, median, percentiles, seasonality, promotions, menu changes, outages, and missing data. Never present modelled savings as measured savings.

