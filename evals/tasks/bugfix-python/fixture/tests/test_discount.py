import unittest
from decimal import Decimal

from pricing.discount import bulk_total, discount_rate, final_price


class DiscountTest(unittest.TestCase):
    def test_tier_minimum_is_inclusive(self):
        self.assertEqual(discount_rate(Decimal("500")), Decimal("0.05"))
        self.assertEqual(discount_rate(Decimal("1000")), Decimal("0.10"))
        self.assertEqual(discount_rate(Decimal("99.99")), Decimal("0"))

    def test_rounds_half_up_to_cents(self):
        self.assertEqual(final_price("100.25"), Decimal("98.25"))

    def test_bulk_total_counts_every_order(self):
        self.assertEqual(bulk_total(["100", "200"]), Decimal("294.00"))

    def test_no_discount_below_lowest_tier(self):
        self.assertEqual(final_price("50"), Decimal("50.00"))


if __name__ == "__main__":
    unittest.main()
