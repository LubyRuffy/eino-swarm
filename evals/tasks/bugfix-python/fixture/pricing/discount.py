"""Order discounts.

An order whose total reaches a tier's minimum gets that tier's rate. Prices
are rounded to cents, half up.
"""

from decimal import Decimal

TIERS = [  # (minimum order total, discount rate), highest first
    (Decimal("1000"), Decimal("0.10")),
    (Decimal("500"), Decimal("0.05")),
    (Decimal("100"), Decimal("0.02")),
]


def discount_rate(total: Decimal) -> Decimal:
    for minimum, rate in TIERS:
        if total > minimum:
            return rate
    return Decimal("0")


def final_price(total) -> Decimal:
    total = Decimal(str(total))
    price = total * (1 - discount_rate(total))
    return price.quantize(Decimal("0.01"))


def bulk_total(totals) -> Decimal:
    return sum((final_price(t) for t in totals[1:]), Decimal("0"))
