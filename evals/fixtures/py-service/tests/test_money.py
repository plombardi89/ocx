import unittest

from catalog.money import format_cents, parse_dollars


class ParseDollarsTest(unittest.TestCase):
    def test_valid_amounts(self) -> None:
        cases = {"0": 0, "12": 1200, "12.5": 1250, "12.34": 1234, "0.07": 7, "007.50": 750}
        for text, want in cases.items():
            with self.subTest(text=text):
                self.assertEqual(parse_dollars(text), want)

    def test_rejects_invalid_amounts(self) -> None:
        for text in ["", ".", "1.", ".5", "-1", "+1", "1.234", "1,00", " 1", "abc"]:
            with self.subTest(text=text):
                with self.assertRaises(ValueError):
                    parse_dollars(text)


class FormatCentsTest(unittest.TestCase):
    def test_formats(self) -> None:
        cases = {0: "$0.00", 5: "$0.05", 1234: "$12.34", -5: "-$0.05", -1234: "-$12.34"}
        for cents, want in cases.items():
            with self.subTest(cents=cents):
                self.assertEqual(format_cents(cents), want)


if __name__ == "__main__":
    unittest.main()
