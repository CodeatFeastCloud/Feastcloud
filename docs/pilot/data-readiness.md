# Live pilot data-readiness checklist

## Authority and purpose

- [ ] Named customer data owner and FeastCloud implementation owner
- [ ] Documented purposes for orders, recipes, inventory, employee, supplier, and guest data
- [ ] Approved retention, deletion, export, incident, and support procedures
- [ ] Tenant identifier and outlet hierarchy agreed
- [ ] Production data prohibited from source control, fixtures, screenshots, and issue trackers

## Minimum imports

- [ ] Menu items, modifiers, brands, effective prices, taxes, and station routing
- [ ] Ingredients, units/conversions, recipe versions, yields, and effective dates
- [ ] At least 8–12 weeks of order history where available
- [ ] Purchases, receipts, transfers, counts, waste, and supplier price history
- [ ] Source identifiers preserved for reconciliation and idempotency

## Validation

- [ ] Currency uses integer minor units
- [ ] Quantities retain source unit and normalized unit
- [ ] Timestamps retain source timezone and convert to UTC
- [ ] Totals reconcile to source reports before data is used for recommendations
- [ ] Allergens and food-safety content receive authorized human review
- [ ] Employee and guest personal data minimized or pseudonymized

## Launch gate

- [ ] Backup and restore tested
- [ ] Manual fallback documented and rehearsed
- [ ] Support hours and severity definitions agreed
- [ ] Baseline period signed off
- [ ] Shadow-mode end date and approval authority recorded

